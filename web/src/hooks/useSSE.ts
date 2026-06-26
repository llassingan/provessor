import { useState, useEffect, useRef, useCallback } from "react";

export interface SSEEvent {
  type: string;
  status: string;
  step: string;
  message: string;
  data?: unknown;
  timestamp: number;
}

interface SSEResult {
  events: SSEEvent[];
  connected: boolean;
}

export function useSSE(url: string): SSEResult {
  const [events, setEvents] = useState<SSEEvent[]>([]);
  const [connected, setConnected] = useState(false);
  const sourceRef = useRef<EventSource | null>(null);
  const retryTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const connect = useCallback(() => {
    if (sourceRef.current) {
      sourceRef.current.close();
    }

    const es = new EventSource(url, { withCredentials: true });
    sourceRef.current = es;

    es.onopen = () => {
      setConnected(true);
    };

    es.onmessage = (event: MessageEvent) => {
      try {
        const parsed = JSON.parse(event.data as string) as SSEEvent;
        setEvents((prev) => [...prev, parsed]);
      } catch {
        // ignore malformed events
      }
    };

    es.onerror = () => {
      setConnected(false);
      es.close();
      retryTimeoutRef.current = setTimeout(() => {
        connect();
      }, 3000);
    };
  }, [url]);

  useEffect(() => {
    connect();

    return () => {
      if (sourceRef.current) {
        sourceRef.current.close();
        sourceRef.current = null;
      }
      if (retryTimeoutRef.current) {
        clearTimeout(retryTimeoutRef.current);
        retryTimeoutRef.current = null;
      }
    };
  }, [connect]);

  return { events, connected };
}
