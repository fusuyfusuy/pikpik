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

  const abortControllerRef = useRef<AbortController | null>(null);
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
    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
      abortControllerRef.current = null;
    }
    setStatus('disconnected');
  }, []);

  const connect = useCallback(() => {
    if (!enabled) return;
    disconnect();

    setStatus('connecting');
    setError(null);

    const abortController = new AbortController();
    abortControllerRef.current = abortController;

    const token = getToken();
    const headers: Record<string, string> = {
      Accept: 'text/event-stream',
    };
    if (token) {
      headers['Authorization'] = `Bearer ${token}`;
    }

    const url = new URL(endpoint, window.location.origin);

    const scheduleReconnect = () => {
      if (abortController.signal.aborted) return;
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

    (async () => {
      try {
        const response = await fetch(url.toString(), {
          headers,
          signal: abortController.signal,
        });

        if (!response.ok) {
          throw new Error(`HTTP error ${response.status}: ${response.statusText}`);
        }

        if (!response.body) {
          throw new Error('Response body is null');
        }

        setStatus('connected');
        reconnectAttemptsRef.current = 0;

        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';

        while (true) {
          const { value, done } = await reader.read();
          if (done) break;

          buffer += decoder.decode(value, { stream: true });
          const blocks = buffer.split('\n\n');
          buffer = blocks.pop() || '';

          for (const block of blocks) {
            if (!block.trim()) continue;
            const lines = block.split('\n');
            const dataLines: string[] = [];

            for (const line of lines) {
              if (line.startsWith('data:')) {
                dataLines.push(line.slice(5).trimStart());
              }
            }

            if (dataLines.length > 0) {
              const fullData = dataLines.join('\n');
              try {
                const parsed = JSON.parse(fullData) as T;
                setData(parsed);
                onMessageRef.current?.(parsed);
              } catch {
                setData(fullData as unknown as T);
                onMessageRef.current?.(fullData as unknown as T);
              }
            }
          }
        }

        if (!abortController.signal.aborted) {
          setStatus('disconnected');
          scheduleReconnect();
        }
      } catch (err: unknown) {
        if (abortController.signal.aborted) return;
        const e = err instanceof Error ? err : new Error(String(err));
        setError(e);
        setStatus('error');
        scheduleReconnect();
      }
    })();
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

  // Reset reconnect attempts and reconnect on tab visibility restore
  useEffect(() => {
    const handleVisibilityChange = () => {
      if (document.visibilityState === 'visible' && enabled && status === 'disconnected') {
        reconnectAttemptsRef.current = 0;
        connect();
      }
    };
    document.addEventListener('visibilitychange', handleVisibilityChange);
    return () => {
      document.removeEventListener('visibilitychange', handleVisibilityChange);
    };
  }, [enabled, status, connect]);

  return {
    data,
    status,
    error,
    reconnect: () => {
      reconnectAttemptsRef.current = 0;
      connect();
    },
    disconnect,
  };
}
