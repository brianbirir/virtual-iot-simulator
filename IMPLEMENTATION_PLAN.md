# IoT Device Simulator — AI Agent Implementation Strategy

> **Purpose**: Step-by-step implementation plan designed to be executed by an AI coding agent. Each task specifies context, inputs, outputs, acceptance criteria, and ordering constraints so the agent can work autonomously with minimal ambiguity.

---

## 1. Architecture Summary

```
┌─────────────────────────────────────────────────────────────┐
│                SIMULATION ORCHESTRATOR (Python)              │
│  Config management · Fleet lifecycle · Scenario scripting    │
└──────────┬──────────────────────────────┬───────────────────┘
           │ gRPC (protobuf v1)           │ gRPC (protobuf v1)
   ┌───────▼────────┐          ┌──────────▼──────────┐
   │ DEVICE REGISTRY │          │   SCENARIO ENGINE    │
   │ YAML profiles   │          │   Fault injection    │
   │ Fleet topology   │         │   Traffic patterns   │
   └───────┬─────────┘          └──────────┬──────────┘
           │                               │
   ┌───────▼───────────────────────────────▼──────────┐
   │            DEVICE RUNTIME (Go)                    │
   │  1 goroutine per device · Protocol adapters       │
   │  Data generators · Telemetry batching             │
   │  ┌────────┐ ┌────────┐ ┌────────┐                │
   │  │Dev 1   │ │Dev 2   │ │Dev N   │                │
   │  └────┬───┘ └────┬───┘ └────┬───┘                │
   └───────┼──────────┼──────────┼────────────────────┘
           │          │          │
   ┌───────▼──────────▼──────────▼────────────────────┐
   │          PROTOCOL ADAPTERS                        │
   │        MQTT  │  AMQP  │  HTTP  │  console        │
   └──────────────────────────────────────────────────┘
```

**Language split rationale**:
- **Python (orchestrator)**: Expressiveness for config, scenarios, and glue. Not on the hot path.
- **Go (device runtime)**: Goroutine-per-device model scales to 100K+ concurrent devices per process. Go's GMP scheduler, epoll-backed netpoll, and per-P mcache give near-linear core scaling with minimal memory overhead (~4KB/goroutine).

---

## 2. Repository Structure

```
iot-simulator/
├── proto/
│   └── simulator/
│       └── v1/
│           ├── common.proto
│           ├── device.proto
│           ├── telemetry.proto
│           ├── orchestrator.proto
│           └── scenario.proto
├── device-runtime/                  # Go module
│   ├── cmd/
│   │   └── runtime/
│   │       └── main.go
│   ├── internal/
│   │   ├── device/
│   │   │   ├── device.go            # VirtualDevice struct + Run loop
│   │   │   ├── manager.go           # Fleet lifecycle (spawn, stop, query)
│   │   │   └── registry.go          # In-memory device index
│   │   ├── generator/
│   │   │   ├── generator.go         # Generator interface
│   │   │   ├── gaussian.go
│   │   │   ├── brownian.go
│   │   │   ├── diurnal.go
│   │   │   ├── markov.go
│   │   │   └── factory.go           # Config → Generator mapping
│   │   ├── protocol/
│   │   │   ├── publisher.go          # Publisher interface
│   │   │   ├── mqtt.go
│   │   │   ├── http.go
│   │   │   ├── amqp.go
│   │   │   └── console.go           # Stdout sink for testing
│   │   ├── server/
│   │   │   ├── grpc.go              # gRPC service impl
│   │   │   └── interceptors.go
│   │   └── metrics/
│   │       └── prometheus.go
│   ├── go.mod
│   └── go.sum
├── orchestrator/                    # Python package
│   ├── orchestrator/
│   │   ├── __init__.py
│   │   ├── cli.py                   # CLI entrypoint
│   │   ├── config.py                # Profile + scenario loading
│   │   ├── fleet.py                 # Fleet management via gRPC
│   │   ├── scenario_runner.py       # Scenario execution engine
│   │   └── grpc_client.py           # Typed gRPC client wrapper
│   ├── scenarios/
│   │   ├── ramp_up.py
│   │   ├── rolling_update.py
│   │   └── chaos.py
│   ├── pyproject.toml
│   └── tests/
├── profiles/                        # Device profile YAML definitions
│   ├── temperature_sensor.yaml
│   ├── smart_lock.yaml
│   └── industrial_valve.yaml
├── deployments/
│   ├── docker-compose.yaml
│   ├── Dockerfile.runtime
│   ├── Dockerfile.orchestrator
│   └── k8s/
│       ├── runtime-deployment.yaml
│       └── orchestrator-job.yaml
├── buf.yaml
├── buf.gen.yaml
├── Makefile
└── README.md
```

---

## 3. Implementation Phases

The work is divided into 7 phases. Each phase is self-contained and produces a testable artifact. Phases are sequential — each depends on the prior phase being complete.

---

### PHASE 0: Project Scaffolding

**Goal**: Monorepo with build tooling, CI skeleton, and both language environments bootstrapped.

#### Task 0.1: Initialize Repository

```
Context: Greenfield monorepo. Two languages, shared proto definitions.
```

**Steps**:
1. Create the directory structure from Section 2.
2. Initialize Go module: `cd device-runtime && go mod init github.com/virtual-iot-simulator/device-runtime`
3. Initialize Python project: `cd orchestrator && uv init` with `pyproject.toml`
4. Create `buf.yaml` at repo root:
   ```yaml
   version: v2
   modules:
     - path: proto
   lint:
     use:
       - STANDARD
   breaking:
     use:
       - FILE
   ```
5. Create `buf.gen.yaml`:
   ```yaml
   version: v2
   plugins:
     - remote: buf.build/protocolbuffers/go
       out: device-runtime/gen/go
       opt: paths=source_relative
     - remote: buf.build/grpc/go
       out: device-runtime/gen/go
       opt:
         - paths=source_relative
         - require_unimplemented_servers=false
     - remote: buf.build/protocolbuffers/python
       out: orchestrator/gen/python
     - remote: buf.build/grpc/python
       out: orchestrator/gen/python
   ```
6. Create `Makefile` with targets:
   - `proto-gen`: runs `buf generate`
   - `proto-lint`: runs `buf lint`
   - `proto-breaking`: runs `buf breaking --against .git#branch=main`
   - `go-build`: builds device-runtime binary
   - `go-test`: runs Go tests
   - `py-test`: runs Python tests
   - `docker-build`: builds both Docker images
   - `all`: lint + generate + build + test

**Acceptance criteria**:
- `make proto-gen` succeeds and produces generated code in `device-runtime/gen/go/` and `orchestrator/gen/python/`.
- `make go-build` produces a binary.
- `make py-test` runs (even if no tests yet).
- `.gitignore` excludes `gen/` directories (they're build artifacts).

#### Task 0.2: Define Proto Contracts

```
Context: These protos are the single source of truth for all Python ↔ Go communication.
Prior decisions: Batched telemetry streaming, label selectors for bulk ops,
google.protobuf.Struct for flexible behavior params, bidirectional streaming for telemetry.

File split:
  common.proto    — DeviceState enum (shared by all files)
  device.proto    — DeviceProfile, DeviceSpec (device identity + spawn config)
  telemetry.proto — TelemetryPoint, TelemetryBatch (data types)
  orchestrator.proto — DeviceRuntimeService + all request/response messages
  scenario.proto  — Placeholder; reserved for scenario event types (Phase 4)
```

**File: `proto/simulator/v1/common.proto`**
```protobuf
syntax = "proto3";
package simulator.v1;
option go_package = "github.com/virtual-iot-simulator/device-runtime/gen/go/simulator/v1;simulatorv1";

enum DeviceState {
  DEVICE_STATE_UNSPECIFIED = 0;
  DEVICE_STATE_IDLE = 1;
  DEVICE_STATE_RUNNING = 2;
  DEVICE_STATE_ERROR = 3;
  DEVICE_STATE_STOPPED = 4;
}
```

**File: `proto/simulator/v1/device.proto`**
```protobuf
syntax = "proto3";
package simulator.v1;
option go_package = "github.com/virtual-iot-simulator/device-runtime/gen/go/simulator/v1;simulatorv1";

import "google/protobuf/duration.proto";
import "google/protobuf/struct.proto";
import "simulator/v1/common.proto";

message DeviceProfile {
  string device_id = 1;
  string device_type = 2;
  map<string, string> labels = 3;
  DeviceState state = 4;
}

message DeviceSpec {
  string device_id = 1;
  string device_type = 2;
  map<string, string> labels = 3;
  google.protobuf.Duration telemetry_interval = 4;
  google.protobuf.Struct behavior_params = 5;
  string protocol = 6;
  string topic_template = 7;
}
```

**File: `proto/simulator/v1/telemetry.proto`**
```protobuf
syntax = "proto3";
package simulator.v1;
option go_package = "github.com/virtual-iot-simulator/device-runtime/gen/go/simulator/v1;simulatorv1";

import "google/protobuf/timestamp.proto";

message TelemetryPoint {
  string device_id = 1;
  string metric_name = 2;
  oneof value {
    double double_value = 3;
    int64 int_value = 4;
    string string_value = 5;
    bool bool_value = 6;
  }
  google.protobuf.Timestamp timestamp = 7;
  map<string, string> tags = 8;
}

message TelemetryBatch {
  repeated TelemetryPoint points = 1;
  int64 sequence_number = 2;
}
```

**File: `proto/simulator/v1/scenario.proto`**
```protobuf
syntax = "proto3";
package simulator.v1;
option go_package = "github.com/virtual-iot-simulator/device-runtime/gen/go/simulator/v1;simulatorv1";

// Reserved for Phase 4 scenario event types.
// ScenarioEvent and related streaming messages will be defined here.
```

**File: `proto/simulator/v1/orchestrator.proto`**
```protobuf
syntax = "proto3";
package simulator.v1;
option go_package = "github.com/virtual-iot-simulator/device-runtime/gen/go/simulator/v1;simulatorv1";

import "google/protobuf/empty.proto";
import "google/protobuf/duration.proto";
import "google/protobuf/struct.proto";
import "google/protobuf/timestamp.proto";
import "simulator/v1/common.proto";
import "simulator/v1/device.proto";
import "simulator/v1/telemetry.proto";

service DeviceRuntimeService {
  // Lifecycle
  rpc SpawnDevices(SpawnDevicesRequest) returns (SpawnDevicesResponse);
  rpc StopDevices(StopDevicesRequest) returns (StopDevicesResponse);
  rpc GetFleetStatus(GetFleetStatusRequest) returns (FleetStatus);

  // Real-time control
  rpc InjectFault(InjectFaultRequest) returns (google.protobuf.Empty);
  rpc UpdateDeviceBehavior(UpdateDeviceBehaviorRequest) returns (google.protobuf.Empty);

  // Observation
  rpc StreamTelemetry(StreamTelemetryRequest) returns (stream TelemetryBatch);
  rpc StreamEvents(StreamEventsRequest) returns (stream DeviceEvent);

  // Health
  rpc GetRuntimeStatus(google.protobuf.Empty) returns (RuntimeStatus);
}

// --- Request/Response Messages ---
// DeviceSpec and DeviceProfile are defined in device.proto.
// TelemetryBatch is defined in telemetry.proto.

message SpawnDevicesRequest {
  repeated DeviceSpec specs = 1;
  string scenario_id = 2;
}

message SpawnDevicesResponse {
  int32 spawned = 1;
  repeated string failed_device_ids = 2;
  map<string, string> failure_reasons = 3;
}

message DeviceSelector {
  oneof selector {
    DeviceIdList device_ids = 1;
    string label_selector = 2;
  }
}

message DeviceIdList {
  repeated string ids = 1;
}

message StopDevicesRequest {
  DeviceSelector selector = 1;
  bool graceful = 2;
}

message StopDevicesResponse {
  int32 stopped = 1;
  repeated string failed_device_ids = 2;
}

message GetFleetStatusRequest {
  DeviceSelector selector = 1;
}

message FleetStatus {
  int32 total_devices = 1;
  map<string, int32> by_state = 2;
  map<string, int32> by_type = 3;
  repeated DeviceProfile devices = 4;
}

message InjectFaultRequest {
  DeviceSelector selector = 1;
  FaultType fault_type = 2;
  google.protobuf.Duration duration = 3;
  google.protobuf.Struct parameters = 4;
}

enum FaultType {
  FAULT_TYPE_UNSPECIFIED = 0;
  FAULT_TYPE_DISCONNECT = 1;
  FAULT_TYPE_LATENCY_SPIKE = 2;
  FAULT_TYPE_DATA_CORRUPTION = 3;
  FAULT_TYPE_BATTERY_DRAIN = 4;
  FAULT_TYPE_CLOCK_DRIFT = 5;
}

message UpdateDeviceBehaviorRequest {
  DeviceSelector selector = 1;
  google.protobuf.Struct behavior_params = 2;
}

message StreamTelemetryRequest {
  DeviceSelector selector = 1;
  int32 batch_size = 2;
  google.protobuf.Duration flush_interval = 3;
}

message StreamEventsRequest {
  DeviceSelector selector = 1;
}

message DeviceEvent {
  string device_id = 1;
  DeviceEventType event_type = 2;
  string message = 3;
  google.protobuf.Timestamp timestamp = 4;
  map<string, string> metadata = 5;
}

enum DeviceEventType {
  DEVICE_EVENT_TYPE_UNSPECIFIED = 0;
  DEVICE_EVENT_TYPE_SPAWNED = 1;
  DEVICE_EVENT_TYPE_CONNECTED = 2;
  DEVICE_EVENT_TYPE_DISCONNECTED = 3;
  DEVICE_EVENT_TYPE_ERROR = 4;
  DEVICE_EVENT_TYPE_FAULT_INJECTED = 5;
  DEVICE_EVENT_TYPE_STOPPED = 6;
}

message RuntimeStatus {
  int32 active_devices = 1;
  int32 goroutine_count = 2;
  int64 messages_sent_total = 3;
  double messages_per_second = 4;
  int64 memory_bytes = 5;
  google.protobuf.Duration uptime = 6;
}
```

**Acceptance criteria**:
- `buf lint` passes with zero warnings.
- `buf generate` produces compilable Go and Python code.
- Go code compiles: `cd device-runtime && go build ./...`

---

### PHASE 1: Walking Skeleton — Single Device, Console Output

**Goal**: One virtual device runs in a goroutine, generates fake telemetry on a timer, and writes to stdout. The Python orchestrator connects via gRPC and spawns it.

#### Task 1.1: Generator Interface + Gaussian Generator (Go)

```
File: device-runtime/internal/generator/generator.go
```

**Specification**:
```go
// Generator produces the next value for a telemetry field.
// Implementations must be safe for single-goroutine use (no concurrent calls).
type Generator interface {
    // Next produces the next value. `now` is the simulation clock time.
    // `state` is the device's mutable state map (for cross-field dependencies).
    Next(now time.Time, state map[string]any) any
}
```

Implement `GaussianGenerator`:
- Fields: `Mean float64`, `StdDev float64`, `rng *rand.Rand`
- Constructor takes a seed (for deterministic replay): `NewGaussian(mean, stddev float64, seed int64) *GaussianGenerator`
- `Next()` returns `mean + stddev * rng.NormFloat64()`

Implement `StaticGenerator` (returns a fixed value — useful for testing).

**Unit tests**:
- Gaussian output is within 4σ of mean over 10,000 samples.
- Deterministic: same seed → same sequence.
- Static generator always returns configured value.

**Acceptance criteria**: `go test ./internal/generator/...` passes.

#### Task 1.2: Console Publisher (Go)

```
File: device-runtime/internal/protocol/publisher.go
File: device-runtime/internal/protocol/console.go
```

**Specification**:
```go
// Publisher abstracts the telemetry delivery mechanism.
type Publisher interface {
    Publish(ctx context.Context, topic string, payload []byte) error
    Close() error
}
```

`ConsolePublisher` implements `Publisher` by writing JSON to stdout with topic prefix. This is the testing/development sink.

**Acceptance criteria**: Publishing a payload prints `[topic] {"device_id":"d1","temperature":22.5,...}` to stdout.

#### Task 1.3: VirtualDevice Core Loop (Go)

```
File: device-runtime/internal/device/device.go
```

**Specification**:
```go
// DeviceState mirrors the proto enum. Use the proto-generated type:
// simulatorv1.DeviceState (e.g. simulatorv1.DeviceState_DEVICE_STATE_RUNNING).
// A local type alias is NOT used — keep the proto type throughout the runtime.

type VirtualDevice struct {
    ID         string
    DeviceType string
    Labels     map[string]string
    State      simulatorv1.DeviceState  // proto-generated enum
    Generators map[string]Generator     // field_name → generator
    Interval   time.Duration
    Publisher  Publisher
    Topic      string
    // telemetryCh is a write-only reference to the Manager's fan-in channel.
    // Each generated TelemetryPoint is sent here so StreamTelemetry RPCs can
    // observe all devices without intercepting the external Publisher.
    telemetryCh chan<- *simulatorv1.TelemetryPoint
    cancel      context.CancelFunc
    mu          sync.RWMutex             // protects State
}

// Run blocks until context is cancelled. Call in a goroutine.
func (d *VirtualDevice) Run(ctx context.Context) error {
    d.setState(Running)
    ticker := time.NewTicker(d.Interval)
    defer ticker.Stop()
    defer d.setState(Stopped)

    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case t := <-ticker.C:
            payload, points := d.generatePayload(t)
            // Fan-in: non-blocking send to internal telemetry channel (drop if full).
            for _, pt := range points {
                select {
                case d.telemetryCh <- pt:
                default: // back-pressure: drop, increment sim_backpressure_events_total
                }
            }
            if err := d.Publisher.Publish(ctx, d.Topic, payload); err != nil {
                // log error, increment error counter, continue
            }
        }
    }
}
```

`generatePayload(t)` iterates generators, builds a JSON byte slice with `device_id`, `timestamp`, and all field values, and also returns a `[]*simulatorv1.TelemetryPoint` slice (one per field) for the internal fan-in channel.

**Unit tests**:
- Device transitions through IDLE → RUNNING → STOPPED.
- Cancelling context stops the device within one interval.
- Generated payloads contain all configured fields.
- Payload timestamps advance monotonically.

**Acceptance criteria**: `go test ./internal/device/...` passes.

#### Task 1.4: Device Manager (Go)

```
File: device-runtime/internal/device/manager.go
```

**Specification**:
```go
type Manager struct {
    devices map[string]*VirtualDevice   // guarded by mu
    mu      sync.RWMutex
    metrics *Metrics
}

func (m *Manager) Spawn(specs []DeviceSpec) (spawned int, failures map[string]string)
func (m *Manager) Stop(selector DeviceSelector, graceful bool) (stopped int, failures map[string]string)
func (m *Manager) GetStatus(selector DeviceSelector) FleetStatus
func (m *Manager) InjectFault(selector DeviceSelector, fault FaultConfig) error
```

- `Spawn`: For each spec, build generators from behavior_params (via generator factory), create VirtualDevice (passing the Manager's shared `telemetryCh` fan-in channel), launch `device.Run()` in a goroutine.
- `Stop`: Resolve selector → device IDs, call cancel on each.
- Duplicate device_id returns an error for that specific device (doesn't fail the batch).
- **Device ID generation**: Specs arrive from the orchestrator with IDs already set. The Python CLI is responsible for generating IDs before calling `SpawnDevices`. Convention: `{device_type}-{zero_padded_index}` for sequential spawns (e.g., `temperature_sensor-0001`), or `{device_type}-{uuid4_short}` when spawning from a scenario for uniqueness across batches. The Go runtime treats device IDs as opaque strings and never generates them.

**Unit tests**:
- Spawn 100 devices, verify all running.
- Stop by label selector, verify only matching devices stopped.
- Spawn duplicate ID returns failure for that ID, others succeed.

**Acceptance criteria**: `go test ./internal/device/...` passes.

#### Task 1.5: gRPC Server (Go)

```
File: device-runtime/internal/server/grpc.go
File: device-runtime/cmd/runtime/main.go
```

**Specification**:
- Implement `DeviceRuntimeServiceServer` interface from generated proto code.
- Each RPC method delegates to `Manager`.
- `StreamTelemetry`: The `Manager` owns a single `telemetryCh chan *simulatorv1.TelemetryPoint` (buffered, size configurable, default 10,000). Each `VirtualDevice` holds a write-only reference to this channel. The gRPC handler reads from the channel, applies the `DeviceSelector` filter, accumulates points into a `TelemetryBatch` up to `batch_size` or `flush_interval` (whichever comes first), then sends the batch to the client stream. Multiple concurrent `StreamTelemetry` RPCs each get a fan-out copy via a `Broadcaster` struct.
- **The `Broadcaster` is a non-trivial component — implement it explicitly**: `Broadcaster` has `Subscribe() chan *simulatorv1.TelemetryPoint`, `Unsubscribe(ch)`, and a single dispatch goroutine that reads from the Manager's `telemetryCh` and forwards to all subscriber channels (non-blocking per subscriber to prevent one slow client from stalling others). File: `device-runtime/internal/server/broadcaster.go`.
- Register `grpc_health_v1` using `google.golang.org/grpc/health` — required for `grpcurl` healthcheck and Kubernetes readiness probes.
- `main.go` wires everything: create Manager, create gRPC server, register service and health server, listen on `--port` flag (default 50051), handle SIGINT/SIGTERM for graceful shutdown.

**gRPC interceptors** (Task 1.5b):
- Recovery interceptor (panic → INTERNAL status).
- Logging interceptor (zerolog, log method + duration + status).
- Metrics interceptor (Prometheus histogram for RPC latency, counter for RPC calls by method+status).

**Acceptance criteria**:
- Binary starts, binds to port, responds to health check and `GetRuntimeStatus`.
- `grpcurl -plaintext localhost:50051 simulator.v1.DeviceRuntimeService/GetRuntimeStatus` returns valid JSON.
- `grpcurl -plaintext localhost:50051 grpc.health.v1.Health/Check` returns `SERVING`.

#### Task 1.6: Python gRPC Client + CLI (Python)

```
File: orchestrator/orchestrator/grpc_client.py
File: orchestrator/orchestrator/cli.py
```

**Specification**:
- `RuntimeClient` wraps the generated gRPC stub with typed methods: `spawn(specs)`, `stop(selector)`, `status()`, `stream_telemetry(selector, batch_size)`.
- Use `grpc.aio` for async streaming.
- CLI (use `typer`):
  - `iot-sim spawn --profile profiles/temperature_sensor.yaml --count 10`
  - `iot-sim stop --all` / `iot-sim stop --type temperature_sensor`
  - `iot-sim status`
  - `iot-sim stream --type temperature_sensor`

**Acceptance criteria**:
- `iot-sim spawn --profile profiles/temperature_sensor.yaml --count 1` → Go runtime spawns a device → `iot-sim stream` shows telemetry lines arriving.
- End-to-end: Python → gRPC → Go → console publisher → stdout.

---

### PHASE 2: Device Profile System

**Goal**: Rich device profiles defined in YAML drive device behavior. Multiple generator types. Multi-field devices.

#### Task 2.1: Brownian Motion Generator (Go)

```
File: device-runtime/internal/generator/brownian.go
```

**Specification**:
- Implements `Generator`.
- Params: `Start`, `Drift`, `Volatility`, `MeanReversion`, `Mean`, `Min`, `Max`, `Seed`.
- Each call: `next = current + drift*dt + volatility*sqrt(dt)*Z + meanReversion*(mean-current)*dt`, clamped to [Min, Max].
- `dt` is computed internally as `(now - lastCallTime).Seconds()`. On the first call, `dt = interval.Seconds()` (passed at construction). Store `lastCallTime` as a field on the generator.
- Used for slowly drifting sensors (humidity, soil moisture).

**Unit tests**: Output stays within bounds. Mean reversion pulls toward mean over 10,000 steps.

#### Task 2.2: Diurnal Cycle Generator (Go)

```
File: device-runtime/internal/generator/diurnal.go
```

**Specification**:
- Params: `Baseline`, `Amplitude`, `PeakHour` (0-23), `NoiseStdDev`, `Seed`.
- Formula: `baseline + amplitude * sin(2π * (hour - peakHour + 6) / 24) + noise`
- Used for temperature, light level, occupancy patterns.

**Unit tests**: Peak value occurs near PeakHour. Trough occurs ~12h from peak.

#### Task 2.3: Markov State Machine Generator (Go)

```
File: device-runtime/internal/generator/markov.go
```

**Specification**:
- Params: `States []string`, `TransitionMatrix [][]float64`, `InitialState string`, `Seed`.
- `Next()` returns current state string, then transitions based on probability row.
- Used for discrete-state devices (locks, doors, valves).

**Unit tests**: State distribution converges to stationary distribution over many steps.

#### Task 2.4: Generator Factory (Go)

```
File: device-runtime/internal/generator/factory.go
```

**Specification**:
```go
// NewFromConfig creates a Generator from a proto Struct's fields.
// The "type" key determines the generator: "gaussian", "brownian", "diurnal", "markov", "static".
func NewFromConfig(config map[string]any, seed int64) (Generator, error)
```

- Validates required params per type. Returns descriptive errors.
- Seeds are derived: `baseSeed XOR hash(fieldName)` for per-field determinism.
- **`google.protobuf.Struct` access in Go is verbose**: fields are accessed via `config["type"].GetStringValue()` after asserting through `structpb.Value`. To avoid this throughout the factory, convert the proto Struct to `map[string]any` once at entry using a helper: `func structToMap(s *structpb.Struct) map[string]any`. Then work with the plain map internally.

**Unit tests**: Each generator type round-trips through config. Unknown type returns error.

#### Task 2.5: YAML Profile Loader (Python)

```
File: orchestrator/orchestrator/config.py
```

**Specification**:
- Loads YAML files from `profiles/` directory.
- Validates schema (use Pydantic models).
- Converts to `DeviceSpec` proto messages with behavior_params as `google.protobuf.Struct`.

**Profile schema**:
```yaml
# profiles/temperature_sensor.yaml
type: temperature_sensor
protocol: mqtt
topic_template: "devices/{device_id}/telemetry"
telemetry_interval: 5s
telemetry_fields:
  temperature:
    type: diurnal
    baseline: 22.0
    amplitude: 5.0
    peak_hour: 14
    noise_stddev: 0.3
  humidity:
    type: brownian
    start: 55.0
    drift: 0
    volatility: 0.5
    mean_reversion: 0.1
    mean: 55.0
    min: 20.0
    max: 95.0
  battery:
    type: brownian
    start: 100.0
    drift: -0.001
    volatility: 0.0
    mean_reversion: 0.0
    mean: 0.0
    min: 0.0
    max: 100.0
labels:
  category: environmental
  firmware: "1.2.0"
```

**Acceptance criteria**:
- `iot-sim spawn --profile profiles/temperature_sensor.yaml --count 50` spawns 50 devices.
- Each device generates temperature (diurnal), humidity (brownian), and battery (linear decay) values.
- Telemetry stream shows realistic multi-field data.

---

### PHASE 3: Protocol Adapters

**Goal**: Devices publish telemetry over real IoT protocols to actual brokers.

#### Task 3.1: MQTT Publisher (Go)

```
File: device-runtime/internal/protocol/mqtt.go
```

**Specification**:
- Uses `github.com/eclipse/paho.golang` (MQTT v5). Do not use the v1 `paho.mqtt.golang` library — it is in maintenance mode.
- Constructor: `NewMQTTPublisher(brokerURL, clientIDPrefix string, opts MQTTOptions) (*MQTTPublisher, error)`
- `MQTTOptions`: QoS (0/1/2), TLS config, credentials, keepalive, clean session.
- Connection pool: Maintain N connections (configurable). Devices are assigned to connections via `hash(deviceID) % N`.
- Reconnect logic: Automatic with exponential backoff + jitter.
- Publishes JSON payload to resolved topic (replace `{device_id}` in template).

**Integration test** (requires mosquitto or test container):
- Publish 1000 messages, verify all received by subscriber.
- Kill broker, verify reconnect and resume.

#### Task 3.2: HTTP Publisher (Go)

```
File: device-runtime/internal/protocol/http.go
```

**Specification**:
- POSTs JSON payload to configured endpoint URL.
- Uses `http.Client` with connection pooling (`MaxIdleConns`, `MaxIdleConnsPerHost`).
- Retry with exponential backoff on 5xx/timeout.
- Batch mode: accumulate N payloads, POST as JSON array.

#### Task 3.3: AMQP Publisher (Go)

```
File: device-runtime/internal/protocol/amqp.go
```

**Specification**:
- Uses `github.com/rabbitmq/amqp091-go`.
- Publishes to configured exchange with routing key from topic template.
- Channel pooling (one channel per goroutine is AMQP best practice).
- Publisher confirms for reliability.

#### Task 3.4: Protocol Factory (Go)

```
File: device-runtime/internal/protocol/factory.go
```

**Specification**:
```go
func NewPublisher(protocol string, config map[string]any) (Publisher, error)
```

Maps `"mqtt"` → MQTTPublisher, `"http"` → HTTPPublisher, `"amqp"` → AMQPPublisher, `"console"` → ConsolePublisher.

Config passed from DeviceSpec's behavior_params or a global protocol config section.

**Acceptance criteria**:
- `iot-sim spawn --profile profiles/temperature_sensor.yaml --count 100` with protocol `mqtt` → 100 devices publishing to a Mosquitto broker.
- `mosquitto_sub -t "devices/#"` shows telemetry from all 100 devices.

---

### PHASE 4: Scenario Engine

**Goal**: Python-scripted scenarios drive fleet behavior over time — ramp-ups, failures, firmware updates.

#### Task 4.1: Scenario Runner Framework (Python)

```
File: orchestrator/orchestrator/scenario_runner.py
```

**Specification**:
```python
class ScenarioContext:
    """Injected into every scenario function."""
    def __init__(self, client: RuntimeClient, clock: SimClock, profiles: ProfileRegistry):
        self.client = client
        self.clock = clock
        self.profiles = profiles

    async def spawn(self, profile_name: str, count: int, label_overrides: dict = None) -> list[str]:
        """Spawn devices, return device IDs."""

    async def stop(self, selector: DeviceSelector, graceful: bool = True) -> int:
        """Stop devices matching selector."""

    async def inject_fault(self, selector: DeviceSelector, fault: FaultType, duration: str, params: dict = None):
        """Inject a fault into matching devices."""

    async def wait(self, duration: str):
        """Wait for a duration (real-time or sim-time)."""

    async def log(self, message: str):
        """Log a scenario event."""
```

**Scenario file convention**:
```python
# scenarios/ramp_up.py
async def run(ctx: ScenarioContext):
    """Gradually ramp up 10,000 temperature sensors over 5 minutes."""
    for batch_num in range(100):
        ids = await ctx.spawn("temperature_sensor", count=100)
        ctx.log(f"Batch {batch_num}: spawned {len(ids)} devices")
        await ctx.wait("3s")

    ctx.log("All devices spawned. Running steady state for 10 minutes.")
    await ctx.wait("10m")

    ctx.log("Injecting network faults on 10% of fleet.")
    await ctx.inject_fault(
        selector=LabelSelector("device_type=temperature_sensor"),
        fault=FaultType.DISCONNECT,
        duration="30s",
    )
    await ctx.wait("2m")
    ctx.log("Scenario complete.")
```

**CLI integration**:
- `iot-sim scenario run scenarios/ramp_up.py`
- `iot-sim scenario list`

**Additional scenario files** (implement alongside `ramp_up.py` — all follow the same `async def run(ctx)` convention):

`scenarios/rolling_update.py` — simulate a firmware rollout: stop devices in batches of 10%, re-spawn with updated label `firmware: "1.3.0"`, verify fleet reaches 100% on new firmware.

`scenarios/chaos.py` — randomly inject faults across the fleet: pick 20% of running devices at random, cycle through DISCONNECT / DATA_CORRUPTION / LATENCY_SPIKE faults with random durations, observe recovery.

#### Task 4.2: Simulation Clock (Python)

```
File: orchestrator/orchestrator/clock.py
```

**Specification**:
```python
class SimClock:
    def __init__(self, speed_multiplier: float = 1.0, start_time: datetime = None):
        """
        speed_multiplier=1.0: real-time
        speed_multiplier=60.0: 1 real second = 1 simulated minute
        """

    def now(self) -> datetime:
        """Current simulation time."""

    async def sleep(self, duration: timedelta):
        """Sleep for simulated duration (real sleep = duration / speed)."""
```

**Sim-time integration with Go runtime**:
- The orchestrator sends a `sim-clock-epoch` gRPC metadata header on every `SpawnDevices` call containing the simulation start time (RFC3339) and `sim-clock-speed` (float64 multiplier).
- The Go runtime stores these in a `RuntimeClock` struct (created once at startup, updated on each spawn). `RuntimeClock.Now()` computes `simStart + (realElapsed * speedMultiplier)`.
- `VirtualDevice.generatePayload(t)` calls `runtimeClock.Now()` instead of `time.Now()` for the telemetry timestamp, and passes it to generators as the `now` argument.
- When `speed_multiplier == 1.0` (default), `RuntimeClock.Now()` is equivalent to `time.Now()`.
- **Known limitation**: Sending the epoch on each `SpawnDevices` call means devices spawned in separate batches during a single scenario may have slightly different time bases (due to real-time elapsed between batches). Acceptable for Phase 4. If this causes observable drift, add a `SetClock(SimClockRequest)` RPC to `DeviceRuntimeService` that initializes the clock once before any spawning — record this decision in `DECISIONS.md` if the limitation is hit during implementation.
- `iot-sim scenario run scenarios/ramp_up.py --time-multiplier 60` runs 60x faster.

#### Task 4.3: Fault Injection in Go Runtime

```
File: device-runtime/internal/device/fault.go
```

**Specification**:
- `DISCONNECT`: Cancel device's publisher, pause telemetry. Resume after duration.
- `LATENCY_SPIKE`: Add configurable delay before each Publish call.
- `DATA_CORRUPTION`: Wrap generator output with random perturbation (NaN, zero, spike).
- `BATTERY_DRAIN`: Override battery generator to drain 10x faster.
- `CLOCK_DRIFT`: Offset timestamps by a growing delta.

Faults are time-bounded (auto-revert after duration). Multiple faults can stack on one device.

**Unit tests**: Each fault type modifies behavior as expected and auto-reverts.

---

### PHASE 5: Observability & Metrics

**Goal**: The simulator itself is fully observable — you can monitor it like a production system.

#### Task 5.1: Prometheus Metrics (Go)

```
File: device-runtime/internal/metrics/prometheus.go
```

**Specification**:

Prometheus metrics are served on the admin HTTP server (`:8080/metrics`, configurable via `--admin-port`). There is no separate metrics port. The docker-compose `9090` mapping is for the Prometheus scraper service itself — not the runtime. The runtime exposes only two ports: `50051` (gRPC) and `8080` (admin/metrics).

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `sim_devices_active` | Gauge | `device_type`, `protocol` | Currently running devices |
| `sim_messages_sent_total` | Counter | `device_type`, `protocol`, `status` | Total messages published |
| `sim_publish_latency_seconds` | Histogram | `device_type`, `protocol` | Time to publish one message |
| `sim_telemetry_batch_size` | Histogram | — | Points per streamed batch |
| `sim_device_errors_total` | Counter | `device_type`, `error_type` | Publish failures, timeouts |
| `sim_goroutines_active` | Gauge | — | runtime.NumGoroutine() |
| `sim_memory_alloc_bytes` | Gauge | — | runtime.MemStats.Alloc |
| `sim_faults_active` | Gauge | `fault_type` | Currently active faults |

#### Task 5.2: Structured Logging (Go)

```
File: device-runtime/internal/logging/logger.go
```

**Specification**:
- Use `zerolog`.
- Every device lifecycle event logged: `{"level":"info","device_id":"d1","event":"spawned","device_type":"temperature_sensor","ts":"..."}`.
- Log levels: DEBUG (every telemetry point — disabled by default), INFO (lifecycle), WARN (publish retry), ERROR (unrecoverable).
- Configurable via `--log-level` flag and `LOG_LEVEL` env var.

#### Task 5.3: Admin HTTP API (Go)

```
File: device-runtime/internal/server/admin.go
```

**Specification**:
- Small HTTP server on `:8080` (alongside gRPC on `:50051`).
- Endpoints:
  - `GET /healthz` → 200 OK
  - `GET /readyz` → 200 when ready to accept devices
  - `GET /metrics` → Prometheus metrics
  - `GET /api/v1/status` → JSON runtime status (mirrors GetRuntimeStatus RPC)
  - `GET /api/v1/devices?type=X&state=running` → Device list with filtering
  - `POST /api/v1/devices/{id}/pause` → Pause a specific device
  - `POST /api/v1/devices/{id}/resume` → Resume a specific device

---

### PHASE 6: Containerization & Local Deployment

**Goal**: Full stack runs via `docker compose up` with a single command.

#### Task 6.1: Dockerfiles

**`Dockerfile.runtime`** (multi-stage):
```dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY device-runtime/ .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /runtime ./cmd/runtime

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=builder /runtime /usr/local/bin/runtime
EXPOSE 50051 8080
ENTRYPOINT ["runtime"]
```

**`Dockerfile.orchestrator`**:
```dockerfile
FROM python:3.12-slim
WORKDIR /app
COPY orchestrator/ .
RUN pip install --no-cache-dir .
COPY profiles/ /app/profiles/
COPY scenarios/ /app/scenarios/
ENTRYPOINT ["iot-sim"]
```

#### Task 6.2: Docker Compose Stack

```yaml
# deployments/docker-compose.yaml
services:
  runtime:
    build:
      context: .
      dockerfile: deployments/Dockerfile.runtime
    ports:
      - "50051:50051"
      - "8080:8080"   # admin API + /metrics (Prometheus scrape target)
    environment:
      - LOG_LEVEL=info
      - GOGC=100
      - GOMEMLIMIT=512MiB

  mosquitto:
    image: eclipse-mosquitto:2
    ports:
      - "1883:1883"
    volumes:
      - ./deployments/mosquitto.conf:/mosquitto/config/mosquitto.conf

  prometheus:
    image: prom/prometheus:latest
    ports:
      - "9090:9090"   # Prometheus UI accessible at localhost:9090
    volumes:
      - ./deployments/prometheus.yml:/etc/prometheus/prometheus.yml
    # prometheus.yml should scrape: http://runtime:8080/metrics

  grafana:
    image: grafana/grafana:latest
    ports:
      - "3000:3000"
    volumes:
      - ./deployments/grafana/dashboards:/var/lib/grafana/dashboards
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=admin
```

#### Task 6.3: Grafana Dashboard

```
File: deployments/grafana/dashboards/simulator.json
```

**Panels**:
- Active devices (gauge, by type)
- Messages/second (graph, by protocol)
- Publish latency p50/p95/p99 (graph)
- Error rate (graph)
- Memory usage (graph)
- Goroutine count (graph)
- Active faults (stat panel)

**Acceptance criteria**:
- `docker compose up -d` starts all services.
- `iot-sim spawn --profile profiles/temperature_sensor.yaml --count 1000` from orchestrator container.
- Grafana dashboard at `localhost:3000` shows live metrics (datasource: `http://prometheus:9090`).
- Prometheus at `localhost:9090` scrapes runtime metrics from `http://runtime:8080/metrics`.
- Mosquitto receives telemetry: `docker exec mosquitto mosquitto_sub -t "devices/#" -C 5`.

---

### PHASE 7: Scale-Out & Hardening

**Goal**: Support 100K+ devices across multiple runtime instances.

#### Task 7.1: Multi-Instance Device Sharding

**Specification**:
- Orchestrator maintains a list of runtime endpoints.
- Device assignment: `runtime_index = consistentHash(device_id) % len(runtimes)`.
- `RuntimeClient` becomes `RuntimePool` — routes spawn/stop/fault to correct instance.
- `StreamTelemetry` merges streams from all instances.

```python
# orchestrator/orchestrator/fleet.py
class RuntimePool:
    def __init__(self, endpoints: list[str]):
        self.clients = [RuntimeClient(ep) for ep in endpoints]
        self.ring = ConsistentHashRing(endpoints)  # use uhashring: pip install uhashring

    async def spawn(self, specs: list[DeviceSpec]) -> SpawnResult:
        # Group specs by target runtime
        groups = defaultdict(list)
        for spec in specs:
            target = self.ring.get(spec.device_id)
            groups[target].append(spec)
        # Fan-out spawn calls
        results = await asyncio.gather(*[
            self.clients[i].spawn(group) for i, group in groups.items()
        ])
        return merge_results(results)
```

#### Task 7.2: Connection Pooling for MQTT at Scale

**Specification**:
- One TCP connection can handle ~1000 devices (client-ID multiplexing or shared connection with unique topics).
- Pool size configurable: `--mqtt-pool-size 100` → supports ~100K devices.
- Connection health monitoring: replace dead connections automatically.

#### Task 7.3: Backpressure Handling

**Specification**:
- Device → publisher channel has bounded buffer (configurable, default 1000).
- When buffer is full, device either drops oldest (lossy mode) or slows ticker (backpressure mode).
- Mode configurable per device profile: `backpressure_strategy: drop_oldest | slow_down`.
- Metrics: `sim_backpressure_events_total` counter, `sim_publish_queue_depth` gauge.

#### Task 7.4: Graceful Shutdown

**Specification**:
- SIGINT/SIGTERM → stop accepting new devices → drain all device goroutines (with timeout) → flush telemetry → close publishers → close gRPC server.
- Shutdown timeout configurable: `--shutdown-timeout 30s`.
- Log progress: "Stopping 50000 devices...", "45000 remaining...", "Shutdown complete."

#### Task 7.5: Deterministic Replay

**Specification**:
- Every simulation run gets a `run_id` (UUID) and a `master_seed` (int64).
- Per-device seed: `masterSeed XOR hash(deviceID)`.
- Per-field seed: `deviceSeed XOR hash(fieldName)`.
- Scenario events are logged with sim-clock timestamps.
- Replay: provide same `master_seed` + same scenario → identical telemetry sequence.

**Test**: Run scenario, capture first 100 telemetry points. Replay with same seed, verify byte-identical output.

---

## 4. Cross-Cutting Concerns

### Error Handling Convention

| Layer | Strategy |
|-------|----------|
| Generator | Never errors. Returns default value on edge cases. |
| Publisher | Returns error. Device logs + increments counter + continues. |
| gRPC server | Maps to gRPC status codes (ALREADY_EXISTS, RESOURCE_EXHAUSTED, INTERNAL). |
| gRPC client (Python) | Retries UNAVAILABLE with exponential backoff. Surfaces others to scenario/CLI. |
| Scenario engine | Catches + logs. Continues unless `--fail-fast` flag set. |

### Configuration Hierarchy

```
Defaults (compiled) → Config file (YAML) → Environment variables → CLI flags
```

Go runtime config:
```yaml
# runtime-config.yaml
grpc:
  port: 50051
admin:
  port: 8080    # serves /healthz, /readyz, /metrics, /api/v1/...
mqtt:
  default_broker: tcp://localhost:1883
  pool_size: 10
  qos: 1
logging:
  level: info
  format: json
runtime:
  max_devices: 100000
  shutdown_timeout: 30s
  telemetry_buffer_size: 1000
  backpressure_strategy: slow_down
gc:
  gogc: 100
  gomemlimit: 512MiB
```

### Testing Strategy

| Level | Tool | What |
|-------|------|------|
| Unit | `go test`, `pytest` | Generators, publishers (mock broker), config parsing |
| Integration | `testcontainers` | MQTT with Mosquitto container, gRPC client↔server |
| E2E | Docker Compose + script | Full stack: spawn 1000 devices, verify telemetry arrives, run scenario, check metrics |
| Load | Custom Go benchmark | Spawn 100K devices, measure msg/s, latency, memory |
| Chaos | Scenario | Kill runtime mid-simulation, verify orchestrator detects + recovers |

---

## 5. Implementation Order Summary

```
Phase 0  ──▶  Phase 1  ──▶  Phase 2  ──▶  Phase 3  ──▶  Phase 4  ──▶  Phase 5  ──▶  Phase 6  ──▶  Phase 7
Scaffold     Walking       Profiles      Protocols     Scenarios     Observability  Containers   Scale-out
             skeleton      + generators  MQTT/HTTP     Engine        Prometheus     Docker        Sharding
             1 device      multi-field   AMQP          Fault inject  Grafana        Compose       Backpressure
             console out   YAML config   real brokers  Sim clock     Admin API      Full stack    Replay

             [testable]    [testable]    [testable]    [testable]    [testable]     [testable]    [testable]
```

Each phase ends with a working, testable system. An AI agent should execute tasks within a phase sequentially (task dependencies are linear within a phase), but should run all unit tests after each task to catch regressions early.

---

## 6. Agent Execution Guidelines

1. **Read the full phase before starting any task.** Understand how tasks connect.
2. **Run `make proto-gen` after any proto change.** Never hand-edit generated code.
3. **Run tests after every task.** `make go-test py-test`. Fix before proceeding.
4. **Commit after each task.** One logical commit per task (e.g., "feat: add Gaussian generator with tests").
5. **Don't optimize prematurely.** Phase 1-4 prioritize correctness. Phase 7 handles performance.
6. **Use the console publisher extensively.** It's the fastest feedback loop before real brokers are wired.
7. **When in doubt about a proto field type, use `google.protobuf.Struct`.** It's flexible from Python and parseable in Go. Tighten later.
8. **Integration tests use testcontainers.** Don't require manual broker setup.
9. **Every public Go function gets a doc comment.** Every Python function gets a docstring.
10. **Log decisions.** If a task requires a design choice not specified here, log it in a `DECISIONS.md` file with rationale.
