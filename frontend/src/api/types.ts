// ── Requests ──────────────────────────────────────────────────────────────────

export interface SpawnRequest {
  profile: string;
  count: number;
  runtime?: string;
}

export interface StopRequest {
  all_devices?: boolean;
  device_type?: string;
  runtime?: string;
}

// ── Responses ─────────────────────────────────────────────────────────────────

export interface SpawnResponse {
  spawned: number;
  failed: string[];
}

export interface StopResponse {
  stopped: number;
}

export interface FleetStatusResponse {
  total_devices: number;
  by_state: Record<string, number>;
  by_type: Record<string, number>;
}

export interface RuntimeStatusResponse {
  active_devices: number;
  goroutine_count: number;
  memory_mb: number;
  uptime_seconds: number;
}

export interface StatusResponse {
  fleet: FleetStatusResponse;
  runtime: RuntimeStatusResponse;
}

export interface HealthResponse {
  status: string;
}

// ── Telemetry stream ───────────────────────────────────────────────────────────

export interface TelemetryEvent {
  device_id: string;
  metric: string;
  value: number | string | boolean;
  timestamp: string;
}

export interface TelemetryStreamParams {
  device_type?: string;
  device_ids?: string;
  batch_size?: number;
}
