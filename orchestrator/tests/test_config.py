"""Tests for profile loading and DeviceSpec generation."""
import pytest
from pathlib import Path

PROFILES_DIR = Path(__file__).parent.parent.parent / "profiles"


def test_load_temperature_sensor_profile():
    from orchestrator.config import load_profile, load_profile_specs

    profile = load_profile(PROFILES_DIR / "temperature_sensor.yaml")
    assert profile.type == "temperature_sensor"
    assert "temperature" in profile.telemetry_fields
    assert profile.telemetry_fields["temperature"].type == "gaussian"


def test_load_profile_specs_count():
    from orchestrator.config import load_profile_specs

    specs = load_profile_specs(PROFILES_DIR / "temperature_sensor.yaml", 5)
    assert len(specs) == 5


def test_device_ids_sequential():
    from orchestrator.config import load_profile_specs

    specs = load_profile_specs(PROFILES_DIR / "temperature_sensor.yaml", 3)
    ids = [s.device_id for s in specs]
    assert ids == ["temperature_sensor-0000", "temperature_sensor-0001", "temperature_sensor-0002"]


def test_device_spec_has_behavior_params():
    from orchestrator.config import load_profile_specs

    specs = load_profile_specs(PROFILES_DIR / "temperature_sensor.yaml", 1)
    spec = specs[0]
    assert spec.behavior_params is not None
    params = spec.behavior_params.fields
    assert "fields" in params


def test_device_spec_labels_include_device_type():
    from orchestrator.config import load_profile_specs

    specs = load_profile_specs(PROFILES_DIR / "temperature_sensor.yaml", 1)
    assert specs[0].labels.get("device_type") == "temperature_sensor"
