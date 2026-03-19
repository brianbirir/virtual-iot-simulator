import type {
  HealthResponse,
  SpawnRequest,
  SpawnResponse,
  StatusResponse,
  StopRequest,
  StopResponse,
} from './types';

const BASE = '/api/v1';

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, {
    headers: { 'Content-Type': 'application/json' },
    ...init,
  });

  if (!res.ok) {
    let detail = `HTTP ${res.status}`;
    try {
      const err = (await res.json()) as { detail?: string };
      if (err.detail) detail = err.detail;
    } catch {
      // ignore parse errors
    }
    throw new Error(detail);
  }

  return res.json() as Promise<T>;
}

export const api = {
  health: () => request<HealthResponse>(`${BASE}/health`),

  status: () => request<StatusResponse>(`${BASE}/devices/status`),

  spawn: (body: SpawnRequest) =>
    request<SpawnResponse>(`${BASE}/devices/spawn`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  stop: (body: StopRequest) =>
    request<StopResponse>(`${BASE}/devices/stop`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),
};
