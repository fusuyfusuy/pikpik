import { useEffect, useRef, useState, useCallback } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { WebLinksAddon } from '@xterm/addon-web-links';
import { getToken } from '../lib/api';
import { TermResizeMessage, TermExitMessage } from '../lib/types';

export interface UsePTYOptions {
  targetType?: 'container' | 'swarm_task' | 'host_machine';
  targetId?: string;
  cmd?: string;
  onExit?: (exitInfo: TermExitMessage) => void;
}

export type PTYConnectionStatus = 'connecting' | 'connected' | 'disconnected' | 'error';

export function usePTY(options: UsePTYOptions = {}) {
  const { targetType, targetId, cmd, onExit } = options;
  const [status, setStatus] = useState<PTYConnectionStatus>('disconnected');
  const [error, setError] = useState<string | null>(null);

  const containerRef = useRef<HTMLDivElement | null>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const onExitRef = useRef(onExit);

  useEffect(() => {
    onExitRef.current = onExit;
  }, [onExit]);

  const sendBinary = useCallback((opcode: number, payload: Uint8Array | string) => {
    if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) return;

    let payloadBytes: Uint8Array;
    if (typeof payload === 'string') {
      payloadBytes = new TextEncoder().encode(payload);
    } else {
      payloadBytes = payload;
    }

    const frame = new Uint8Array(1 + payloadBytes.length);
    frame[0] = opcode;
    frame.set(payloadBytes, 1);
    wsRef.current.send(frame.buffer);
  }, []);

  const sendResize = useCallback(
    (cols: number, rows: number) => {
      const resizeMsg: TermResizeMessage = { cols, rows };
      sendBinary(0x01, JSON.stringify(resizeMsg));
    },
    [sendBinary]
  );

  const disconnect = useCallback(() => {
    if (wsRef.current) {
      wsRef.current.close();
      wsRef.current = null;
    }
    setStatus('disconnected');
  }, []);

  const connect = useCallback(() => {
    if (!targetId && targetType !== 'host_machine') return;
    disconnect();

    setStatus('connecting');
    setError(null);

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const host = window.location.host;
    const url = new URL(`${protocol}//${host}/ws/pty`);

    if (targetType) url.searchParams.set('target_type', targetType);
    if (targetId) url.searchParams.set('target_id', targetId);
    if (cmd) url.searchParams.set('cmd', cmd);

    const token = getToken();
    if (token) url.searchParams.set('token', token);

    const ws = new WebSocket(url.toString());
    ws.binaryType = 'arraybuffer';
    wsRef.current = ws;

    ws.onopen = () => {
      setStatus('connected');
      if (terminalRef.current && fitAddonRef.current) {
        fitAddonRef.current.fit();
        sendResize(terminalRef.current.cols, terminalRef.current.rows);
      }
    };

    ws.onmessage = (event: MessageEvent) => {
      if (!(event.data instanceof ArrayBuffer)) return;
      const bytes = new Uint8Array(event.data);
      if (bytes.length === 0) return;

      const opcode = bytes[0];
      const payload = bytes.subarray(1);

      switch (opcode) {
        case 0x00: {
          // Output from PTY -> Terminal
          if (terminalRef.current) {
            terminalRef.current.write(payload);
          }
          break;
        }
        case 0xff: {
          // Exit frame
          try {
            const exitMsg: TermExitMessage = JSON.parse(new TextDecoder().decode(payload));
            if (exitMsg.error) {
              setError(exitMsg.error);
              if (terminalRef.current) {
                terminalRef.current.writeln(`\r\n\x1b[31mSession Error: ${exitMsg.error}\x1b[0m`);
              }
            } else if (terminalRef.current) {
              terminalRef.current.writeln(`\r\n\x1b[90mSession exited with code ${exitMsg.exit_code}\x1b[0m`);
            }
            if (onExitRef.current) {
              onExitRef.current(exitMsg);
            }
          } catch {
            // Ignored
          }
          break;
        }
      }
    };

    ws.onerror = () => {
      setError('WebSocket terminal connection error');
      setStatus('error');
    };

    ws.onclose = () => {
      setStatus('disconnected');
    };
  }, [targetType, targetId, cmd, disconnect, sendResize]);

  // Terminal lifecycle & mounting
  useEffect(() => {
    if (!containerRef.current) return;

    const term = new Terminal({
      cursorBlink: true,
      cursorStyle: 'block',
      fontFamily: 'JetBrains Mono, Menlo, Monaco, Consolas, monospace',
      fontSize: 13,
      lineHeight: 1.2,
      theme: {
        background: '#09090b',
        foreground: '#f4f4f5',
        cursor: '#22d3ee',
        cursorAccent: '#09090b',
        selectionBackground: 'rgba(6, 182, 212, 0.3)',
        black: '#18181b',
        red: '#f43f5e',
        green: '#10b981',
        yellow: '#f59e0b',
        blue: '#3b82f6',
        magenta: '#d946ef',
        cyan: '#06b6d4',
        white: '#f4f4f5',
        brightBlack: '#71717a',
        brightRed: '#fb7185',
        brightGreen: '#34d399',
        brightYellow: '#fbbf24',
        brightBlue: '#60a5fa',
        brightMagenta: '#e879f9',
        brightCyan: '#22d3ee',
        brightWhite: '#ffffff',
      },
      convertEol: true,
    });

    const fitAddon = new FitAddon();
    const webLinksAddon = new WebLinksAddon();

    term.loadAddon(fitAddon);
    term.loadAddon(webLinksAddon);

    term.open(containerRef.current);
    fitAddon.fit();

    terminalRef.current = term;
    fitAddonRef.current = fitAddon;

    // Send stdin to PTY
    const dataDispose = term.onData((data) => {
      sendBinary(0x00, data);
    });

    // Send resize event to PTY
    const resizeDispose = term.onResize(({ cols, rows }) => {
      sendResize(cols, rows);
    });

    const handleWindowResize = () => {
      if (fitAddonRef.current) {
        try {
          fitAddonRef.current.fit();
        } catch {
          // Ignored if hidden
        }
      }
    };

    window.addEventListener('resize', handleWindowResize);

    return () => {
      window.removeEventListener('resize', handleWindowResize);
      dataDispose.dispose();
      resizeDispose.dispose();
      term.dispose();
      terminalRef.current = null;
      fitAddonRef.current = null;
    };
  }, [sendBinary, sendResize]);

  // Connect whenever target changes
  useEffect(() => {
    connect();
    return () => {
      disconnect();
    };
  }, [connect, disconnect]);

  const fit = useCallback(() => {
    if (fitAddonRef.current) {
      try {
        fitAddonRef.current.fit();
      } catch {
        // Ignored
      }
    }
  }, []);

  return {
    containerRef,
    terminal: terminalRef.current,
    status,
    error,
    fit,
    reconnect: connect,
    disconnect,
  };
}
