import { useEffect, useRef, useState, useCallback } from 'react';
import { getToken } from '../lib/api';

export type SSEConnectionStatus = 'connecting' | 'connected' | 'disconnected' | 'error';

export interface UseSSEOptions<T> {
  endpoint: string; // e.g. '/api/v1/events/stream' or '/api/v1/stats/stream'
  onMessage?: (data: T) => void;
  enabled?: boolean;
  maxReconnectAttempts?: number;
  reconnectInterval?: number;
}

export function useSSE<T = unknown>({
  endpoint,
  onMessage,
  enabled = true,
  maxReconnectAttempts = 10,
  reconnectInterval = 3000,
}: UseSSEOptions<T>) {
  const [data, setData] = useState<T | null>(null);
  const [status, setStatus] = useState<SSEConnectionStatus>('disconnected');
  const [error, setError] = useState<Error | null>(null);
  
  const eventSourceRef = useRef<EventSource | null>(null);
  const reconnectAttemptsRef = useRef(0);
  const reconnectTimeoutRef = useRef<number | null>(null);
  const onMessageRef = useRef(onMessage);

  useEffect(() => {
    onMessageRef.current = onMessage;
  }, [onMessage]);

  const disconnect = useCallback(() => {
    if (reconnectTimeoutRef.current) {
      window.clearTimeout(reconnectTimeoutRef.current);
      reconnectTimeoutRef.current = null;
    }
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
      eventSourceRef.current = null;
    }
    setStatus('disconnected');
  }, []);

  const connect = useCallback(() => {
    if (!enabled) return;
    disconnect();

    setStatus('connecting');
    const token = getToken();
    const url = new URL(endpoint, window.location.origin);
    if (token) {
      url.searchParams.set('token', token);
    }

    try {
      const es = new EventSource(url.toString(), { withCredentials: true });
      eventSourceRef.current = es;

      es.onopen = () => {
        setStatus('connected');
        setError(null);
        reconnectAttemptsRef.current = 0;
      };

      es.onmessage = (event) => {
        try {
          const parsed = JSON.parse(event.data) as T;
          setData(parsed);
          if (onMessageRef.current) {
            onMessageRef.current(parsed);
          }
        } catch {
          // If plain text message
          setData(event.data as unknown as T);
          if (onMessageRef.current) {
            onMessageRef.current(event.data as unknown as T);
          }
        }
      };

      es.onerror = () => {
        setStatus('error');
        es.close();
        eventSourceRef.current = null;

        if (reconnectAttemptsRef.current < maxReconnectAttempts) {
          const backoff = Math.min(
            reconnectInterval * Math.pow(1.5, reconnectAttemptsRef.current),
            30000
          );
          reconnectAttemptsRef.current += 1;
          reconnectTimeoutRef.current = window.setTimeout(() => {
            connect();
          }, backoff);
        } else {
          setError(new Error('Max SSE reconnection attempts exceeded'));
          setStatus('disconnected');
        }
      };
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)));
      setStatus('error');
    }
  }, [endpoint, enabled, maxReconnectAttempts, reconnectInterval, disconnect]);

  useEffect(() => {
    if (enabled) {
      connect();
    } else {
      disconnect();
    }

    return () => {
      disconnect();
    };
  }, [enabled, endpoint, connect, disconnect]);

  return {
    data,
    status,
    error,
    reconnect: connect,
    disconnect,
  };
}
