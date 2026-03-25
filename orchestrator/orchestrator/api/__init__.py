"""FastAPI REST API for the IoT Simulator orchestrator.

Exposes fleet management operations and device profile CRUD via HTTP.

Base path: /api/v1
"""

from __future__ import annotations

from fastapi import FastAPI

from orchestrator.database import init_db

from .routes import devices, profiles

app = FastAPI(
    title="IoT Simulator API",
    description="REST interface for the virtual IoT device simulator orchestrator.",
    version="1.0.0",
)


@app.on_event("startup")
async def _startup() -> None:
    await init_db()


app.include_router(profiles.router)
app.include_router(devices.router)


@app.get("/api/v1/health", tags=["meta"])
async def health() -> dict:
    """Health check — returns 200 if the API process is running."""
    return {"status": "ok"}
