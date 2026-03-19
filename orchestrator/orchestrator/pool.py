"""RuntimePool — consistent-hash routing across multiple Device Runtime instances.

Distributes devices across a pool of runtime gRPC endpoints so the fleet can
exceed the capacity of a single process. Spawn requests are routed to the
runtime that owns the device's hash slot; stop and fault-inject requests are
fanned out to all instances.
"""

from __future__ import annotations

import asyncio
import bisect
import hashlib
from collections import defaultdict
from typing import Optional

from orchestrator.grpc_client import (
    RuntimeClient,
    make_device_id_selector,
    make_label_selector,
)
from simulator.v1.device_pb2 import DeviceSpec
from simulator.v1.orchestrator_pb2 import (
    DeviceSelector,
    FleetStatus,
    RuntimeStatus,
    SpawnDevicesResponse,
    StopDevicesResponse,
)


# ---------------------------------------------------------------------------
# Consistent hash ring
# ---------------------------------------------------------------------------


class _HashRing:
    """Simple consistent hash ring with virtual nodes for load distribution."""

    def __init__(self, nodes: list[str], vnodes: int = 150):
        self._ring: dict[int, str] = {}
        self._keys: list[int] = []
        for node in nodes:
            for i in range(vnodes):
                key = self._hash(f"{node}#{i}")
                self._ring[key] = node
                self._keys.append(key)
        self._keys.sort()

    def _hash(self, value: str) -> int:
        return int(hashlib.md5(value.encode(), usedforsecurity=False).hexdigest(), 16)

    def get(self, key: str) -> str:
        if not self._ring:
            raise ValueError("Hash ring is empty")
        h = self._hash(key)
        idx = bisect.bisect(self._keys, h) % len(self._keys)
        return self._ring[self._keys[idx]]

    @property
    def nodes(self) -> list[str]:
        return list(dict.fromkeys(self._ring.values()))  # unique, insertion-order


# ---------------------------------------------------------------------------
# RuntimePool
# ---------------------------------------------------------------------------


class RuntimePool:
    """Manages multiple RuntimeClient connections with consistent-hash routing.

    Usage::

        async with RuntimePool(["host1:50051", "host2:50051"]) as pool:
            await pool.spawn(specs)
            await pool.stop()
    """

    def __init__(self, addresses: list[str]):
        if not addresses:
            raise ValueError("RuntimePool requires at least one address")
        self._ring = _HashRing(addresses)
        self._clients: dict[str, RuntimeClient] = {}

    async def connect(self) -> None:
        """Open gRPC channels to all runtime instances."""
        for addr in self._ring.nodes:
            client = RuntimeClient(addr)
            await client.connect()
            self._clients[addr] = client

    async def close(self) -> None:
        """Close all gRPC channels."""
        await asyncio.gather(*(c.close() for c in self._clients.values()), return_exceptions=True)

    async def __aenter__(self) -> "RuntimePool":
        await self.connect()
        return self

    async def __aexit__(self, *_: object) -> None:
        await self.close()

    # ------------------------------------------------------------------
    # Fleet operations
    # ------------------------------------------------------------------

    async def spawn(
        self,
        specs: list[DeviceSpec],
        scenario_id: str = "",
    ) -> dict[str, SpawnDevicesResponse]:
        """Spawn devices, routing each spec to its consistent-hash runtime.

        Returns a map of address → SpawnDevicesResponse.
        """
        groups: dict[str, list[DeviceSpec]] = defaultdict(list)
        for spec in specs:
            addr = self._ring.get(spec.device_id)
            groups[addr].append(spec)

        responses = await asyncio.gather(
            *(
                self._clients[addr].spawn(group_specs, scenario_id)
                for addr, group_specs in groups.items()
            )
        )
        return dict(zip(groups.keys(), responses))

    async def stop(
        self,
        selector: Optional[DeviceSelector] = None,
        graceful: bool = True,
    ) -> list[StopDevicesResponse]:
        """Stop devices on all instances. Fan-out for label selectors and --all."""
        return list(
            await asyncio.gather(
                *(c.stop(selector=selector, graceful=graceful) for c in self._clients.values())
            )
        )

    async def status(self, selector: Optional[DeviceSelector] = None) -> list[FleetStatus]:
        """Return fleet status from all instances."""
        return list(
            await asyncio.gather(*(c.status(selector=selector) for c in self._clients.values()))
        )

    async def runtime_status(self) -> list[RuntimeStatus]:
        """Return runtime health metrics from all instances."""
        return list(await asyncio.gather(*(c.runtime_status() for c in self._clients.values())))

    def client_for(self, device_id: str) -> RuntimeClient:
        """Return the client responsible for a given device_id."""
        addr = self._ring.get(device_id)
        return self._clients[addr]
