"""FastAPI REST API for the IoT Simulator orchestrator.

Exposes fleet management operations and device profile CRUD via HTTP.

Base path: /api/v1
"""

from __future__ import annotations

import asyncio
import json
import os
import uuid
from datetime import datetime
from typing import Annotated

from fastapi import Depends, FastAPI, HTTPException, Query
from fastapi.responses import StreamingResponse
from pydantic import BaseModel, Field
from sqlalchemy import delete, select
from sqlalchemy.ext.asyncio import AsyncSession

from orchestrator.database import get_session, init_db
from orchestrator.grpc_client import (
    RuntimeClient,
    make_device_id_selector,
    make_label_selector,
)
from orchestrator.models import DeviceProfile

app = FastAPI(
    title="IoT Simulator API",
    description="REST interface for the virtual IoT device simulator orchestrator.",
    version="1.0.0",
)

_DEFAULT_RUNTIME = os.environ.get("IOT_SIM_RUNTIME", "localhost:50051")

DbSession = Annotated[AsyncSession, Depends(get_session)]


# ---------------------------------------------------------------------------
# Lifecycle
# ---------------------------------------------------------------------------


@app.on_event("startup")
async def _startup() -> None:
    await init_db()


# ---------------------------------------------------------------------------
# Request / response schemas
# ---------------------------------------------------------------------------


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


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _profile_to_response(p: DeviceProfile) -> ProfileResponse:
    return ProfileResponse(
        id=str(p.id),
        name=p.name,
        type=p.type,
        protocol=p.protocol,
        topic_template=p.topic_template,
        telemetry_interval=p.telemetry_interval,
        telemetry_fields=p.telemetry_fields,
        labels=p.labels,
        created_at=p.created_at,
        updated_at=p.updated_at,
    )


# ---------------------------------------------------------------------------
# Profile CRUD routes
# ---------------------------------------------------------------------------


@app.get("/api/v1/profiles", response_model=list[ProfileResponse], tags=["profiles"])
async def list_profiles(session: DbSession) -> list[ProfileResponse]:
    """Return all device profiles."""
    result = await session.execute(select(DeviceProfile).order_by(DeviceProfile.name))
    return [_profile_to_response(p) for p in result.scalars()]


@app.post("/api/v1/profiles", response_model=ProfileResponse, status_code=201, tags=["profiles"])
async def create_profile(body: ProfileCreate, session: DbSession) -> ProfileResponse:
    """Create a new device profile."""
    profile = DeviceProfile(
        name=body.name,
        type=body.type,
        protocol=body.protocol,
        topic_template=body.topic_template,
        telemetry_interval=body.telemetry_interval,
        telemetry_fields={
            k: v.model_dump(exclude_none=True) for k, v in body.telemetry_fields.items()
        },
        labels=body.labels,
    )
    session.add(profile)
    try:
        await session.commit()
    except Exception as exc:
        await session.rollback()
        if "unique" in str(exc).lower():
            raise HTTPException(
                status_code=409, detail=f"Profile name '{body.name}' already exists"
            ) from exc
        raise HTTPException(status_code=500, detail=str(exc)) from exc
    await session.refresh(profile)
    return _profile_to_response(profile)


@app.get("/api/v1/profiles/{profile_id}", response_model=ProfileResponse, tags=["profiles"])
async def get_profile(profile_id: str, session: DbSession) -> ProfileResponse:
    """Get a device profile by ID."""
    try:
        uid = uuid.UUID(profile_id)
    except ValueError:
        raise HTTPException(status_code=422, detail="Invalid profile ID format") from None
    profile = await session.get(DeviceProfile, uid)
    if profile is None:
        raise HTTPException(status_code=404, detail="Profile not found")
    return _profile_to_response(profile)


@app.put("/api/v1/profiles/{profile_id}", response_model=ProfileResponse, tags=["profiles"])
async def update_profile(
    profile_id: str, body: ProfileUpdate, session: DbSession
) -> ProfileResponse:
    """Update an existing device profile."""
    try:
        uid = uuid.UUID(profile_id)
    except ValueError:
        raise HTTPException(status_code=422, detail="Invalid profile ID format") from None
    profile = await session.get(DeviceProfile, uid)
    if profile is None:
        raise HTTPException(status_code=404, detail="Profile not found")

    for field, value in body.model_dump(exclude_none=True).items():
        if field == "telemetry_fields":
            value = {
                k: v.model_dump(exclude_none=True) if hasattr(v, "model_dump") else v
                for k, v in value.items()
            }
        setattr(profile, field, value)

    try:
        await session.commit()
    except Exception as exc:
        await session.rollback()
        if "unique" in str(exc).lower():
            raise HTTPException(
                status_code=409, detail=f"Profile name '{body.name}' already exists"
            ) from exc
        raise HTTPException(status_code=500, detail=str(exc)) from exc
    await session.refresh(profile)
    return _profile_to_response(profile)


@app.delete("/api/v1/profiles/{profile_id}", status_code=204, tags=["profiles"])
async def delete_profile(profile_id: str, session: DbSession) -> None:
    """Delete a device profile."""
    try:
        uid = uuid.UUID(profile_id)
    except ValueError:
        raise HTTPException(status_code=422, detail="Invalid profile ID format") from None
    result = await session.execute(delete(DeviceProfile).where(DeviceProfile.id == uid))
    if result.rowcount == 0:
        raise HTTPException(status_code=404, detail="Profile not found")
    await session.commit()


# ---------------------------------------------------------------------------
# Device routes
# ---------------------------------------------------------------------------


@app.post("/api/v1/devices/spawn", response_model=SpawnResponse, tags=["devices"])
async def spawn_devices(req: SpawnRequest, session: DbSession) -> SpawnResponse:
    """Spawn virtual devices from a database-stored device profile."""
    from orchestrator.config import profile_to_specs_from_dict

    try:
        uid = uuid.UUID(req.profile_id)
    except ValueError:
        raise HTTPException(status_code=422, detail="Invalid profile_id format") from None

    profile = await session.get(DeviceProfile, uid)
    if profile is None:
        raise HTTPException(status_code=404, detail=f"Profile '{req.profile_id}' not found")

    try:
        specs = profile_to_specs_from_dict(
            {
                "type": profile.type,
                "protocol": profile.protocol,
                "topic_template": profile.topic_template,
                "telemetry_interval": profile.telemetry_interval,
                "telemetry_fields": profile.telemetry_fields,
                "labels": profile.labels,
            },
            req.count,
        )
    except ValueError as exc:
        raise HTTPException(status_code=422, detail=str(exc)) from exc

    async with RuntimeClient(req.runtime) as client:
        resp = await client.spawn(specs)

    return SpawnResponse(spawned=resp.spawned, failed=list(resp.failed_device_ids))


@app.post("/api/v1/devices/stop", response_model=StopResponse, tags=["devices"])
async def stop_devices(req: StopRequest) -> StopResponse:
    """Stop running devices by type label or all at once."""
    if not req.all_devices and not req.device_type:
        raise HTTPException(
            status_code=422,
            detail="Provide 'all': true or a 'device_type' to stop.",
        )

    selector = None
    if req.device_type:
        selector = make_label_selector(f"device_type={req.device_type}")

    async with RuntimeClient(req.runtime) as client:
        resp = await client.stop(selector=selector)

    return StopResponse(stopped=resp.stopped)


@app.get("/api/v1/devices/status", response_model=StatusResponse, tags=["devices"])
async def get_status(
    runtime: str = Query(_DEFAULT_RUNTIME, description="Runtime gRPC address"),
) -> StatusResponse:
    """Return combined fleet and runtime status."""
    async with RuntimeClient(runtime) as client:
        fleet = await client.status()
        rt = await client.runtime_status()

    return StatusResponse(
        fleet=FleetStatusResponse(
            total_devices=fleet.total_devices,
            by_state=dict(fleet.by_state),
            by_type=dict(fleet.by_type),
        ),
        runtime=RuntimeStatusResponse(
            active_devices=rt.active_devices,
            goroutine_count=rt.goroutine_count,
            memory_mb=rt.memory_bytes / 1024 / 1024,
            uptime_seconds=rt.uptime.ToTimedelta().total_seconds(),
        ),
    )


@app.get("/api/v1/devices/stream", tags=["devices"])
async def stream_telemetry(
    device_type: str | None = Query(None, description="Filter by device_type label"),
    device_ids: str | None = Query(None, description="Comma-separated device IDs"),
    batch_size: int = Query(50, ge=1, le=500),
    runtime: str = Query(_DEFAULT_RUNTIME, description="Runtime gRPC address"),
) -> StreamingResponse:
    """Stream live telemetry as Server-Sent Events (SSE)."""
    selector = None
    if device_ids:
        ids = [s.strip() for s in device_ids.split(",")]
        selector = make_device_id_selector(ids)
    elif device_type:
        selector = make_label_selector(f"device_type={device_type}")

    async def event_generator():
        async with RuntimeClient(runtime) as client:
            try:
                async for batch in client.stream_telemetry(
                    selector=selector, batch_size=batch_size
                ):
                    for pt in batch.points:
                        kind = pt.WhichOneof("value")
                        value = getattr(pt, kind) if kind else None
                        event = {
                            "device_id": pt.device_id,
                            "metric": pt.metric_name,
                            "value": value,
                            "timestamp": pt.timestamp.ToDatetime().isoformat(),
                        }
                        yield f"data: {json.dumps(event)}\n\n"
            except asyncio.CancelledError:
                return

    return StreamingResponse(
        event_generator(),
        media_type="text/event-stream",
        headers={
            "Cache-Control": "no-cache",
            "X-Accel-Buffering": "no",
        },
    )


@app.get("/api/v1/health", tags=["meta"])
async def health() -> dict:
    """Health check — returns 200 if the API process is running."""
    return {"status": "ok"}
