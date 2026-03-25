"""Typed gRPC client wrapper for the DeviceRuntimeService."""

from __future__ import annotations

import uuid
from collections.abc import AsyncIterator

import grpc
import grpc.aio
from gen.python.simulator.v1 import device_pb2, orchestrator_pb2_grpc
from simulator.v1.orchestrator_pb2 import (
    DeviceIdList,
    DeviceSelector,
    FleetStatus,
    GetFleetStatusRequest,
    RuntimeStatus,
    SpawnDevicesRequest,
    SpawnDevicesResponse,
    StopDevicesRequest,
    StopDevicesResponse,
    StreamTelemetryRequest,
)
from simulator.v1.telemetry_pb2 import TelemetryBatch


class RuntimeClient:
    """Typed wrapper around the generated DeviceRuntimeService gRPC stub."""

    def __init__(self, address: str = "localhost:50051"):
        """Connect to a runtime at address (host:port)."""
        self._address = address
        self._channel: grpc.aio.Channel | None = None
        self._stub: orchestrator_pb2_grpc.DeviceRuntimeServiceStub | None = None

    async def connect(self) -> None:
        """Open the gRPC channel."""
        self._channel = grpc.aio.insecure_channel(
            self._address,
            options=[
                ("grpc.max_send_message_length", 200 * 1024 * 1024),
                ("grpc.max_receive_message_length", 200 * 1024 * 1024),
            ],
        )
        self._stub = orchestrator_pb2_grpc.DeviceRuntimeServiceStub(self._channel)

    async def close(self) -> None:
        """Close the gRPC channel."""
        if self._channel:
            await self._channel.close()

    async def __aenter__(self) -> RuntimeClient:
        await self.connect()
        return self

    async def __aexit__(self, *_: object) -> None:
        await self.close()

    @property
    def stub(self) -> orchestrator_pb2_grpc.DeviceRuntimeServiceStub:
        if self._stub is None:
            raise RuntimeError("RuntimeClient not connected — call connect() or use async with")
        return self._stub

    async def spawn(
        self,
        specs: list[device_pb2.DeviceSpec],
        scenario_id: str = "",
    ) -> SpawnDevicesResponse:
        """Spawn devices from a list of DeviceSpecs."""
        return await self.stub.SpawnDevices(
            SpawnDevicesRequest(specs=specs, scenario_id=scenario_id)
        )

    async def stop(
        self,
        selector: DeviceSelector | None = None,
        graceful: bool = True,
    ) -> StopDevicesResponse:
        """Stop devices matching selector (or all devices if selector is None)."""
        req = StopDevicesRequest(graceful=graceful)
        if selector:
            req.selector.CopyFrom(selector)
        return await self.stub.StopDevices(req)

    async def status(
        self,
        selector: DeviceSelector | None = None,
    ) -> FleetStatus:
        """Return current fleet status."""
        req = GetFleetStatusRequest()
        if selector:
            req.selector.CopyFrom(selector)
        return await self.stub.GetFleetStatus(req)

    async def runtime_status(self) -> RuntimeStatus:
        """Return runtime health metrics."""
        from google.protobuf import empty_pb2

        return await self.stub.GetRuntimeStatus(empty_pb2.Empty())

    async def stream_telemetry(
        self,
        selector: DeviceSelector | None = None,
        batch_size: int = 100,
    ) -> AsyncIterator[TelemetryBatch]:
        """Yield TelemetryBatch objects from the runtime stream."""
        req = StreamTelemetryRequest(batch_size=batch_size)
        if selector:
            req.selector.CopyFrom(selector)
        async for batch in self.stub.StreamTelemetry(req):
            yield batch


def make_device_id_selector(device_ids: list[str]) -> DeviceSelector:
    """Build a DeviceSelector that matches a specific list of device IDs."""
    return DeviceSelector(device_ids=DeviceIdList(ids=device_ids))


def make_label_selector(expression: str) -> DeviceSelector:
    """Build a DeviceSelector from a 'key=value' label expression."""
    return DeviceSelector(label_selector=expression)


def generate_device_ids(device_type: str, count: int, offset: int = 0) -> list[str]:
    """
    Generate sequential device IDs.

    Uses the convention: {device_type}-{zero_padded_index}
    e.g. temperature_sensor-0001, temperature_sensor-0002, ...
    """
    return [f"{device_type}-{(offset + i):04d}" for i in range(count)]


def generate_uuid_device_ids(device_type: str, count: int) -> list[str]:
    """
    Generate unique device IDs with UUID suffix for scenario use.

    Uses the convention: {device_type}-{uuid4_short}
    """
    return [f"{device_type}-{uuid.uuid4().hex[:8]}" for _ in range(count)]
