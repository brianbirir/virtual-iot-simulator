.PHONY: all proto-gen proto-lint proto-breaking go-build go-test py-test docker-build

ROOT_DIR := $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))

# Proto
proto-gen: proto-gen-go proto-gen-py

proto-gen-go:
	buf generate

proto-gen-py:
	cd orchestrator && uv run python -m grpc_tools.protoc \
		-I ../proto \
		-I $(shell python3 -c "import grpc_tools; import os; print(os.path.join(os.path.dirname(grpc_tools.__file__), '_proto'))") \
		--python_out=gen/python \
		--grpc_python_out=gen/python \
		../proto/simulator/v1/*.proto

proto-lint:
	buf lint

proto-breaking:
	buf breaking --against '.git#branch=main'

# Go
go-build:
	cd device-runtime && go build ./cmd/runtime

go-test:
	cd device-runtime && go test ./...

# Python
py-test:
	cd orchestrator && uv run pytest tests/ -v

# Docker
docker-build:
	docker build -f deployments/Dockerfile.runtime -t iot-runtime:dev .
	docker build -f deployments/Dockerfile.orchestrator -t iot-orchestrator:dev .

# All
all: proto-lint proto-gen go-build go-test py-test
