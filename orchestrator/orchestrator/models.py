"""SQLAlchemy ORM models for persisted simulator configuration."""

from __future__ import annotations

import uuid
from datetime import UTC, datetime

from sqlalchemy import DateTime, String, Text
from sqlalchemy.dialects.postgresql import JSONB, UUID
from sqlalchemy.orm import Mapped, mapped_column

from orchestrator.database import Base


def _now() -> datetime:
    return datetime.now(UTC)


class DeviceProfile(Base):
    """A reusable device profile stored in PostgreSQL."""

    __tablename__ = "device_profiles"

    id: Mapped[uuid.UUID] = mapped_column(UUID(as_uuid=True), primary_key=True, default=uuid.uuid4)
    name: Mapped[str] = mapped_column(String(255), unique=True, nullable=False)
    type: Mapped[str] = mapped_column(String(255), nullable=False)
    protocol: Mapped[str] = mapped_column(String(32), nullable=False, default="console")
    topic_template: Mapped[str] = mapped_column(
        Text, nullable=False, default="devices/{device_id}/telemetry"
    )
    telemetry_interval: Mapped[str] = mapped_column(String(16), nullable=False, default="5s")
    # JSONB columns — stored as native dicts in Python
    telemetry_fields: Mapped[dict] = mapped_column(JSONB, nullable=False, default=dict)
    labels: Mapped[dict] = mapped_column(JSONB, nullable=False, default=dict)

    created_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True), nullable=False, default=_now
    )
    updated_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True), nullable=False, default=_now, onupdate=_now
    )
