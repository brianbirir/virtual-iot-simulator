"""FastAPI REST API for the IoT Simulator orchestrator.

Exposes the same fleet management operations as the CLI via HTTP,
allowing external tools, dashboards, and scripts to control the simulator
without shell access.

Base path: /api/v1
"""
from __future__ import annotations

import asyncio
import json
from typing import Optional

from fastapi import FastAPI, HTTPException, Query
from fastapi.responses import StreamingResponse
from pydantic import BaseModel, Field

from orchestrator.grpc_client import (
    RuntimeClient,
    make_device_id_selector,
    make_label_selector,
)

app = FastAPI(
    title="IoT Simulator API",
    description="REST interface for the virtual IoT device simulator orchestrator.",
    version="1.0.0",
)

_DEFAULT_RUNTIME = "localhost:50051"


# ---------------------------------------------------------------------------
# Request / response schemas
# ---------------------------------------------------------------------------


class SpawnRequest(BaseModel):
    profile: str = Field(..., description="Path to device profile YAML file")
    count: int = Field(1, ge=1, description="Number of devices to spawn")
    runtime: str = Field(_DEFAULT_RUNTIME, description="Runtime gRPC address (host:port)")


class SpawnResponse(BaseModel):
    spawned: int
    failed: list[str] = []


class StopRequest(BaseModel):
    all_devices: bool = Field(False, alias="all", description="Stop all running devices")
    device_type: Optional[str] = Field(None, description="Stop devices with this device_type label")
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
# Routes
# ---------------------------------------------------------------------------


@app.post("/api/v1/devices/spawn", response_model=SpawnResponse, tags=["devices"])
async def spawn_devices(req: SpawnRequest) -> SpawnResponse:
    """Spawn virtual devices from a YAML device profile."""
    from orchestrator.config import load_profile_specs

    try:
        specs = load_profile_specs(req.profile, req.count)
    except (FileNotFoundError, ValueError) as exc:
        raise HTTPException(status_code=422, detail=str(exc)) from exc

    async with RuntimeClient(req.runtime) as client:
        resp = await client.spawn(specs)

    return SpawnResponse(
        spawned=resp.spawned,
        failed=list(resp.failed_device_ids),
    )


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
    device_type: Optional[str] = Query(None, description="Filter by device_type label"),
    device_ids: Optional[str] = Query(None, description="Comma-separated device IDs"),
    batch_size: int = Query(50, ge=1, le=500),
    runtime: str = Query(_DEFAULT_RUNTIME, description="Runtime gRPC address"),
) -> StreamingResponse:
    """Stream live telemetry as Server-Sent Events (SSE).

    Connect with `EventSource` in the browser or `curl -N` from the terminal.
    Each event is a JSON object: `{"device_id": "...", "metric": "...", "value": ...}`.
    """
    selector = None
    if device_ids:
        ids = [s.strip() for s in device_ids.split(",")]
        selector = make_device_id_selector(ids)
    elif device_type:
        selector = make_label_selector(f"device_type={device_type}")

    async def event_generator():
        async with RuntimeClient(runtime) as client:
            try:
                async for batch in client.stream_telemetry(selector=selector, batch_size=batch_size):
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
