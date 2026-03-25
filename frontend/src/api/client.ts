import type {
  DeviceProfile,
  HealthResponse,
  ProfileCreateRequest,
  ProfileUpdateRequest,
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

  // 204 No Content has no body
  if (res.status === 204) return undefined as T;

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

  profiles: {
    list: () => request<DeviceProfile[]>(`${BASE}/profiles`),
    get: (id: string) => request<DeviceProfile>(`${BASE}/profiles/${id}`),
    create: (body: ProfileCreateRequest) =>
      request<DeviceProfile>(`${BASE}/profiles`, {
        method: 'POST',
        body: JSON.stringify(body),
      }),
    update: (id: string, body: ProfileUpdateRequest) =>
      request<DeviceProfile>(`${BASE}/profiles/${id}`, {
        method: 'PUT',
        body: JSON.stringify(body),
      }),
    delete: (id: string) =>
      request<void>(`${BASE}/profiles/${id}`, { method: 'DELETE' }),
  },
};
