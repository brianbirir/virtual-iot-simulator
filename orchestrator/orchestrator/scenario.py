"""Scenario engine — SimClock, ScenarioContext, and ScenarioRunner.

Scenarios are async Python functions with the signature::

    async def run(ctx: ScenarioContext) -> None:
        ids = await ctx.spawn("temperature_sensor", count=100)
        await ctx.wait("10m")
        await ctx.inject_fault(ids=ids[:10], fault="DISCONNECT", duration="60s")
        await ctx.wait("2m")
        await ctx.log("done")

They are executed by ScenarioRunner, which loads the script from a file path
and injects a ScenarioContext backed by a real RuntimeClient.
"""
from __future__ import annotations

import asyncio
import importlib.util
import time
from pathlib import Path
from typing import Optional

from orchestrator.config import load_profile_specs
from orchestrator.grpc_client import (
    RuntimeClient,
    generate_device_ids,
    make_device_id_selector,
    make_label_selector,
)
from simulator.v1.orchestrator_pb2 import DeviceSelector, FleetStatus


# ---------------------------------------------------------------------------
# SimClock
# ---------------------------------------------------------------------------


class SimClock:
    """Simulation clock with a configurable time multiplier.

    At speed=1.0, ``wait("1h")`` sleeps for 1 real hour.
    At speed=60.0, ``wait("1h")`` sleeps for 1 real minute.
    """

    _SUFFIXES: dict[str, float] = {
        "ms": 0.001,
        "s": 1.0,
        "m": 60.0,
        "h": 3600.0,
    }

    def __init__(self, speed: float = 1.0):
        if speed <= 0:
            raise ValueError(f"speed must be > 0, got {speed}")
        self.speed = speed
        self._start_real = time.monotonic()
        self._start_sim = time.time()

    def parse_seconds(self, duration: str) -> float:
        """Parse a duration string like '5s', '500ms', '1m', '2h' → seconds."""
        s = duration.strip()
        for suffix, factor in sorted(self._SUFFIXES.items(), key=lambda x: -len(x[0])):
            if s.endswith(suffix):
                try:
                    value = float(s[: -len(suffix)])
                except ValueError as exc:
                    raise ValueError(f"Invalid duration: {duration!r}") from exc
                return value * factor
        raise ValueError(f"Unrecognised duration format: {duration!r}")

    async def sleep(self, duration: str) -> None:
        """Sleep for *duration* in simulation time (shortened by speed multiplier)."""
        real_seconds = self.parse_seconds(duration) / self.speed
        await asyncio.sleep(real_seconds)

    @property
    def sim_now(self) -> float:
        """Current simulation time as a Unix timestamp."""
        elapsed_real = time.monotonic() - self._start_real
        return self._start_sim + elapsed_real * self.speed


# ---------------------------------------------------------------------------
# ScenarioContext
# ---------------------------------------------------------------------------


class ScenarioContext:
    """Facade over RuntimeClient exposed to scenario scripts.

    All durations are strings like "5s", "500ms", "1m", "2h".
    """

    def __init__(self, client: RuntimeClient, clock: SimClock):
        self._client = client
        self._clock = clock

    # -- Fleet control --

    async def spawn(
        self,
        profile: str,
        count: int = 1,
        labels: Optional[dict[str, str]] = None,
        offset: int = 0,
    ) -> list[str]:
        """Spawn *count* devices from a YAML profile. Returns the device IDs."""
        specs = load_profile_specs(profile, count, offset)
        if labels:
            for spec in specs:
                spec.labels.update(labels)
        resp = await self._client.spawn(specs)
        return [s.device_id for s in specs if s.device_id not in resp.failed_device_ids]

    async def stop(
        self,
        ids: Optional[list[str]] = None,
        device_type: Optional[str] = None,
        graceful: bool = True,
    ) -> int:
        """Stop devices by ID list or device_type label. Returns count stopped."""
        selector = _build_selector(ids, device_type)
        resp = await self._client.stop(selector=selector, graceful=graceful)
        return resp.stopped

    async def inject_fault(
        self,
        fault: str,
        ids: Optional[list[str]] = None,
        device_type: Optional[str] = None,
        duration: str = "60s",
        params: Optional[dict] = None,
    ) -> None:
        """Inject a fault into matching devices.

        fault is one of: DISCONNECT, LATENCY_SPIKE, DATA_CORRUPTION,
        BATTERY_DRAIN, CLOCK_DRIFT.
        """
        from google.protobuf import duration_pb2, struct_pb2
        from simulator.v1.orchestrator_pb2 import FaultType, InjectFaultRequest

        fault_type = FaultType.Value(f"FAULT_TYPE_{fault.upper()}")
        selector = _build_selector(ids, device_type)

        dur_secs = self._clock.parse_seconds(duration)
        pb_dur = duration_pb2.Duration(
            seconds=int(dur_secs),
            nanos=int((dur_secs % 1) * 1e9),
        )

        pb_params = struct_pb2.Struct()
        if params:
            pb_params.update(params)

        req = InjectFaultRequest(
            fault_type=fault_type,
            duration=pb_dur,
            parameters=pb_params,
        )
        if selector:
            req.selector.CopyFrom(selector)

        await self._client.stub.InjectFault(req)

    async def update_behavior(
        self,
        behavior_params: dict,
        ids: Optional[list[str]] = None,
        device_type: Optional[str] = None,
    ) -> None:
        """Update generator parameters for matching devices at runtime."""
        from google.protobuf import struct_pb2
        from simulator.v1.orchestrator_pb2 import UpdateDeviceBehaviorRequest

        pb_params = struct_pb2.Struct()
        pb_params.update(behavior_params)

        selector = _build_selector(ids, device_type)
        req = UpdateDeviceBehaviorRequest(behavior_params=pb_params)
        if selector:
            req.selector.CopyFrom(selector)

        await self._client.stub.UpdateDeviceBehavior(req)

    # -- Observation --

    async def status(self) -> FleetStatus:
        """Return current fleet status."""
        return await self._client.status()

    async def log(self, message: str) -> None:
        """Emit a structured scenario log entry."""
        sim_ts = self._clock.sim_now
        print(f"[SCENARIO t={sim_ts:.1f}] {message}", flush=True)

    # -- Time --

    async def wait(self, duration: str) -> None:
        """Sleep for *duration* in simulation time."""
        await self._clock.sleep(duration)

    @property
    def clock(self) -> SimClock:
        return self._clock

    @property
    def client(self) -> RuntimeClient:
        """Raw RuntimeClient for advanced use."""
        return self._client


# ---------------------------------------------------------------------------
# ScenarioRunner
# ---------------------------------------------------------------------------


class ScenarioRunner:
    """Loads a scenario script and executes its ``run(ctx)`` coroutine."""

    def __init__(self, runtime_address: str, speed: float = 1.0):
        self._address = runtime_address
        self._clock = SimClock(speed)

    async def run(self, script_path: str) -> None:
        """Load *script_path* and call its async ``run(ctx)`` function."""
        path = Path(script_path).resolve()
        if not path.exists():
            raise FileNotFoundError(f"Scenario script not found: {path}")

        spec = importlib.util.spec_from_file_location("_scenario", path)
        module = importlib.util.module_from_spec(spec)  # type: ignore[arg-type]
        spec.loader.exec_module(module)  # type: ignore[union-attr]

        if not hasattr(module, "run"):
            raise AttributeError(f"Scenario script {path} must define an async def run(ctx)")

        async with RuntimeClient(self._address) as client:
            ctx = ScenarioContext(client, self._clock)
            await module.run(ctx)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _build_selector(
    ids: Optional[list[str]],
    device_type: Optional[str],
) -> Optional[DeviceSelector]:
    if ids:
        return make_device_id_selector(ids)
    if device_type:
        return make_label_selector(f"device_type={device_type}")
    return None
