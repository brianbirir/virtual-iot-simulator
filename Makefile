.PHONY: all proto-gen proto-lint proto-breaking go-build go-test py-test docker-build deps

ROOT_DIR := $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))

# ── Dependencies ────────────────────────────────────────────────────────────

# Fetch/tidy Go modules (run after adding new deps to go.mod).
deps-go:
	cd device-runtime && go mod tidy

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

# ── Python ───────────────────────────────────────────────────────────────────

py-test:
	cd orchestrator && pipenv run pytest tests/ -v

# ── Docker ───────────────────────────────────────────────────────────────────

docker-build:
	docker build -f deployments/Dockerfile.runtime -t iot-runtime:dev .
	docker build -f deployments/Dockerfile.orchestrator -t iot-orchestrator:dev .

# ── All ──────────────────────────────────────────────────────────────────────

all: proto-lint proto-gen go-build go-test py-test
