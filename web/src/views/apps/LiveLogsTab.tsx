import { useState, useEffect, useRef, useMemo } from 'react';
import { useSSE } from '../../hooks/useSSE';
import { useVirtualizer } from '@tanstack/react-virtual';
import { AnsiUp } from 'ansi_up';
import { Button } from '../../components/ui/Button';
import {
  FileText,
  Search,
  Trash2,
  Download,
  ArrowDown,
} from 'lucide-react';

const ansi = new AnsiUp();

export interface LiveLogsTabProps {
  appId: string;
}

export function LiveLogsTab({ appId }: { appId: string }) {
  const [logs, setLogs] = useState<string[]>([]);
  const [regexFilter, setRegexFilter] = useState('');
  const [autoScroll, setAutoScroll] = useState(true);
  const parentRef = useRef<HTMLDivElement>(null);

  // SSE Log Stream connection
  const { status } = useSSE<any>({
    endpoint: `/api/v1/apps/${appId}/logs/stream`,
    onMessage: (msg) => {
      let line = '';
      if (typeof msg === 'string') {
        line = msg;
      } else if (msg && typeof msg === 'object') {
        line = msg.log || msg.message || msg.text || JSON.stringify(msg);
      }
      if (line) {
        setLogs((prev) => {
          const next = [...prev, line];
          if (next.length > 3000) {
            return next.slice(next.length - 3000);
          }
          return next;
        });
      }
    },
  });

  // Filtered logs
  const filteredLogs = useMemo(() => {
    if (!regexFilter.trim()) return logs;
    try {
      const re = new RegExp(regexFilter, 'i');
      return logs.filter((l) => re.test(l));
    } catch {
      // Fallback to substring matching if regex is invalid
      const term = regexFilter.toLowerCase();
      return logs.filter((l) => l.toLowerCase().includes(term));
    }
  }, [logs, regexFilter]);

  const rowVirtualizer = useVirtualizer({
    count: filteredLogs.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => 22,
    overscan: 20,
  });

  useEffect(() => {
    if (autoScroll && filteredLogs.length > 0 && parentRef.current) {
      rowVirtualizer.scrollToIndex(filteredLogs.length - 1, { align: 'end' });
    }
  }, [filteredLogs.length, autoScroll, rowVirtualizer]);

  const handleDownload = () => {
    const blob = new Blob([logs.join('\n')], { type: 'text/plain;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `${appId}-logs-${new Date().toISOString().slice(0, 19)}.log`;
    link.click();
    URL.revokeObjectURL(url);
  };

  return (
    <div className="space-y-3 pt-2">
      {/* Controls Bar */}
      <div className="p-3 bg-zinc-950/80 rounded-xl border border-zinc-800 flex flex-col md:flex-row md:items-center justify-between gap-3">
        {/* Search / Regex Filter */}
        <div className="relative flex-1 max-w-md">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-zinc-500" />
          <input
            type="text"
            placeholder="Filter logs by keyword or regex (e.g. error|warn|GET /)..."
            value={regexFilter}
            onChange={(e) => setRegexFilter(e.target.value)}
            className="w-full bg-zinc-900 border border-zinc-800 rounded-lg pl-9 pr-8 py-1.5 text-xs font-mono text-zinc-100 placeholder-zinc-500 focus:outline-none focus:border-cyan-500"
          />
          {regexFilter && (
            <button
              onClick={() => setRegexFilter('')}
              className="absolute right-2.5 top-1/2 -translate-y-1/2 text-zinc-500 hover:text-zinc-300 text-xs"
            >
              ×
            </button>
          )}
        </div>

        {/* Action Buttons & Status */}
        <div className="flex items-center gap-2 flex-wrap justify-between md:justify-end">
          <div className="flex items-center gap-2">
            <span
              className={`flex items-center gap-1.5 text-xs font-mono px-2 py-0.5 rounded border ${
                status === 'connected'
                  ? 'bg-emerald-950/40 text-emerald-400 border-emerald-800/40'
                  : status === 'connecting'
                  ? 'bg-amber-950/40 text-amber-400 border-amber-800/40'
                  : 'bg-zinc-900 text-zinc-400 border-zinc-800'
              }`}
            >
              <span
                className={`h-1.5 w-1.5 rounded-full ${
                  status === 'connected'
                    ? 'bg-emerald-400 animate-pulse'
                    : status === 'connecting'
                    ? 'bg-amber-400 animate-pulse'
                    : 'bg-zinc-500'
                }`}
              />
              {status === 'connected'
                ? 'Live SSE'
                : status === 'connecting'
                ? 'Connecting...'
                : 'Disconnected'}
            </span>

            <span className="text-[11px] text-zinc-500 font-mono">
              {filteredLogs.length} / {logs.length} lines
            </span>
          </div>

          <div className="flex items-center gap-1.5">
            <button
              type="button"
              onClick={() => setAutoScroll(!autoScroll)}
              className={`px-2.5 py-1 rounded text-xs font-mono flex items-center gap-1 border transition-colors ${
                autoScroll
                  ? 'bg-cyan-950/60 border-cyan-800/50 text-cyan-300'
                  : 'bg-zinc-900 border-zinc-800 text-zinc-400 hover:text-zinc-200'
              }`}
              title="Toggle auto-scroll to bottom"
            >
              <ArrowDown className="h-3 w-3" />
              <span>Scroll</span>
            </button>

            <Button
              variant="outline"
              size="sm"
              onClick={handleDownload}
              disabled={logs.length === 0}
              leftIcon={<Download className="h-3 w-3 text-zinc-400" />}
            >
              Export
            </Button>

            <Button
              variant="ghost"
              size="sm"
              onClick={() => setLogs([])}
              disabled={logs.length === 0}
              leftIcon={<Trash2 className="h-3 w-3 text-zinc-400" />}
            >
              Clear
            </Button>
          </div>
        </div>
      </div>

      {/* Log Console Viewport */}
      <div
        ref={parentRef}
        className="h-[520px] w-full bg-zinc-950 rounded-xl border border-zinc-800 p-3.5 overflow-y-auto font-mono text-xs leading-relaxed select-text shadow-inner"
        onScroll={(e) => {
          const target = e.currentTarget;
          const isAtBottom = target.scrollHeight - target.scrollTop <= target.clientHeight + 50;
          if (isAtBottom !== autoScroll) {
            setAutoScroll(isAtBottom);
          }
        }}
      >
        {filteredLogs.length === 0 ? (
          <div className="h-full flex flex-col items-center justify-center text-zinc-600 font-mono text-xs space-y-2">
            <FileText className="h-6 w-6 text-zinc-700" />
            <span>
              {status === 'connecting'
                ? 'Connecting to live service stream...'
                : regexFilter
                ? `No log lines matching "${regexFilter}"`
                : 'Waiting for incoming application logs...'}
            </span>
          </div>
        ) : (
          <div
            style={{
              height: `${rowVirtualizer.getTotalSize()}px`,
              width: '100%',
              position: 'relative',
            }}
          >
            {rowVirtualizer.getVirtualItems().map((virtualRow) => (
              <div
                key={virtualRow.index}
                style={{
                  position: 'absolute',
                  top: 0,
                  left: 0,
                  width: '100%',
                  height: `${virtualRow.size}px`,
                  transform: `translateY(${virtualRow.start}px)`,
                }}
                className="truncate text-zinc-300 hover:bg-zinc-900/60 px-1 py-0.5 rounded"
                dangerouslySetInnerHTML={{
                  __html: ansi.ansi_to_html(filteredLogs[virtualRow.index]),
                }}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
