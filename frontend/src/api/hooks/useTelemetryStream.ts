import { useCallback, useEffect, useRef, useState } from 'react';
import type { TelemetryEvent, TelemetryStreamParams } from '../types';

const MAX_EVENTS = 500;

export function useTelemetryStream() {
  const [events, setEvents] = useState<TelemetryEvent[]>([]);
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const esRef = useRef<EventSource | null>(null);

  const connect = useCallback((params: TelemetryStreamParams) => {
    esRef.current?.close();

    const qs = new URLSearchParams();
    if (params.device_type) qs.set('device_type', params.device_type);
    if (params.device_ids) qs.set('device_ids', params.device_ids);
    if (params.batch_size != null) qs.set('batch_size', String(params.batch_size));

    const url = `/api/v1/devices/stream${qs.size ? `?${qs.toString()}` : ''}`;
    const es = new EventSource(url);
    esRef.current = es;

    es.onopen = () => {
      setConnected(true);
      setError(null);
    };

    es.onmessage = (ev: MessageEvent<string>) => {
      try {
        const data: unknown = JSON.parse(ev.data);
        const incoming: TelemetryEvent[] = Array.isArray(data)
          ? (data as TelemetryEvent[])
          : [data as TelemetryEvent];
        setEvents((prev) => {
          const next = [...prev, ...incoming];
          return next.length > MAX_EVENTS ? next.slice(next.length - MAX_EVENTS) : next;
        });
      } catch {
        // ignore parse errors
      }
    };

    es.onerror = () => {
      setConnected(false);
      setError('Connection lost. The stream may have ended or the server is unreachable.');
      es.close();
    };
  }, []);

  const disconnect = useCallback(() => {
    esRef.current?.close();
    esRef.current = null;
    setConnected(false);
  }, []);

  const clearEvents = useCallback(() => setEvents([]), []);

  // cleanup on unmount
  useEffect(() => () => { esRef.current?.close(); }, []);

  return { events, connected, error, connect, disconnect, clearEvents };
}
