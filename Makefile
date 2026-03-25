.PHONY: all proto-gen proto-gen-go proto-gen-py proto-lint proto-breaking \
        go-build go-test go-lint go-fmt \
        docker-go-test docker-go-lint docker-go-fmt \
        py-test py-lint py-fmt \
        docker-py-test docker-py-lint docker-py-fmt \
        docker-build docker-up docker-down docker-logs \
        deps deps-go deps-py lint docker-lint

ROOT_DIR              := $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
DOCKER_GO_IMAGE       ?= golang:1.26-alpine
DOCKER_PY_IMAGE       ?= python:3.12-slim
GOLANGCI_LINT_VERSION ?= v1.64.8
# Source is mounted read-only; a named volume provides a writable module/build
# cache that persists across runs so repeated invocations stay fast.
DOCKER_GO_RUN := docker run --rm \
	-v "$(ROOT_DIR):/workspace:ro" \
	-v iot-sim-gocache:/root/.cache \
	-v iot-sim-gopath:/go \
	-w /workspace/device-runtime \
	$(DOCKER_GO_IMAGE)
# Source is mounted read-only; pip cache is persisted in a named volume.
DOCKER_PY_RUN := docker run --rm \
	-v "$(ROOT_DIR)/orchestrator:/workspace:ro" \
	-v iot-sim-pipcache:/root/.cache/pip \
	-w /workspace \
	$(DOCKER_PY_IMAGE)

# ── Dependencies ────────────────────────────────────────────────────────────

GOLANGCI_LINT        := $(shell go env GOPATH)/bin/golangci-lint

GOIMPORTS := $(shell go env GOPATH)/bin/goimports

# Fetch/tidy Go modules and install Go dev tools.
deps-go:
	cd device-runtime && go mod tidy
	@test -f $(GOLANGCI_LINT) || go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	@test -f $(GOIMPORTS) || go install golang.org/x/tools/cmd/goimports@latest

# Install Python dependencies via pipenv.
deps-py:
	cd orchestrator && pipenv install --dev

deps: deps-go deps-py

# ── Proto ────────────────────────────────────────────────────────────────────

proto-gen: proto-gen-go proto-gen-py

proto-gen-go:
	buf generate

proto-gen-py:
	cd orchestrator && pipenv run python -m grpc_tools.protoc \
		-I ../proto \
		-I $$(pipenv run python -c "import grpc_tools, os; print(os.path.join(os.path.dirname(grpc_tools.__file__), '_proto'))") \
		--python_out=gen/python \
		--grpc_python_out=gen/python \
		../proto/simulator/v1/*.proto

proto-lint:
	buf lint

proto-breaking:
	buf breaking --against '.git#branch=main'

# ── Go ───────────────────────────────────────────────────────────────────────

go-build:
	cd device-runtime && go build ./cmd/runtime

go-test:
	cd device-runtime && go test ./...

# Auto-fix Go import ordering and formatting.
go-fmt:
	@test -f $(GOIMPORTS) || go install golang.org/x/tools/cmd/goimports@latest
	cd device-runtime && $(GOIMPORTS) -w -local github.com/virtual-iot-simulator ./...
	cd device-runtime && gofmt -w ./...

# Auto-installs golangci-lint to $(GOPATH)/bin if missing.
go-lint:
	@test -f $(GOLANGCI_LINT) || go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	cd device-runtime && $(GOLANGCI_LINT) run ./...

# ── Python ───────────────────────────────────────────────────────────────────

py-test:
	cd orchestrator && pipenv run pytest tests/ -v

# Check formatting and lint (CI-safe: no writes).
py-lint:
	cd orchestrator && pipenv run ruff format --check .
	cd orchestrator && pipenv run ruff check .

# Auto-fix formatting and safe lint issues.
py-fmt:
	cd orchestrator && pipenv run ruff format .
	cd orchestrator && pipenv run ruff check --fix .

# ── Docker — image builds & compose ─────────────────────────────────────────

docker-build:
	docker build -f deployments/Dockerfile.runtime -t iot-simulator-runtime:latest .
	docker build -f deployments/Dockerfile.orchestrator -t iot-simulator-orchestrator:latest .
	docker build -f deployments/Dockerfile.frontend -t iot-simulator-frontend:latest .

docker-up:
	docker compose -f deployments/docker-compose.yaml up -d

docker-down:
	docker compose -f deployments/docker-compose.yaml down

docker-logs:
	docker compose -f deployments/docker-compose.yaml logs -f

# ── Docker — Go: test / lint / fmt ──────────────────────────────────────────
# These targets run inside a plain golang image — no local Go install needed.

docker-go-test:
	$(DOCKER_GO_RUN) sh -c "go test ./..."

docker-go-lint:
	$(DOCKER_GO_RUN) sh -c "\
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) && \
		golangci-lint run ./..."

# fmt writes back to the host via a rw mount (goimports + gofmt).
docker-go-fmt:
	docker run --rm \
		-v "$(ROOT_DIR)/device-runtime:/workspace" \
		-w /workspace \
		$(DOCKER_GO_IMAGE) sh -c "\
			go install golang.org/x/tools/cmd/goimports@latest && \
			goimports -w -local github.com/virtual-iot-simulator ./... && \
			gofmt -w ./..."

# ── Docker — Python: test / lint / fmt ──────────────────────────────────────
# These targets run inside a plain python image — no pipenv needed on the host.

DOCKER_PY_INSTALL := pip install --quiet --no-cache-dir \
	grpcio grpcio-tools protobuf typer pydantic pyyaml rich fastapi \
	"uvicorn[standard]" pytest pytest-asyncio httpx ruff

docker-py-test:
	docker run --rm \
		-v "$(ROOT_DIR)/orchestrator:/workspace:ro" \
		-v "$(ROOT_DIR)/profiles:/profiles:ro" \
		-v iot-sim-pipcache:/root/.cache/pip \
		-w /workspace \
		$(DOCKER_PY_IMAGE) sh -c "$(DOCKER_PY_INSTALL) && pytest tests/ -v -p no:cacheprovider"

docker-py-lint:
	$(DOCKER_PY_RUN) sh -c "\
		pip install --quiet --no-cache-dir ruff && \
		ruff format --check --no-cache . && \
		ruff check --no-cache ."

# fmt writes back to the host via a rw mount.
docker-py-fmt:
	docker run --rm \
		-v "$(ROOT_DIR)/orchestrator:/workspace" \
		-w /workspace \
		$(DOCKER_PY_IMAGE) sh -c "\
			pip install --quiet --no-cache-dir ruff && \
			ruff format . && \
			ruff check --fix ."

# Run all Docker-based checks (no local toolchain required).
docker-lint: docker-go-lint docker-py-lint

# ── All ──────────────────────────────────────────────────────────────────────

# Run all checks (lint + build + test). Used in CI.
lint: go-lint py-lint

all: proto-lint proto-gen go-build go-test go-lint py-test py-lint
