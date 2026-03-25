"""Profile CRUD routes."""

from __future__ import annotations

import uuid
from typing import Annotated

from fastapi import APIRouter, Depends, HTTPException
from sqlalchemy import delete, select
from sqlalchemy.ext.asyncio import AsyncSession

from orchestrator.api.schemas import (
    ProfileCreate,
    ProfileResponse,
    ProfileUpdate,
)
from orchestrator.database import get_session
from orchestrator.models import DeviceProfile

router = APIRouter(prefix="/api/v1/profiles", tags=["profiles"])

DbSession = Annotated[AsyncSession, Depends(get_session)]


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


@router.get("", response_model=list[ProfileResponse])
async def list_profiles(session: DbSession) -> list[ProfileResponse]:
    """Return all device profiles."""
    result = await session.execute(select(DeviceProfile).order_by(DeviceProfile.name))
    return [_profile_to_response(p) for p in result.scalars()]


@router.post("", response_model=ProfileResponse, status_code=201)
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


@router.get("/{profile_id}", response_model=ProfileResponse)
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


@router.put("/{profile_id}", response_model=ProfileResponse)
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


@router.delete("/{profile_id}", status_code=204)
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
