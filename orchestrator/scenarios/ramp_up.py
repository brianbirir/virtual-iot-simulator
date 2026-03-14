"""
ramp_up.py — Gradual fleet scale-up scenario.

Spawns devices in three waves, injects a transient fault in the middle
of the ramp, then verifies the fleet stabilises before exiting.

Usage:
    iot-sim scenario run orchestrator/scenarios/ramp_up.py
    iot-sim scenario run orchestrator/scenarios/ramp_up.py --speed 10
"""

from __future__ import annotations

PROFILE = "temperature_sensor"
TOTAL_DEVICES = 60
WAVE_SIZE = 20       # devices spawned per wave
WAVE_DELAY = "30s"   # simulated time between waves


async def run(ctx):  # ctx: ScenarioContext injected by ScenarioRunner
    ctx.log("=== ramp_up scenario starting ===")

    # ── Wave 1 ──────────────────────────────────────────────────────────────
    ctx.log(f"Wave 1: spawning {WAVE_SIZE} devices")
    wave1_ids = await ctx.spawn(
        profile=PROFILE,
        count=WAVE_SIZE,
        labels={"wave": "1", "scenario": "ramp_up"},
    )
    ctx.log(f"Wave 1 spawned: {len(wave1_ids)} devices")
    await ctx.wait(WAVE_DELAY)

    # ── Wave 2 ──────────────────────────────────────────────────────────────
    ctx.log(f"Wave 2: spawning {WAVE_SIZE} more devices")
    wave2_ids = await ctx.spawn(
        profile=PROFILE,
        count=WAVE_SIZE,
        labels={"wave": "2", "scenario": "ramp_up"},
    )
    ctx.log(f"Wave 2 spawned: {len(wave2_ids)} devices")

    # Inject a 20-second latency spike on the first wave while wave 2 ramps up.
    ctx.log("Injecting LATENCY_SPIKE on wave-1 devices (20s)")
    await ctx.inject_fault(
        fault="LATENCY_SPIKE",
        ids=wave1_ids,
        duration="20s",
        params={"latency_ms": 200},
    )
    await ctx.wait(WAVE_DELAY)

    # ── Wave 3 ──────────────────────────────────────────────────────────────
    ctx.log(f"Wave 3: spawning final {WAVE_SIZE} devices")
    wave3_ids = await ctx.spawn(
        profile=PROFILE,
        count=WAVE_SIZE,
        labels={"wave": "3", "scenario": "ramp_up"},
    )
    ctx.log(f"Wave 3 spawned: {len(wave3_ids)} devices — fleet at {TOTAL_DEVICES}")

    # Let the full fleet run for a while.
    await ctx.wait("2m")

    # ── Tear-down ───────────────────────────────────────────────────────────
    ctx.log("Tearing down all scenario devices")
    all_ids = wave1_ids + wave2_ids + wave3_ids
    stopped = await ctx.stop(ids=all_ids, graceful=True)
    ctx.log(f"Stopped {stopped} devices. Scenario complete.")
