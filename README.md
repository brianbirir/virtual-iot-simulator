# Virtual IoT Simulator

A large-scale IoT device simulator capable of running thousands of concurrent virtual devices that generate realistic telemetry and publish it over configurable protocols.

The system is split into two services that communicate via gRPC:

- **Device Runtime** (Go) — the data plane. Runs one goroutine per device, generates telemetry, and streams it over the configured protocol adapter.
- **Simulation Orchestrator** (Python) — the control plane. Loads device profiles, manages the fleet lifecycle, and exposes both a CLI and a REST API.

> **Status:** Core runtime, gRPC contract, console publisher, Gaussian/Static generators, CLI, and REST API are all functional. See [docs/SYSTEM.md](docs/SYSTEM.md) for the full design and implementation roadmap.

---

## Repository Layout

```text
.
├── device-runtime/          # Go gRPC server + virtual device engine
│   ├── cmd/runtime/         # Binary entry point
│   └── internal/
│       ├── device/          # VirtualDevice, Manager, RuntimeClock
│       ├── generator/       # Gaussian, Static generators + factory
│       ├── protocol/        # Publisher interface + Console adapter
│       └── server/          # gRPC handlers, Broadcaster, interceptors
├── orchestrator/            # Python orchestrator
│   ├── orchestrator/
│   │   ├── api.py           # FastAPI REST app
│   │   ├── cli.py           # Typer CLI (iot-sim)
│   │   ├── config.py        # Profile loader + Pydantic validation
│   │   └── grpc_client.py   # Typed async gRPC client
│   ├── tests/
│   ├── Pipfile              # Pipenv dependency manifest
│   └── pyproject.toml       # Package metadata + entry points
├── proto/simulator/v1/      # Protobuf definitions (source of truth)
├── profiles/                # Device profile YAML files
├── deployments/             # Docker Compose + Kubernetes manifests
├── docs/SYSTEM.md           # Full technical design document
├── IMPLEMENTATION_PLAN.md   # Phase-by-phase implementation plan
├── buf.yaml                 # Buf lint/breaking-change config
└── Makefile                 # Build, test, and code-gen targets
```

---

## Prerequisites

| Tool | Version | Purpose |
| ---- | ------- | ------- |
| Go | ≥ 1.21 | Device runtime |
| Python | ≥ 3.12 | Orchestrator |
| pipenv | latest | Python dependency management |
| buf | latest | Protobuf linting and code generation |

---

## Quick Start

### 1. Generate protobuf code

```bash
make proto-gen
```

This runs `buf generate` for Go and `grpc_tools.protoc` for Python, writing generated files to `device-runtime/gen/go/` and `orchestrator/gen/python/`.

### 2. Start the Device Runtime

```bash
make go-build
./device-runtime/runtime --port 50051 --admin-port 8080 --log-level info
```

The runtime exposes:

- `:50051` — gRPC (`DeviceRuntimeService`)
- `:8080` — Admin HTTP (`/healthz`, `/readyz`)

### 3. Install Python dependencies

```bash
cd orchestrator
pipenv install
```

### 4. Control devices via CLI

```bash
# Spawn 5 temperature sensors
pipenv run iot-sim spawn --profile ../profiles/temperature_sensor.yaml --count 5

# Check fleet status
pipenv run iot-sim status

# Stream live telemetry to the terminal
pipenv run iot-sim stream --type temperature_sensor

# Stop all devices
pipenv run iot-sim stop --all
```

### 5. Control devices via the REST API

```bash
# Start the API server (default: http://localhost:8000)
pipenv run iot-sim serve

# Spawn devices
curl -X POST http://localhost:8000/api/v1/devices/spawn \
  -H "Content-Type: application/json" \
  -d '{"profile": "../profiles/temperature_sensor.yaml", "count": 5}'

# Fleet status
curl http://localhost:8000/api/v1/devices/status

# Stream telemetry (SSE)
curl -N "http://localhost:8000/api/v1/devices/stream?device_type=temperature_sensor"

# Stop all
curl -X POST http://localhost:8000/api/v1/devices/stop \
  -H "Content-Type: application/json" \
  -d '{"all": true}'
```

Interactive API docs (Swagger UI) are available at `http://localhost:8000/docs`.

---

## Device Profiles

Profiles are YAML files in `profiles/` that define a device type's telemetry schema, protocol, and generator configuration.

```yaml
# profiles/temperature_sensor.yaml
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

**Supported generator types:** `gaussian`, `static`

**Supported protocols:** `console` (stdout), MQTT, AMQP, and HTTP

---

## Development

### Run tests

```bash
# Go
make go-test

# Python
cd orchestrator && pipenv run pytest tests/ -v
```

### Makefile targets

| Target | Description |
| ------ | ----------- |
| `make proto-gen` | Generate Go + Python code from `.proto` files |
| `make proto-lint` | Lint proto files with Buf |
| `make proto-breaking` | Check for breaking changes against `main` |
| `make go-build` | Build the runtime binary |
| `make go-test` | Run Go tests |
| `make py-test` | Run Python tests |
| `make all` | Lint → generate → build → test (all) |

---

## Documentation

- [docs/SYSTEM.md](docs/SYSTEM.md) — full technical design: architecture, data models, concurrency model, API reference, and implementation status per phase.
- [IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md) — task-level execution plan for the AI-assisted build.
