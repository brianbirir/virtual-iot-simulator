# IoT Device Simulator — Design Document

| Field | Value |
|-------|-------|
| **Document type** | Technical Design Document |
| **Status** | In Progress — Phase 1 complete |
| **Authors** | Brian |
| **Last updated** | March 2026 |
| **Companion doc** | `IMPLEMENTATION_PLAN.md` (task-level execution plan) |

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Problem Statement](#2-problem-statement)
3. [Goals & Non-Goals](#3-goals--non-goals)
4. [System Architecture](#4-system-architecture)
5. [Component Deep Dives](#5-component-deep-dives)
   - 5.1 Simulation Orchestrator (Python)
   - 5.2 Device Runtime (Go)
   - 5.3 Data Generation Engine
   - 5.4 Protocol Adapters
   - 5.5 Scenario Engine
   - 5.6 Service Contract (gRPC/Protobuf)
6. [Data Models](#6-data-models)
7. [Concurrency & Scaling Model](#7-concurrency--scaling-model)
8. [Fault Injection Framework](#8-fault-injection-framework)
9. [Observability](#9-observability)
10. [Deployment Architecture](#10-deployment-architecture)
11. [Security Considerations](#11-security-considerations)
12. [Performance Targets & Capacity Planning](#12-performance-targets--capacity-planning)
13. [API Reference](#13-api-reference)
14. [Configuration Reference](#14-configuration-reference)
15. [Risks & Mitigations](#15-risks--mitigations)
16. [Decision Log](#16-decision-log)
17. [Glossary](#17-glossary)

---

## 1. Executive Summary

This document describes the design of a large-scale IoT device simulator capable of emulating hundreds of thousands of concurrent virtual devices. The simulator generates realistic telemetry data across multiple IoT protocols (MQTT, AMQP, HTTP, CoAP) and publishes it to real backend infrastructure for testing, load validation, and development purposes.

The system is split into two services: a **Python-based Simulation Orchestrator** for configuration, fleet management, and scenario scripting, and a **Go-based Device Runtime** for high-performance concurrent device simulation. They communicate via a **gRPC contract** defined in Protocol Buffers.

The design prioritises realistic device behaviour (not just load generation), deterministic replay for debugging, and a scaling path from single-process development to distributed cloud-native deployment.

---

## 1.1 Implementation Status

| Phase | Scope | Status |
| ----- | ----- | ------ |
| **Phase 1** | Proto definitions · Go device runtime (Manager, VirtualDevice, Broadcaster) · gRPC server (SpawnDevices, StopDevices, GetFleetStatus, StreamTelemetry, GetRuntimeStatus) · Python orchestrator (RuntimeClient, config loader, CLI: spawn/stop/status/stream) · Console publisher · Gaussian + Static generators · Temperature sensor profile | ✅ Complete |
| **Phase 2** | Brownian, Diurnal, Markov generators · Structured telemetry envelope (`device_type`, nested `fields`, `labels`) · `masterSeed → deviceSeed → fieldSeed` chain | ✅ Complete |
| **Phase 3** | MQTT, HTTP, AMQP protocol adapters · Shared publisher pool in Manager · Python `RuntimePool` (consistent-hash routing) · `ScenarioEngine`, `SimClock`, `ScenarioContext`, `ScenarioRunner` · CLI `scenario run` command | ✅ Complete |
| **Phase 4** | Fault injection: DISCONNECT, LATENCY_SPIKE, DATA_CORRUPTION, BATTERY_DRAIN, CLOCK_DRIFT · `InjectFault` + `UpdateDeviceBehavior` gRPC handlers · `StreamEvents` gRPC handler · Device lifecycle events | ✅ Complete |
| **Phase 5** | Prometheus metrics (`sim_devices_active`, `sim_messages_sent_total`, `sim_publish_latency_seconds`, `sim_device_errors_total`, `sim_backpressure_drops_total`, `sim_faults_injected_total`) · `/metrics` endpoint on admin HTTP server · `--master-seed`, `--mqtt-url`, `--http-endpoint`, `--amqp-url` runtime flags | ✅ Complete |

---

## 2. Problem Statement

### Context

IoT platforms must handle fleets of thousands to millions of devices, each publishing telemetry at varying intervals with diverse payload schemas. Testing these platforms requires device fleets that exhibit realistic behaviour — gradual sensor drift, diurnal cycles, intermittent connectivity, firmware update disruptions, and correlated failures.

### The Gap

Existing approaches fall short in several ways:

- **Simple load generators** (e.g., `mosquitto_pub` in a loop) produce uniform traffic that doesn't stress realistic code paths like anomaly detection, time-series compression, or stateful device management.
- **Hardware test fleets** are expensive, physically constrained, and impossible to scale to 100K+ devices.
- **Cloud-provider simulators** (e.g., AWS IoT Device Simulator) are locked into one vendor's ecosystem and don't support arbitrary protocol combinations or custom behavioural models.

### What We Need

A simulator that acts as a **digital twin factory** — spawning virtual devices that are indistinguishable from real ones at the protocol and data layer, at a scale of 100K–1M concurrent devices, with programmable behaviour and fault injection.

---

## 3. Goals & Non-Goals

### Goals

| ID | Goal | Success Metric |
|----|------|---------------|
| G1 | Simulate 100K+ concurrent devices in a single process | Measured via benchmark |
| G2 | Produce realistic, statistically valid telemetry | Diurnal cycles, drift, noise match configurable distributions |
| G3 | Support MQTT, AMQP, HTTP, and CoAP protocols | Each protocol adapter passes integration tests against real brokers |
| G4 | Enable programmable scenarios (ramp-up, failure, firmware update) | Scenario scripts drive fleet behaviour over time |
| G5 | Provide fault injection primitives | Disconnect, latency spike, data corruption, clock drift |
| G6 | Deterministic replay | Same seed + scenario = byte-identical telemetry |
| G7 | Full observability of the simulator itself | Prometheus metrics, structured logging, admin API |
| G8 | Scale horizontally to 1M+ devices | Multi-instance sharding with consistent hashing |

### Non-Goals

- **Cloud-to-device command handling beyond stub responses** — the simulator acknowledges C2D commands but doesn't implement full device-side business logic.
- **Physical device emulation** — no hardware-in-the-loop, no radio-layer simulation.
- **Built-in IoT backend** — the simulator targets *external* backends; it doesn't include its own MQTT broker, time-series DB, or rules engine.
- **GUI** — CLI and API only. Grafana dashboards provide visualisation.
- **Multi-tenancy** — single-tenant; one simulator instance serves one test scenario.

---

## 4. System Architecture

### 4.1 High-Level Component Diagram

```
┌──────────────────────────────────────────────────────────────────┐
│                    SIMULATION ORCHESTRATOR (Python)                │
│                                                                    │
│  ┌──────────────┐  ┌──────────────┐  ┌────────────────────┐      │
│  │ Profile       │  │ Scenario     │  │ Fleet Manager      │      │
│  │ Registry      │  │ Engine       │  │ (gRPC client pool) │      │
│  │               │  │              │  │                    │      │
│  │ YAML → proto  │  │ Python       │  │ Spawn/stop/fault   │      │
│  │ validation    │  │ scenario     │  │ Consistent-hash    │      │
│  │ Pydantic      │  │ scripts      │  │ routing to runtimes│      │
│  └──────┬───────┘  └──────┬───────┘  └────────┬───────────┘      │
│         │                  │                   │                   │
│         └──────────────────┼───────────────────┘                   │
│                            │                                       │
│                     CLI (typer/click)                               │
└────────────────────────────┼───────────────────────────────────────┘
                             │
                     gRPC (protobuf v1)
                     HTTP/2, binary frames
                             │
              ┌──────────────┼──────────────┐
              │              │              │
     ┌────────▼───┐  ┌──────▼─────┐  ┌────▼──────────┐
     │ Runtime     │  │ Runtime    │  │ Runtime        │
     │ Instance 0  │  │ Instance 1 │  │ Instance N     │
     │             │  │            │  │                │
     │ 50K devices │  │ 50K devices│  │ 50K devices    │
     └──────┬──────┘  └─────┬──────┘  └───────┬───────┘
            │               │                  │
     ┌──────▼──────────────▼──────────────────▼────────┐
     │              Protocol Adapters                    │
     │  ┌──────┐  ┌──────┐  ┌──────┐  ┌──────┐         │
     │  │ MQTT │  │ AMQP │  │ HTTP │  │ CoAP │         │
     │  └──┬───┘  └──┬───┘  └──┬───┘  └──┬───┘         │
     └─────┼─────────┼────────┼──────────┼──────────────┘
           │         │        │          │
           ▼         ▼        ▼          ▼
     ┌─────────────────────────────────────────────┐
     │          TARGET IoT BACKEND                  │
     │  (MQTT broker, REST API, message queue, ...) │
     └─────────────────────────────────────────────┘
```

### 4.2 Language Split Rationale

The system uses two languages, each chosen for the layer it serves:

**Python (Simulation Orchestrator)** handles configuration parsing, scenario scripting, fleet lifecycle management, and the CLI. These tasks prioritise developer ergonomics and expressiveness over raw throughput. Python is not on the hot path — it sends gRPC commands and receives aggregated telemetry streams.

**Go (Device Runtime)** handles the performance-critical inner loop: running N goroutines (one per virtual device), each generating telemetry on a timer and publishing via protocol adapters. Go's concurrency model is a natural fit:

- Each goroutine starts at ~4 KB of stack. At 100K devices, baseline stack memory is ~400 MB — well within a single machine.
- Go's GMP scheduler (Goroutines, Machines, Processors) uses work stealing across per-P local run queues, delivering near-linear core scaling without manual sharding.
- The network poller (epoll on Linux) parks goroutines waiting on I/O. 100K goroutines each holding an MQTT connection aren't 100K OS threads — they're parked Gs waiting on a single epoll instance.
- Per-P mcache provides lock-free memory allocation for the high-frequency small allocations generated by telemetry payloads.
- Cooperative preemption (Go 1.14+) ensures CPU-intensive telemetry generation in one device doesn't starve others.
- The sysmon background thread handles timer firing (critical for device tick intervals), GC triggering, and preemption signalling.

### 4.3 Communication Model

The orchestrator and runtime communicate exclusively via gRPC over HTTP/2. This gives:

- **Schema-first contract** via `.proto` files — the single source of truth for all inter-service types.
- **Typed code generation** in both languages — eliminates serialisation bugs.
- **Server-streaming** for telemetry — Go streams batched telemetry to Python, not the other way around. The orchestrator uses separate unary RPCs for control commands.
- **HTTP/2 flow control** provides natural backpressure — if the Python consumer can't keep up, `stream.Send()` on the Go side blocks, preventing unbounded memory growth.

---

## 5. Component Deep Dives

### 5.1 Simulation Orchestrator (Python)

The orchestrator is the control plane. It doesn't simulate devices — it tells the runtime what to simulate and monitors the results.

**Responsibilities:**
- Load and validate device profiles from YAML files.
- Convert profiles to protobuf `DeviceSpec` messages.
- Manage fleet lifecycle (spawn, stop, query) via gRPC calls to one or more runtime instances.
- Execute scenario scripts that choreograph fleet behaviour over time.
- Route requests to the correct runtime instance using consistent hashing on `device_id`.
- Provide a **CLI** for interactive and scripted use.
- Provide a **REST API** (FastAPI) for programmatic and dashboard integration.

**Key classes (Phase 1 implemented):**

```
RuntimeClient          — Async typed wrapper around generated gRPC stub.
                         Supports spawn, stop, status, runtime_status,
                         stream_telemetry.
config.py              — load_profile() / load_profile_specs() — loads and
                         validates YAML profiles via Pydantic, converts to
                         DeviceSpec protos.
CLI (cli.py)           — typer commands: spawn, stop, status, stream, serve.
REST API (api.py)      — FastAPI app: POST /spawn, POST /stop, GET /status,
                         GET /stream (SSE), GET /health.
```

**Planned (Phase 3+):**

```text
ProfileRegistry        — Caching profile loader.
RuntimePool            — Consistent-hash routing across multiple runtime instances.
ScenarioContext        — Injected into scenario scripts; exposes spawn/stop/
                         fault/wait primitives.
ScenarioRunner         — Discovers and executes scenario scripts.
SimClock               — Simulation clock with configurable speed multiplier.
```

**Profile validation schema** (Pydantic):

```python
class TelemetryFieldConfig(BaseModel):
    type: Literal["gaussian", "brownian", "diurnal", "markov", "static"]
    # Gaussian
    mean: float | None = None
    stddev: float | None = None
    # Brownian
    start: float | None = None
    drift: float | None = None
    volatility: float | None = None
    mean_reversion: float | None = None
    min: float | None = None
    max: float | None = None
    # Diurnal
    baseline: float | None = None
    amplitude: float | None = None
    peak_hour: int | None = None
    noise_stddev: float | None = None  # renamed to stddev before proto encoding
    # Markov
    states: list[str] | None = None
    transition_matrix: list[list[float]] | None = None
    initial_state: str | None = None
    # Static
    value: Any | None = None

class DeviceProfileConfig(BaseModel):
    type: str
    protocol: Literal["mqtt", "amqp", "http", "console"] = "console"
    topic_template: str = "devices/{device_id}/telemetry"
    telemetry_interval: str = "5s"  # parsed as duration, e.g. "5s", "500ms"
    telemetry_fields: dict[str, TelemetryFieldConfig] = {}
    labels: dict[str, str] = {}
```

### 5.2 Device Runtime (Go)

The runtime is the data plane. It runs virtual devices as goroutines, generates telemetry, and publishes it over configured protocols.

**Core abstractions:**

```go
// VirtualDevice represents one simulated IoT device.
type VirtualDevice struct {
    ID         string
    DeviceType string
    Labels     map[string]string
    Interval   time.Duration
    Publisher  protocol.Publisher          // protocol adapter
    Topic      string                      // resolved topic string
    generators map[string]generator.Generator // field_name → data generator
    clock      *RuntimeClock
    state      simulatorv1.DeviceState     // proto enum: IDLE, RUNNING, ERROR, STOPPED
    telemetryCh chan<- *simulatorv1.TelemetryPoint  // write-only fan-in channel
    cancel     context.CancelFunc
    mu         sync.RWMutex
}

// Manager controls the fleet of virtual devices.
type Manager struct {
    devices     map[string]*VirtualDevice
    mu          sync.RWMutex
    clock       *RuntimeClock
    telemetryCh chan *simulatorv1.TelemetryPoint  // fan-in to broadcaster
    ctx         context.Context
    cancel      context.CancelFunc
}
```

> **Phase 1 note:** `Faults`, per-device `seed`, and `metrics` fields are planned for Phase 4 (fault injection) and Phase 5 (observability).

**Device lifecycle state machine:**

```
                  Spawn()
    ┌──────────┐ ────────▶ ┌──────────┐
    │   IDLE   │           │ RUNNING  │
    └──────────┘           └─────┬────┘
                                 │
                    ┌────────────┼────────────┐
                    │            │            │
               Stop(graceful)  Error     Fault(disconnect)
                    │            │            │
                    ▼            ▼            ▼
              ┌──────────┐ ┌──────────┐ ┌──────────┐
              │ STOPPED  │ │  ERROR   │ │ RUNNING  │ (fault auto-reverts)
              └──────────┘ └──────────┘ └──────────┘
```

**Run loop** (per device, per goroutine):

```
1. Set state → RUNNING
2. Start ticker at configured interval
3. Loop:
   a. Wait for tick or context cancellation
   b. Check active faults — apply modifications (skip publish, add latency, corrupt data)
   c. For each telemetry field:
      i.  Call generator.Next(now, state)
      ii. Build TelemetryPoint
   d. Serialize payload as JSON
   e. Call publisher.Publish(topic, payload)
   f. On error: log, increment error counter, continue
   g. Send TelemetryPoint to fan-in channel (non-blocking; drop if full in lossy mode)
4. On context cancel: set state → STOPPED, clean up
```

### 5.3 Data Generation Engine

Each telemetry field on a virtual device is backed by a `Generator` that produces the next value. Generators are deterministic given a seed, enabling replay.

```go
type Generator interface {
    Next(now time.Time, state map[string]any) any
}
```

**Seed derivation for determinism:**
```
baseSeed (per device, derived externally or 0 for Phase 1)
  └──▶ fieldSeed = baseSeed XOR fnv64a(fieldName)
          └──▶ rand.New(rand.NewSource(fieldSeed))
```

> **Planned (Phase 3):** The full 3-level derivation — `masterSeed → deviceSeed = masterSeed XOR hash(deviceID) → fieldSeed` — is not yet wired. Currently `baseSeed=0` is passed to the factory, giving per-field determinism from field name alone.

#### Generator Types

**Gaussian Noise** — Independent samples from a normal distribution. Used for sensors with random measurement error.

```
Parameters: mean, stddev, seed
Formula:    value = mean + stddev × NormFloat64()
Use cases:  Temperature jitter, pressure noise, current fluctuation
```

**Brownian Motion (Random Walk with Mean Reversion)** — Time-correlated drift with tendency to return to a baseline. Used for slowly changing environmental sensors.

```
Parameters: start, drift, volatility, mean_reversion, mean, min, max, seed
Formula:    next = current
                 + drift × dt
                 + volatility × √dt × Z
                 + mean_reversion × (mean − current) × dt
            clamped to [min, max]
Use cases:  Humidity, soil moisture, tank level, ambient pressure
```

**Diurnal Cycle** — Sinusoidal pattern tied to time of day, with optional noise overlay. Used for any sensor that follows a daily pattern.

```
Parameters: baseline, amplitude, peak_hour (0–23), noise_stddev, seed
Formula:    value = baseline
                  + amplitude × sin(2π × (hour − peak_hour + 6) / 24)
                  + noise_stddev × NormFloat64()
Use cases:  Temperature (outdoor), solar irradiance, occupancy, traffic
```

**Markov State Machine** — Discrete state transitions governed by a probability matrix. Used for devices with enumerated states.

```
Parameters: states[], transition_matrix[][], initial_state, seed
Formula:    Probabilistic transition from current row of matrix.
            Returns current state as string.
Use cases:  Door (open/closed), lock (locked/unlocked), valve (open/closed/partial),
            device (online/sleeping/error)
```

**Static** — Returns a fixed value. Used for testing and for immutable device properties.

```
Parameters: value
Formula:    Returns value unchanged.
Use cases:  Firmware version, model number, location (fixed install)
```

**Composite (planned)** — Chains multiple generators. For example, `Diurnal + Gaussian` produces a daily cycle with measurement noise overlaid.

#### Generator Factory

The factory maps configuration dicts (from protobuf `Struct`) to concrete Generator instances:

```go
func NewFromConfig(config map[string]any, fieldName string, baseSeed int64) (Generator, error)
```

The `type` key in the config selects the generator. `fieldName` is used to derive a per-field seed via `baseSeed XOR fnv64a(fieldName)`. Remaining keys are validated per type. Unknown types return a descriptive error.

> **Phase 1 implemented:** `gaussian` and `static`. Brownian, diurnal, and Markov generators are planned for Phase 2.

### 5.4 Protocol Adapters

All telemetry publishing is abstracted behind the `Publisher` interface:

```go
type Publisher interface {
    Publish(ctx context.Context, topic string, payload []byte) error
    Close() error
}
```

This decouples device simulation logic from transport concerns. Devices don't know or care whether they're publishing to MQTT, HTTP, or stdout.

#### MQTT Publisher

The primary adapter. MQTT is the dominant IoT protocol.

- **Library**: `github.com/eclipse/paho.mqtt.golang` (v1) or `github.com/eclipse/paho.golang` (v5).
- **Connection pooling**: Maintains N persistent TCP connections. Devices are assigned to connections via `hash(deviceID) % poolSize`. One connection handles ~1000 devices.
- **QoS**: Configurable per profile (0, 1, or 2). Default QoS 1 for guaranteed delivery.
- **Reconnection**: Automatic with exponential backoff + jitter. Devices using a disconnected connection queue locally until reconnect.
- **Topic resolution**: Replaces `{device_id}` and other placeholders in the template string.
- **TLS**: Optional, configured via broker URL scheme (`ssl://`) and certificate paths.

At 100K devices with pool size 100, the simulator opens 100 TCP connections to the broker — well within typical broker limits.

#### HTTP Publisher

For REST-based IoT platforms.

- **Method**: POST with JSON body to a configurable endpoint URL.
- **Connection pooling**: Uses Go's built-in `http.Client` with `MaxIdleConns` and `MaxIdleConnsPerHost`.
- **Batching**: Accumulates N payloads and POSTs as a JSON array, configurable.
- **Retry**: Exponential backoff on 5xx and timeouts.

#### AMQP Publisher

For RabbitMQ-based architectures.

- **Library**: `github.com/rabbitmq/amqp091-go`.
- **Channel pooling**: One AMQP channel per goroutine (AMQP best practice — channels are not thread-safe).
- **Routing**: Publishes to a configured exchange with the topic template as routing key.
- **Publisher confirms**: Enabled for reliability.

#### Console Publisher

Development/testing sink. Writes `[topic] {json_payload}` to stdout. Zero dependencies, instant feedback.

> **Phase 1 status:** Only the Console publisher is implemented. MQTT, HTTP, and AMQP adapters are Phase 3.

#### Protocol Factory

```go
func NewPublisher(protocol string, config map[string]any) (Publisher, error)
```

Maps protocol names to implementations. Protocol configuration is drawn from a combination of global config and per-device-profile overrides. Currently `publisherForProtocol` always returns a `ConsolePublisher` regardless of the protocol field; routing to real adapters is a Phase 3 task.

### 5.5 Scenario Engine

> **Phase 3+ (not yet implemented).** The `ScenarioContext`, `ScenarioRunner`, and `SimClock` classes described below are the design target. The Phase 1 orchestrator CLI exposes `spawn`, `stop`, `status`, and `stream` commands that can be composed manually or from scripts.

Scenarios are Python async functions that choreograph fleet behaviour over time. They are the primary interface for test engineers.

**Execution model:**

```python
async def run(ctx: ScenarioContext):
    """
    ctx provides:
      .spawn(profile, count, labels)  → list[device_id]
      .stop(selector, graceful)       → int (count stopped)
      .inject_fault(selector, type, duration, params)
      .wait(duration)                 → async sleep (respects sim clock)
      .log(message)                   → structured scenario event log
      .status()                       → FleetStatus
      .client                         → raw RuntimeClient for advanced use
    """
```

**Example scenarios:**

```python
# Gradual ramp-up with steady state and failure injection
async def run(ctx: ScenarioContext):
    for batch in range(100):
        await ctx.spawn("temperature_sensor", count=100,
                        labels={"batch": str(batch)})
        await ctx.wait("3s")

    await ctx.log("Steady state: 10,000 devices running")
    await ctx.wait("10m")

    await ctx.inject_fault(
        selector=LabelSelector("batch=0"),  # first 100 devices
        fault=FaultType.DISCONNECT,
        duration="60s"
    )
    await ctx.wait("2m")
    await ctx.log("Scenario complete")
```

```python
# Rolling firmware update simulation
async def run(ctx: ScenarioContext):
    ids = await ctx.spawn("smart_lock", count=5000)
    await ctx.wait("5m")

    for batch in chunk(ids, size=100):
        for device_id in batch:
            await ctx.inject_fault(
                selector=DeviceIds([device_id]),
                fault=FaultType.DISCONNECT,
                duration="30s",
                params={"reason": "firmware_update"}
            )
        await ctx.wait("2m")  # stagger batches
```

**Simulation clock:** The `SimClock` class supports accelerated time. At `speed_multiplier=60`, a `ctx.wait("1h")` call sleeps for 1 real minute. The clock's current time is passed to the Go runtime via gRPC metadata so that generators use simulated time for diurnal cycles and time-correlated patterns.

### 5.6 Service Contract (gRPC/Protobuf)

The `.proto` files are the single source of truth for all inter-service communication. Code is generated for both Go and Python using Buf.

#### Service Definition

```protobuf
service DeviceRuntimeService {
  // Lifecycle
  rpc SpawnDevices(SpawnDevicesRequest)       returns (SpawnDevicesResponse);
  rpc StopDevices(StopDevicesRequest)         returns (StopDevicesResponse);
  rpc GetFleetStatus(GetFleetStatusRequest)   returns (FleetStatus);

  // Real-time control
  rpc InjectFault(InjectFaultRequest)         returns (google.protobuf.Empty);
  rpc UpdateDeviceBehavior(UpdateDeviceBehaviorRequest) returns (google.protobuf.Empty);

  // Observation — server-streaming from Go → Python
  rpc StreamTelemetry(StreamTelemetryRequest) returns (stream TelemetryBatch);
  rpc StreamEvents(StreamEventsRequest)       returns (stream DeviceEvent);

  // Health
  rpc GetRuntimeStatus(google.protobuf.Empty) returns (RuntimeStatus);
}
```

#### Key Contract Design Decisions

**`google.protobuf.Struct` for behaviour params** — Scenario configs are user-defined dicts (Gaussian params, diurnal configs, etc.). Using `Struct` avoids creating a proto message for every generator variant and decouples the contract from scenario-layer implementation details. The tradeoff is lost type safety on those fields — acceptable because they are validated at runtime by the generator factory.

**Batched telemetry streaming** — `StreamTelemetry` returns `stream TelemetryBatch`, not `stream TelemetryPoint`. At 100K devices × 1 msg/sec, per-message gRPC overhead (framing, headers) would dominate. Batching at 100–500 points with a configurable flush interval bounds both latency and memory.

**Label selectors over device ID lists** — Bulk operations (stop, fault inject) accept a `DeviceSelector` oneof with both `device_ids` and `label_selector`. For fleet-wide operations, a selector like `device_type=temperature_sensor` avoids serialising 50K IDs.

**Server-streaming, not bidirectional** — The orchestrator doesn't need to send messages mid-telemetry-stream; it uses separate unary RPCs for control. This simplifies concurrency on both sides.

**Versioned package path** — `simulator.v1` enables non-breaking evolution. Breaking changes create a `v2` package. Buf CI enforces accidental break detection on every PR.

#### Error Handling Across the Boundary

| Situation | gRPC Status Code | Handler |
|-----------|-----------------|---------|
| Device ID already exists | `ALREADY_EXISTS` | Orchestrator retries with a different ID |
| Invalid scenario params | `INVALID_ARGUMENT` | Orchestrator fixes config before retry |
| Runtime at capacity | `RESOURCE_EXHAUSTED` | Orchestrator routes to another instance |
| Device crashed mid-run | `INTERNAL` (via DeviceEvent stream) | Orchestrator decides: restart or alert |
| Network partition | `UNAVAILABLE` | Client-side retry with exponential backoff |

#### Interceptor Stack

**Go server side:**
```
Request → Recovery (panic → INTERNAL) → Logging (zerolog) → Metrics (Prometheus histogram) → Handler
```

**Python client side:**
```
Request → Request-ID injection → Retry policy (UNAVAILABLE, 3 attempts, exp backoff) → Channel
```

---

## 6. Data Models

### 6.1 Device Profile (YAML → Protobuf)

```yaml
# profiles/temperature_sensor.yaml  (Phase 1 — console + gaussian/static generators)
type: temperature_sensor
protocol: console
topic_template: "devices/{device_id}/telemetry"
telemetry_interval: 5s
telemetry_fields:
  temperature:
    type: gaussian
    mean: 22.0
    stddev: 1.0
  humidity:
    type: gaussian
    mean: 55.0
    stddev: 5.0
  battery:
    type: static
    value: 100.0
labels:
  category: environmental
  firmware: "1.2.0"
```

> **Planned (Phase 2+):** Richer profiles using `diurnal`, `brownian`, and `markov` generators, and `mqtt` protocol once Phase 3 adapters land:
>
> ```yaml
> # profiles/temperature_sensor_full.yaml  (target design)
> type: temperature_sensor
> protocol: mqtt
> topic_template: "devices/{device_id}/telemetry"
> telemetry_interval: 5s
> telemetry_fields:
>   temperature:
>     type: diurnal
>     baseline: 22.0
>     amplitude: 5.0
>     peak_hour: 14
>     noise_stddev: 0.3
>   humidity:
>     type: brownian
>     start: 55.0
>     drift: 0
>     volatility: 0.5
>     mean_reversion: 0.1
>     mean: 55.0
>     min: 20.0
>     max: 95.0
>   battery:
>     type: brownian
>     start: 100.0
>     drift: -0.001
>     volatility: 0.0
>     mean_reversion: 0.0
>     mean: 0.0
>     min: 0.0
>     max: 100.0
> labels:
>   category: environmental
>   firmware: "1.2.0"
> ```

### 6.2 Telemetry Payload (JSON, as published to brokers)

The current Phase 1 payload is a flat JSON object with `device_id`, `timestamp`, and one key per telemetry field:

```json
{
  "device_id": "temperature_sensor-0001",
  "timestamp": "2026-03-14T10:23:45.123456789Z",
  "temperature": 23.14,
  "humidity": 57.82,
  "battery": 100.0
}
```

> **Planned refinement:** A structured envelope with top-level `device_type` and a nested `fields` map is intended for Phase 2, along with label propagation:
>
> ```json
> {
>   "device_id": "temperature_sensor-0001",
>   "device_type": "temperature_sensor",
>   "timestamp": "2026-03-14T10:23:45.123Z",
>   "fields": {
>     "temperature": 24.7,
>     "humidity": 53.2,
>     "battery": 87.3
>   },
>   "labels": {
>     "category": "environmental",
>     "firmware": "1.2.0"
>   }
> }
> ```

### 6.3 Device Event (internal, over gRPC stream)

```json
{
  "device_id": "temp-sensor-00042",
  "event_type": "FAULT_INJECTED",
  "message": "Disconnect fault for 60s",
  "timestamp": "2026-03-14T10:30:00.000Z",
  "metadata": {
    "fault_type": "DISCONNECT",
    "duration_seconds": "60"
  }
}
```

### 6.4 Runtime Status (health check response)

```json
{
  "active_devices": 100000,
  "goroutine_count": 100247,
  "messages_sent_total": 5423891,
  "messages_per_second": 18420.5,
  "memory_bytes": 524288000,
  "uptime": "1h23m45s"
}
```

---

## 7. Concurrency & Scaling Model

### 7.1 Single-Process Model (1–100K devices)

Each virtual device runs as a single goroutine. The Go scheduler handles multiplexing across available CPU cores.

```
┌──────────────────────────────────────────────────────┐
│                   Go Process                          │
│                                                       │
│  GOMAXPROCS = 8 (8-core machine)                     │
│                                                       │
│  ┌────┐ ┌────┐ ┌────┐ ┌────┐ ┌────┐ ┌────┐ ┌────┐  │
│  │ P0 │ │ P1 │ │ P2 │ │ P3 │ │ P4 │ │ P5 │ │ P6 │  │
│  │LRQ │ │LRQ │ │LRQ │ │LRQ │ │LRQ │ │LRQ │ │LRQ │  │
│  └──┬─┘ └──┬─┘ └──┬─┘ └──┬─┘ └──┬─┘ └──┬─┘ └──┬─┘  │
│     │      │      │      │      │      │      │      │
│  ┌──▼─┐┌──▼─┐┌──▼─┐┌──▼─┐┌──▼─┐┌──▼─┐┌──▼─┐        │
│  │ M0 ││ M1 ││ M2 ││ M3 ││ M4 ││ M5 ││ M6 │        │
│  └────┘└────┘└────┘└────┘└────┘└────┘└────┘          │
│                                                       │
│  100K goroutines distributed across 8 P run queues   │
│  Work stealing balances load automatically            │
│  netpoll handles 100K network connections via epoll   │
└──────────────────────────────────────────────────────┘
```

**Memory budget at 100K devices:**

- Goroutine stacks: 100K × 4 KB = ~400 MB
- Per-device state: 100K × ~1 KB = ~100 MB
- MQTT connection buffers: 100 connections × ~64 KB = ~6.4 MB
- Telemetry fan-in channel: 10K buffer × ~200 B = ~2 MB
- **Total estimate: ~600 MB** (well within a 2 GB container)

**GC tuning:**

- `GOGC=100` (default) or higher to reduce GC frequency at the cost of higher steady-state heap.
- `GOMEMLIMIT=512MiB` (or appropriate for container) to prevent OOM.
- `sync.Pool` for `TelemetryPoint` structs to reduce allocation rate.

### 7.2 Distributed Model (100K–1M+ devices)

Multiple runtime instances, each handling a shard of the device fleet.

```
Orchestrator (Python)
    │
    ├── RuntimeClient[0] → runtime-0:50051  (devices 0–49,999)
    ├── RuntimeClient[1] → runtime-1:50051  (devices 50,000–99,999)
    ├── RuntimeClient[2] → runtime-2:50051  (devices 100,000–149,999)
    └── ...
```

**Sharding strategy:** Consistent hashing on `device_id`. The orchestrator maintains a hash ring of runtime endpoints. `Spawn` requests are routed to the runtime that owns the device's hash range. `Stop` and `InjectFault` with label selectors are fanned out to all instances.

**Telemetry aggregation:** `StreamTelemetry` is opened on each runtime instance. The orchestrator merges streams using asyncio or a dedicated aggregation goroutine.

**Scaling trigger:** When `sim_publish_latency_seconds` p99 exceeds the telemetry interval on any instance, add more instances.

### 7.3 Backpressure

```
Device goroutine
    │
    ▼ (non-blocking send)
┌─────────────────────┐
│ Fan-in channel       │  ← bounded buffer (configurable, default 10K)
│ (ring buffer mode or │
│  slow-down mode)     │
└─────────┬───────────┘
          │
          ▼
  StreamTelemetry RPC
    (batched sends)
          │
          ▼
  Python consumer
```

Two configurable modes per device profile:

- **`drop_oldest`**: When the fan-in channel is full, the oldest point is evicted. Telemetry is lossy but the device never slows down. Metric: `sim_backpressure_drops_total`.
- **`slow_down`**: When the channel is >80% full, the device doubles its tick interval. Restores when <50% full. Metric: `sim_backpressure_slowdowns_total`.

---

## 8. Fault Injection Framework

> **Phase 4 (not yet implemented).** The `InjectFault` and `UpdateDeviceBehavior` RPCs currently return `UNIMPLEMENTED`. The proto contract and fault type enum are defined; the device-side apply logic below is the design target.

Faults modify device behaviour for a bounded duration, then auto-revert.

### 8.1 Fault Types

| Fault | Effect | Parameters |
|-------|--------|-----------|
| `DISCONNECT` | Stops publishing; device stays in memory. Resumes after duration. | `duration` |
| `LATENCY_SPIKE` | Adds delay before each `Publish` call. | `duration`, `latency_ms` |
| `DATA_CORRUPTION` | Wraps generator output: random NaN, zero, or spike. | `duration`, `corruption_rate` (0.0–1.0) |
| `BATTERY_DRAIN` | Overrides battery generator to drain at an accelerated rate. | `duration`, `drain_multiplier` |
| `CLOCK_DRIFT` | Offsets telemetry timestamps by a growing delta. | `duration`, `drift_rate_ms_per_sec` |

### 8.2 Fault Stacking

Multiple faults can be active on a single device simultaneously. They are applied in order of injection. A disconnect fault takes precedence (skips publish entirely regardless of other faults).

### 8.3 Implementation

```go
type ActiveFault struct {
    Type      FaultType
    StartedAt time.Time
    Duration  time.Duration
    Params    map[string]any
}

// Applied in the device's Run loop before each publish
func (d *VirtualDevice) applyFaults(payload []byte, now time.Time) ([]byte, bool) {
    d.mu.RLock()
    defer d.mu.RUnlock()

    shouldPublish := true
    for _, f := range d.Faults {
        if now.After(f.StartedAt.Add(f.Duration)) {
            continue // expired, will be reaped
        }
        switch f.Type {
        case FaultDisconnect:
            shouldPublish = false
        case FaultLatencySpike:
            time.Sleep(time.Duration(f.Params["latency_ms"].(float64)) * time.Millisecond)
        case FaultDataCorruption:
            payload = corruptPayload(payload, f.Params)
        // ...
        }
    }
    return payload, shouldPublish
}
```

---

## 9. Observability

The simulator is instrumented as thoroughly as a production service. This serves two purposes: validating the simulator itself works correctly, and providing templates for monitoring the real IoT platform.

### 9.1 Metrics (Prometheus)

**Exposed on `:9090/metrics` from each runtime instance.**

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `sim_devices_active` | Gauge | `device_type`, `protocol` | Currently running devices |
| `sim_messages_sent_total` | Counter | `device_type`, `protocol`, `status` | Messages published (success/error) |
| `sim_publish_latency_seconds` | Histogram | `device_type`, `protocol` | End-to-end publish time |
| `sim_telemetry_batch_size` | Histogram | — | Points per gRPC batch |
| `sim_device_errors_total` | Counter | `device_type`, `error_type` | Errors by category |
| `sim_goroutines_active` | Gauge | — | `runtime.NumGoroutine()` |
| `sim_memory_alloc_bytes` | Gauge | — | `runtime.MemStats.Alloc` |
| `sim_faults_active` | Gauge | `fault_type` | Currently injected faults |
| `sim_backpressure_drops_total` | Counter | `device_type` | Telemetry points dropped due to full buffer |
| `sim_backpressure_slowdowns_total` | Counter | `device_type` | Devices that slowed tick rate |

### 9.2 Structured Logging (zerolog)

Every device lifecycle event is logged as structured JSON:

```json
{"level":"info","device_id":"temp-042","event":"spawned","device_type":"temperature_sensor","protocol":"mqtt","ts":"2026-03-14T10:00:00Z"}
{"level":"warn","device_id":"temp-042","event":"publish_retry","attempt":2,"error":"connection reset","ts":"2026-03-14T10:05:12Z"}
{"level":"info","device_id":"temp-042","event":"fault_injected","fault":"DISCONNECT","duration":"60s","ts":"2026-03-14T10:10:00Z"}
{"level":"info","device_id":"temp-042","event":"stopped","reason":"graceful","ts":"2026-03-14T10:30:00Z"}
```

Log levels: `DEBUG` (every telemetry point — disabled by default), `INFO` (lifecycle events), `WARN` (retries, backpressure), `ERROR` (unrecoverable failures).

### 9.3 Admin HTTP API

Small HTTP server on `:8080` alongside gRPC on `:50051`.

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/healthz` | GET | Liveness probe (200 if process is alive) |
| `/readyz` | GET | Readiness probe (200 when ready to accept devices) |
| `/metrics` | GET | Prometheus metrics (also on `:9090`) |
| `/api/v1/status` | GET | JSON runtime status |
| `/api/v1/devices` | GET | Device list with `?type=X&state=running` filtering |
| `/api/v1/devices/{id}/pause` | POST | Pause a specific device |
| `/api/v1/devices/{id}/resume` | POST | Resume a paused device |

### 9.4 Grafana Dashboard

Pre-built dashboard with panels for: active devices (gauge by type), messages/sec (graph by protocol), publish latency p50/p95/p99, error rate, memory usage, goroutine count, and active faults.

---

## 10. Deployment Architecture

### 10.1 Local Development (Docker Compose)

```yaml
services:
  runtime:       # Go device runtime
  orchestrator:  # Python CLI + scenarios (optional — can run natively)
  mosquitto:     # MQTT broker
  prometheus:    # Metrics scraping
  grafana:       # Dashboards
```

Single command: `docker compose up -d`. Profiles and scenarios are mounted as volumes.

### 10.2 Production / Cloud-Native (Kubernetes)

```txt
┌─────────────────────────────────────────────────────────┐
│                   Kubernetes Cluster                      │
│                                                           │
│  ┌────────────────────────────────────────┐               │
│  │  Deployment: device-runtime            │               │
│  │  replicas: N (HPA on CPU)              │               │
│  │  resources:                            │               │
│  │    requests: { cpu: 2, mem: 1Gi }      │               │
│  │    limits:   { cpu: 4, mem: 2Gi }      │               │
│  │  env:                                  │               │
│  │    GOGC: "100"                         │               │
│  │    GOMEMLIMIT: "1536MiB"              │               │
│  │  ports: [50051, 8080, 9090]            │               │
│  │  livenessProbe: /healthz               │               │
│  │  readinessProbe: /readyz               │               │
│  └────────────────────────────────────────┘               │
│                                                           │
│  ┌────────────────────────────────────────┐               │
│  │  Job: orchestrator-scenario            │               │
│  │  command: iot-sim scenario run ...     │               │
│  │  restartPolicy: Never                  │               │
│  └────────────────────────────────────────┘               │
│                                                           │
│  ┌──────────────┐ ┌──────────────┐ ┌──────────────┐      │
│  │ ServiceMonitor│ │ Prometheus   │ │ Grafana      │      │
│  │ (scrape      │ │              │ │              │      │
│  │  :9090)      │ │              │ │              │      │
│  └──────────────┘ └──────────────┘ └──────────────┘      │
└─────────────────────────────────────────────────────────┘
```

**HPA scaling:** Scale runtime replicas based on CPU utilisation. When a new replica joins, the orchestrator's consistent-hash ring updates and rebalances device assignments.

### 10.3 Container Images

| Image | Base | Size (est.) | Build |
|-------|------|-------------|-------|
| `iot-sim-runtime` | `alpine:3.19` | ~15 MB | Multi-stage: Go build → scratch/alpine |
| `iot-sim-orchestrator` | `python:3.12-slim` | ~150 MB | pip install from pyproject.toml |

---

## 11. Security Considerations

| Concern | Mitigation |
|---------|-----------|
| gRPC between orchestrator and runtime | mTLS in production. Plaintext acceptable in Docker Compose / local dev. |
| MQTT credentials | Stored in environment variables or K8s secrets. Never in YAML profiles. |
| Admin API exposure | Bind to `127.0.0.1` by default. Expose via K8s Service only to internal namespace. |
| Telemetry data | Simulated data only — no real PII. Label values should not contain sensitive information. |
| Container runtime | Non-root user in Dockerfiles. Read-only filesystem where possible. |
| Supply chain | Pin dependency versions. Use `go.sum` and `poetry.lock`. Dependabot / Renovate for updates. |

---

## 12. Performance Targets & Capacity Planning

### 12.1 Targets

| Metric | Target | Measurement |
|--------|--------|-------------|
| Devices per process | 100,000 | Sustained for 1 hour |
| Messages per second (per process) | 20,000 | At 5s telemetry interval with 100K devices |
| Publish latency p99 | < 50 ms | MQTT QoS 1 to local broker |
| Memory per process | < 2 GB | At 100K devices |
| Device spawn time | < 5 ms per device | Batch of 10K devices |
| Graceful shutdown | < 30 s | All devices stopped, connections drained |
| GC pause p99 | < 5 ms | Under sustained load |

### 12.2 Capacity Planning Formula

```txt
Required runtime instances = ceil(total_devices / devices_per_instance)
Required MQTT connections  = total_devices / devices_per_connection
Required broker throughput = total_devices / avg_telemetry_interval_seconds
```

Example: 500K devices at 5s intervals

- Runtime instances: ceil(500,000 / 100,000) = 5
- MQTT connections: 500,000 / 1,000 = 500
- Broker throughput: 500,000 / 5 = 100,000 msg/s

---

## 13. API Reference

### 13.1 CLI Commands

**Phase 1 (implemented):**

```text
iot-sim spawn  --profile <path> --count <N> [--runtime host:port]
iot-sim stop   --all | --type <device_type>  [--runtime host:port]
iot-sim status [--runtime host:port]
iot-sim stream [--type <device_type>] [--ids id1,id2] [--runtime host:port]
iot-sim serve  [--host 0.0.0.0] [--port 8000] [--reload]
```

**Planned (Phase 3+):**

```text
iot-sim fault inject --type <fault> --selector <label=val> --duration <dur>
iot-sim scenario run <script.py> [--time-multiplier <N>]
iot-sim scenario list
iot-sim runtime status
```

### 13.2 REST API (FastAPI)

The API server is started with `iot-sim serve`. Interactive docs are available at `/docs` (Swagger UI) and `/redoc`.

| Method | Path | Description |
| ------ | ---- | ----------- |
| `POST` | `/api/v1/devices/spawn` | Spawn devices from a profile YAML |
| `POST` | `/api/v1/devices/stop` | Stop devices by type or all |
| `GET` | `/api/v1/devices/status` | Fleet + runtime status |
| `GET` | `/api/v1/devices/stream` | Live telemetry as SSE (`text/event-stream`) |
| `GET` | `/api/v1/health` | Health check |

**Spawn request:**

```json
{ "profile": "profiles/temperature_sensor.yaml", "count": 10, "runtime": "localhost:50051" }
```

**Stop request:**

```json
{ "all": true }
{ "device_type": "temperature_sensor" }
```

**SSE stream event** (`GET /api/v1/devices/stream?device_type=temperature_sensor`):

```text
data: {"device_id": "temperature_sensor-0001", "metric": "temperature", "value": 22.4, "timestamp": "2026-03-14T10:23:45"}
data: {"device_id": "temperature_sensor-0002", "metric": "humidity", "value": 58.1, "timestamp": "2026-03-14T10:23:45"}
```

### 13.3 gRPC Service

See Section 5.6 for the full `DeviceRuntimeService` definition. The complete proto files are in `proto/simulator/v1/`.

---

## 14. Configuration Reference

### 14.1 Runtime Configuration (Go)

```yaml
# runtime-config.yaml
grpc:
  port: 50051
  max_recv_msg_size: 16MB
  keepalive_time: 30s
  keepalive_timeout: 10s

admin:
  port: 8080
  bind: "0.0.0.0"    # "127.0.0.1" for local-only

metrics:
  port: 9090

mqtt:
  default_broker: tcp://localhost:1883
  pool_size: 10
  qos: 1
  keepalive: 30s
  connect_timeout: 10s
  tls:
    enabled: false
    ca_cert: ""
    client_cert: ""
    client_key: ""

http:
  default_endpoint: http://localhost:8888/telemetry
  max_idle_conns: 100
  timeout: 10s
  batch_size: 50

amqp:
  default_url: amqp://guest:guest@localhost:5672/
  exchange: iot.telemetry
  exchange_type: topic

logging:
  level: info        # debug, info, warn, error
  format: json       # json, console

runtime:
  max_devices: 100000
  shutdown_timeout: 30s
  telemetry_buffer_size: 10000
  backpressure_strategy: slow_down   # slow_down, drop_oldest
  master_seed: 0                     # 0 = random; set for deterministic replay

gc:
  gogc: 100
  gomemlimit: 1536MiB
```

**Override hierarchy:** Defaults → config file → environment variables → CLI flags.

Environment variable mapping: `IOT_SIM_GRPC_PORT=50051`, `IOT_SIM_MQTT_DEFAULT_BROKER=tcp://...`, etc.

### 14.2 Orchestrator Configuration (Python)

```yaml
# orchestrator-config.yaml
runtimes:
  - host: localhost
    port: 50051
  # Add more for distributed mode

profiles_dir: ./profiles
scenarios_dir: ./scenarios

clock:
  speed_multiplier: 1.0
  start_time: null           # null = current wall-clock time

grpc_client:
  timeout: 30s
  max_retries: 3
  backoff_initial: 100ms
  backoff_max: 5s
  backoff_multiplier: 2
```

---

## 15. Risks & Mitigations

| Risk | Impact | Likelihood | Mitigation |
|------|--------|-----------|-----------|
| MQTT broker becomes bottleneck before simulator does | Cannot validate simulator scale | Medium | Benchmark broker independently. Use clustered broker (EMQX, HiveMQ) for scale tests. |
| Deterministic replay breaks when generators change | Replay tests fail; regression debugging harder | Medium | Version generators. Seed + generator version = replay contract. |
| Goroutine leak on error paths | Memory growth, eventual OOM | Medium | Context cancellation discipline. `goleak` in tests. Monitor `sim_goroutines_active`. |
| Python orchestrator becomes bottleneck at scale | Cannot manage 1M+ device fleet | Low | Orchestrator is control plane only. Batch RPCs. Async client. |
| Simulated time drift between Python clock and Go runtime | Diurnal patterns misaligned | Medium | Clock sync via gRPC metadata on every RPC. Heartbeat validation. |
| Proto contract breaks silently | Runtime errors in production | Low | Buf breaking-change detection in CI. Integration tests on every PR. |

---

## 16. Decision Log

| ID | Decision | Rationale | Alternatives Considered |
|----|----------|-----------|------------------------|
| D1 | Python for orchestrator, Go for runtime | Python: expressiveness for config/scenarios. Go: goroutine-per-device model, epoll netpoll, per-P mcache. | All-Go (loses Python scripting ergonomics), All-Python (asyncio doesn't scale to 100K concurrent connections as cleanly) |
| D2 | gRPC over REST for inter-service communication | Schema-first contract, HTTP/2 streaming, binary encoding, generated code in both languages | REST+JSON (no streaming, no codegen), raw TCP (too low-level) |
| D3 | `google.protobuf.Struct` for behaviour params | Decouples proto contract from generator internals. Python dicts map naturally. | Typed proto messages per generator (too many messages, tight coupling) |
| D4 | Batched telemetry streaming | 100K msg/s with per-message gRPC framing is prohibitive. Batching amortises overhead. | Per-point streaming (too much overhead), polling (too much latency) |
| D5 | Monorepo with shared proto dir | Two services, one team, tight iteration. Simplest CI/CD. | Separate proto repo (overhead not justified at this scale) |
| D6 | Consistent hashing for device sharding | Even distribution, minimal reassignment when instances change | Round-robin (uneven under failures), manual ranges (operational burden) |
| D7 | Deterministic seeding per device per field | Enables byte-identical replay for debugging | Global RNG (not reproducible), no seeding (lose replay capability) |
| D8 | Buf over raw protoc | Dependency management, linting, breaking-change detection in CI | protoc + manual scripts (fragile, no breaking-change checks) |
| D9 | Server-streaming (not bidi) for telemetry | Orchestrator doesn't need to send mid-stream. Simpler concurrency model. | Bidi streaming (unnecessary complexity for this use case) |
| D10 | Console publisher as first adapter | Fastest feedback loop during development. Zero external dependencies. | MQTT-first (requires broker setup before anything works) |

---

## 17. Glossary

| Term | Definition |
|------|-----------|
| **Virtual device** | A goroutine that simulates one IoT device: generates telemetry, publishes via a protocol adapter. |
| **Device profile** | A YAML configuration that defines a device type's telemetry fields, generators, protocol, and publish interval. |
| **Generator** | A pluggable component that produces the next value for one telemetry field (e.g., Gaussian, Brownian, Diurnal). |
| **Publisher** | A protocol adapter that sends telemetry payloads to an external system (MQTT broker, HTTP endpoint, etc.). |
| **Scenario** | A Python async function that choreographs fleet behaviour over time — spawning, stopping, and injecting faults. |
| **Fault injection** | Temporarily modifying a device's behaviour to simulate real-world failures (disconnect, latency, corruption). |
| **Fan-in channel** | A bounded Go channel that collects telemetry points from all device goroutines for gRPC streaming. |
| **Label selector** | A string expression (e.g., `device_type=temperature_sensor`) used to target a subset of devices for bulk operations. |
| **Simulation clock** | A clock with configurable speed multiplier, enabling accelerated-time scenarios (e.g., 24 simulated hours in 10 real minutes). |
| **Master seed** | An integer seed that deterministically derives per-device and per-field seeds for reproducible simulation runs. |
| **Backpressure** | Flow control mechanism that prevents the simulator from overwhelming downstream systems or its own buffers. |
| **GMP model** | Go's scheduler architecture: Goroutines (G), OS threads (M), logical processors (P). |
| **netpoll** | Go's runtime network poller (epoll on Linux) that parks goroutines waiting on I/O without consuming OS threads. |
| **Buf** | A build tool for Protocol Buffers that provides linting, breaking-change detection, and code generation. |
