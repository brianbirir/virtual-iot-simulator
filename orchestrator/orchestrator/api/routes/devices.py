"""Device management and telemetry streaming routes."""

from __future__ import annotations

import asyncio
import json
import uuid
from typing import Annotated

from fastapi import APIRouter, Depends, HTTPException, Query
from fastapi.responses import StreamingResponse
from sqlalchemy.ext.asyncio import AsyncSession

from orchestrator.api.schemas import (
    _DEFAULT_RUNTIME,
    FleetStatusResponse,
    RuntimeStatusResponse,
    SpawnRequest,
    SpawnResponse,
    StatusResponse,
    StopRequest,
    StopResponse,
)
from orchestrator.database import get_session
from orchestrator.grpc_client import (
    RuntimeClient,
    make_device_id_selector,
    make_label_selector,
)
from orchestrator.models import DeviceProfile

router = APIRouter(prefix="/api/v1/devices", tags=["devices"])

DbSession = Annotated[AsyncSession, Depends(get_session)]


@router.post("/spawn", response_model=SpawnResponse)
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


@router.post("/stop", response_model=StopResponse)
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


@router.get("/status", response_model=StatusResponse)
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


@router.get("/stream")
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
