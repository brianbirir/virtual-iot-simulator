"""IoT Simulator CLI — entry point for the iot-sim command."""
from __future__ import annotations

import asyncio
import sys
from typing import Optional

import typer
from rich.console import Console
from rich.table import Table

from orchestrator.grpc_client import (
    RuntimeClient,
    generate_device_ids,
    make_label_selector,
)

app = typer.Typer(name="iot-sim", help="Virtual IoT Device Simulator CLI")
console = Console()

_DEFAULT_RUNTIME = "localhost:50051"


def _runtime_option() -> str:
    return typer.Option(_DEFAULT_RUNTIME, "--runtime", "-r", help="Runtime gRPC address")


# ---------------------------------------------------------------------------
# spawn
# ---------------------------------------------------------------------------


@app.command()
def spawn(
    profile: str = typer.Option(..., "--profile", "-p", help="Path to device profile YAML"),
    count: int = typer.Option(1, "--count", "-n", help="Number of devices to spawn"),
    runtime: str = typer.Option(_DEFAULT_RUNTIME, "--runtime", "-r"),
) -> None:
    """Spawn virtual devices from a YAML device profile."""
    asyncio.run(_spawn(profile, count, runtime))


async def _spawn(profile_path: str, count: int, runtime: str) -> None:
    from orchestrator.config import load_profile_specs

    specs = load_profile_specs(profile_path, count)
    async with RuntimeClient(runtime) as client:
        resp = await client.spawn(specs)
    console.print(f"[green]Spawned {resp.spawned} device(s)[/green]")
    if resp.failed_device_ids:
        console.print(f"[yellow]Failed: {list(resp.failed_device_ids)}[/yellow]")


# ---------------------------------------------------------------------------
# stop
# ---------------------------------------------------------------------------


@app.command()
def stop(
    all_devices: bool = typer.Option(False, "--all", help="Stop all devices"),
    device_type: Optional[str] = typer.Option(None, "--type", "-t", help="Stop by device_type label"),
    runtime: str = typer.Option(_DEFAULT_RUNTIME, "--runtime", "-r"),
) -> None:
    """Stop running devices."""
    asyncio.run(_stop(all_devices, device_type, runtime))


async def _stop(all_devices: bool, device_type: Optional[str], runtime: str) -> None:
    selector = None
    if device_type:
        selector = make_label_selector(f"device_type={device_type}")
    elif not all_devices:
        console.print("[red]Specify --all or --type[/red]")
        raise typer.Exit(1)

    async with RuntimeClient(runtime) as client:
        resp = await client.stop(selector=selector)
    console.print(f"[green]Stopped {resp.stopped} device(s)[/green]")


# ---------------------------------------------------------------------------
# status
# ---------------------------------------------------------------------------


@app.command()
def status(
    runtime: str = typer.Option(_DEFAULT_RUNTIME, "--runtime", "-r"),
) -> None:
    """Show fleet and runtime status."""
    asyncio.run(_status(runtime))


async def _status(runtime: str) -> None:
    async with RuntimeClient(runtime) as client:
        fleet = await client.status()
        rt = await client.runtime_status()

    table = Table(title="Fleet Status")
    table.add_column("Total Devices")
    table.add_column("By State")
    table.add_column("By Type")
    table.add_row(
        str(fleet.total_devices),
        str(dict(fleet.by_state)),
        str(dict(fleet.by_type)),
    )
    console.print(table)

    console.print(
        f"Runtime: goroutines={rt.goroutine_count}  "
        f"memory={rt.memory_bytes // 1024 // 1024}MB  "
        f"uptime={rt.uptime.ToTimedelta()}"
    )


# ---------------------------------------------------------------------------
# stream
# ---------------------------------------------------------------------------


@app.command()
def stream(
    device_type: Optional[str] = typer.Option(None, "--type", "-t"),
    device_ids: Optional[str] = typer.Option(None, "--ids", help="Comma-separated device IDs"),
    runtime: str = typer.Option(_DEFAULT_RUNTIME, "--runtime", "-r"),
) -> None:
    """Stream live telemetry from running devices."""
    asyncio.run(_stream(device_type, device_ids, runtime))


async def _stream(device_type: Optional[str], device_ids: Optional[str], runtime: str) -> None:
    from orchestrator.grpc_client import make_device_id_selector

    selector = None
    if device_ids:
        ids = [s.strip() for s in device_ids.split(",")]
        selector = make_device_id_selector(ids)
    elif device_type:
        selector = make_label_selector(f"device_type={device_type}")

    async with RuntimeClient(runtime) as client:
        try:
            async for batch in client.stream_telemetry(selector=selector, batch_size=50):
                for pt in batch.points:
                    val = _point_value(pt)
                    console.print(f"[cyan]{pt.device_id}[/cyan] {pt.metric_name}={val}  ts={pt.timestamp.ToDatetime()}")
        except KeyboardInterrupt:
            pass


def _point_value(pt: object) -> str:
    """Extract the oneof value from a TelemetryPoint as a string."""
    kind = pt.WhichOneof("value")
    if kind:
        return str(getattr(pt, kind))
    return "null"


if __name__ == "__main__":
    app()
