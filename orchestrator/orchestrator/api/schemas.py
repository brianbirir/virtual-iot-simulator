"""Request and response schemas for the IoT Simulator API."""

from __future__ import annotations

import os
from datetime import datetime

from pydantic import BaseModel, Field

_DEFAULT_RUNTIME = os.environ.get("IOT_SIM_RUNTIME", "localhost:50051")


class TelemetryFieldSchema(BaseModel):
    type: str
    # gaussian
    mean: float | None = None
    stddev: float | None = None
    # brownian
    start: float | None = None
    drift: float | None = None
    volatility: float | None = None
    mean_reversion: float | None = None
    min: float | None = None
    max: float | None = None
    # diurnal
    baseline: float | None = None
    amplitude: float | None = None
    peak_hour: int | None = None
    noise_stddev: float | None = None
    # markov
    states: list[str] | None = None
    transition_matrix: list[list[float]] | None = None
    initial_state: str | None = None
    # static
    value: float | None = None

    model_config = {"extra": "allow"}


class ProfileBase(BaseModel):
    name: str = Field(..., description="Unique human-readable profile name")
    type: str = Field(..., description="Device type identifier (e.g. temperature_sensor)")
    protocol: str = Field("console", description="Output protocol: mqtt | amqp | http | console")
    topic_template: str = Field("devices/{device_id}/telemetry", description="Topic/URL template")
    telemetry_interval: str = Field("5s", description="Interval between readings (e.g. 5s, 500ms)")
    telemetry_fields: dict[str, TelemetryFieldSchema] = Field(
        default_factory=dict, description="Map of field name → generator config"
    )
    labels: dict[str, str] = Field(default_factory=dict, description="Arbitrary metadata labels")


class ProfileCreate(ProfileBase):
    pass


class ProfileUpdate(BaseModel):
    name: str | None = None
    type: str | None = None
    protocol: str | None = None
    topic_template: str | None = None
    telemetry_interval: str | None = None
    telemetry_fields: dict[str, TelemetryFieldSchema] | None = None
    labels: dict[str, str] | None = None


class ProfileResponse(ProfileBase):
    id: str
    created_at: datetime
    updated_at: datetime

    model_config = {"from_attributes": True}


class SpawnRequest(BaseModel):
    profile_id: str = Field(..., description="ID of a device profile stored in the database")
    count: int = Field(1, ge=1, description="Number of devices to spawn")
    runtime: str = Field(_DEFAULT_RUNTIME, description="Runtime gRPC address (host:port)")


class SpawnResponse(BaseModel):
    spawned: int
    failed: list[str] = []


class StopRequest(BaseModel):
    all_devices: bool = Field(False, alias="all", description="Stop all running devices")
    device_type: str | None = Field(None, description="Stop devices with this device_type label")
    runtime: str = Field(_DEFAULT_RUNTIME, description="Runtime gRPC address (host:port)")

    model_config = {"populate_by_name": True}


class StopResponse(BaseModel):
    stopped: int


class FleetStatusResponse(BaseModel):
    total_devices: int
    by_state: dict[str, int]
    by_type: dict[str, int]


class RuntimeStatusResponse(BaseModel):
    active_devices: int
    goroutine_count: int
    memory_mb: float
    uptime_seconds: float


class StatusResponse(BaseModel):
    fleet: FleetStatusResponse
    runtime: RuntimeStatusResponse
