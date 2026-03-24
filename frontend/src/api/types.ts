// ── Device Profiles ────────────────────────────────────────────────────────────

export type GeneratorType = 'gaussian' | 'brownian' | 'diurnal' | 'markov' | 'static';
export type Protocol = 'mqtt' | 'amqp' | 'http' | 'console';

export interface TelemetryFieldConfig {
  type: GeneratorType;
  // gaussian
  mean?: number;
  stddev?: number;
  // brownian
  start?: number;
  drift?: number;
  volatility?: number;
  mean_reversion?: number;
  min?: number;
  max?: number;
  // diurnal
  baseline?: number;
  amplitude?: number;
  peak_hour?: number;
  noise_stddev?: number;
  // markov
  states?: string[];
  transition_matrix?: number[][];
  initial_state?: string;
  // static
  value?: number;
}

export interface DeviceProfileBase {
  name: string;
  type: string;
  protocol: Protocol;
  topic_template: string;
  telemetry_interval: string;
  telemetry_fields: Record<string, TelemetryFieldConfig>;
  labels: Record<string, string>;
}

export interface DeviceProfile extends DeviceProfileBase {
  id: string;
  created_at: string;
  updated_at: string;
}

export type ProfileCreateRequest = DeviceProfileBase;
export type ProfileUpdateRequest = Partial<DeviceProfileBase>;

// ── Requests ──────────────────────────────────────────────────────────────────

export interface SpawnRequest {
  profile_id: string;
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
