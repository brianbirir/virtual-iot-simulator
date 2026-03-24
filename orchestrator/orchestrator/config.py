"""Device profile loading and validation.

Profiles are YAML files that define a device type's telemetry schema,
protocol, and generator configuration. This module validates them via
Pydantic and converts them to protobuf DeviceSpec messages.
"""

from __future__ import annotations

import os
from pathlib import Path
from typing import Any, Literal

import yaml
from google.protobuf import duration_pb2, struct_pb2
from pydantic import BaseModel, field_validator
from simulator.v1.device_pb2 import DeviceSpec

from orchestrator.grpc_client import generate_device_ids

# ---------------------------------------------------------------------------
# Pydantic models for profile validation
# ---------------------------------------------------------------------------


class TelemetryFieldConfig(BaseModel):
    """Configuration for a single telemetry field on a device."""

    type: Literal["gaussian", "brownian", "diurnal", "markov", "static"]
    # Gaussian
    mean: float | None = None
    stddev: float | None = None
    # Brownian
    start: float | None = None
    drift: float | None = None
    volatility: float | None = None
    mean_reversion: float | None = None
    min: float | None = None
    max: float | None = None
    # Diurnal
    baseline: float | None = None
    amplitude: float | None = None
    peak_hour: int | None = None
    noise_stddev: float | None = None
    # Markov
    states: list[str] | None = None
    transition_matrix: list[list[float]] | None = None
    initial_state: str | None = None
    # Static
    value: Any | None = None


class DeviceProfileConfig(BaseModel):
    """Full device profile loaded from YAML."""

    type: str
    protocol: Literal["mqtt", "amqp", "http", "console"] = "console"
    topic_template: str = "devices/{device_id}/telemetry"
    telemetry_interval: str = "5s"
    telemetry_fields: dict[str, TelemetryFieldConfig] = {}
    labels: dict[str, str] = {}

    @field_validator("telemetry_interval")
    @classmethod
    def validate_interval(cls, v: str) -> str:
        _parse_duration(v)  # raises ValueError on bad format
        return v


# ---------------------------------------------------------------------------
# Duration parsing
# ---------------------------------------------------------------------------

_DURATION_SUFFIXES: dict[str, int] = {
    "ms": 1,
    "s": 1_000,
    "m": 60_000,
    "h": 3_600_000,
}


def _parse_duration(s: str) -> duration_pb2.Duration:
    """Parse a duration string like '5s', '500ms', '1m' into a protobuf Duration."""
    s = s.strip()
    for suffix, millis_per_unit in sorted(_DURATION_SUFFIXES.items(), key=lambda x: -len(x[0])):
        if s.endswith(suffix):
            try:
                value = float(s[: -len(suffix)])
            except ValueError as exc:
                raise ValueError(f"Invalid duration: {s!r}") from exc
            total_ms = value * millis_per_unit
            seconds = int(total_ms // 1000)
            nanos = int((total_ms % 1000) * 1_000_000)
            return duration_pb2.Duration(seconds=seconds, nanos=nanos)
    raise ValueError(f"Unrecognised duration format: {s!r} (expected e.g. '5s', '500ms', '1m')")


# ---------------------------------------------------------------------------
# Profile → DeviceSpec conversion
# ---------------------------------------------------------------------------


def _field_config_to_dict(field: TelemetryFieldConfig) -> dict[str, Any]:
    """Convert a TelemetryFieldConfig to a plain dict for proto Struct encoding."""
    raw = field.model_dump(exclude_none=True)
    # Rename noise_stddev to stddev if present for gaussian compatibility
    if "noise_stddev" in raw:
        raw["stddev"] = raw.pop("noise_stddev")
    return raw


def profile_to_spec(profile: DeviceProfileConfig, device_id: str) -> DeviceSpec:
    """Build a single DeviceSpec proto from a validated profile and device_id."""
    labels = dict(profile.labels)
    labels.setdefault("device_type", profile.type)

    # Encode telemetry_fields as google.protobuf.Struct
    fields_dict: dict[str, Any] = {
        name: _field_config_to_dict(field) for name, field in profile.telemetry_fields.items()
    }
    behavior_params = struct_pb2.Struct()
    behavior_params.update({"fields": fields_dict})

    return DeviceSpec(
        device_id=device_id,
        device_type=profile.type,
        labels=labels,
        telemetry_interval=_parse_duration(profile.telemetry_interval),
        behavior_params=behavior_params,
        protocol=profile.protocol,
        topic_template=profile.topic_template,
    )


# ---------------------------------------------------------------------------
# Public API
# ---------------------------------------------------------------------------


_PROFILES_DIR = Path(os.environ.get("IOT_SIM_PROFILES_DIR", "/profiles"))


def load_profile(path: str | Path) -> DeviceProfileConfig:
    """Load and validate a device profile YAML file.

    Relative paths are resolved against IOT_SIM_PROFILES_DIR (default: /profiles).
    """
    p = Path(path)
    if not p.is_absolute():
        p = _PROFILES_DIR / p
    with open(p) as f:
        raw = yaml.safe_load(f)
    return DeviceProfileConfig.model_validate(raw)


def load_profile_specs(path: str | Path, count: int, offset: int = 0) -> list[DeviceSpec]:
    """
    Load a profile YAML and generate count DeviceSpec messages.

    Device IDs follow the convention: {device_type}-{zero_padded_index}.
    """
    profile = load_profile(path)
    ids = generate_device_ids(profile.type, count, offset)
    return [profile_to_spec(profile, device_id) for device_id in ids]


def profile_to_specs_from_dict(data: dict, count: int, offset: int = 0) -> list[DeviceSpec]:
    """Build DeviceSpec messages from a plain dict (e.g. loaded from the database).

    ``data`` must match the DeviceProfileConfig schema.
    """
    profile = DeviceProfileConfig.model_validate(data)
    ids = generate_device_ids(profile.type, count, offset)
    return [profile_to_spec(profile, device_id) for device_id in ids]
