import React, { useState } from 'react';
import { Copy, Check, Search, Code2, RefreshCw } from 'lucide-react';
import { Button } from '../../../../components/ui/Button';
import { Input } from '../../../../components/ui/Input';
import { useToast } from '../../../../components/ui/Toast';

interface LiveCaddyConfigProps {
  config: any;
  isLoading?: boolean;
  onRefresh?: () => void;
}

export function LiveCaddyConfig({
  config,
  isLoading = false,
  onRefresh,
}: LiveCaddyConfigProps) {
  const toast = useToast();
  const [searchFilter, setSearchFilter] = useState('');
  const [isCopied, setIsCopied] = useState(false);

  const formattedJSON = React.useMemo(() => {
    if (!config) return '// No active Caddy configuration loaded';
    try {
      if (typeof config === 'string') {
        return JSON.stringify(JSON.parse(config), null, 2);
      }
      return JSON.stringify(config, null, 2);
    } catch {
      return String(config);
    }
  }, [config]);

  const handleCopy = () => {
    navigator.clipboard.writeText(formattedJSON);
    setIsCopied(true);
    toast.success('Copied', 'Caddy JSON configuration copied to clipboard');
    setTimeout(() => setIsCopied(false), 2000);
  };

  // Filter lines if search query is provided
  const displayLines = React.useMemo(() => {
    const lines = formattedJSON.split('\n');
    if (!searchFilter.trim()) return lines;

    const query = searchFilter.toLowerCase();
    return lines.filter((line) => line.toLowerCase().includes(query));
  }, [formattedJSON, searchFilter]);

  return (
    <div className="space-y-3">
      {/* Top Toolbar */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <Code2 className="w-4 h-4 text-cyan-400" />
          <h4 className="text-xs font-semibold text-zinc-200 uppercase tracking-wider">
            Live Config Inspector (GET http://127.0.0.1:2019/config/)
          </h4>
        </div>

        <div className="flex items-center gap-2">
          <div className="w-48 sm:w-64 relative">
            <Search className="w-3.5 h-3.5 absolute left-2.5 top-1/2 -translate-y-1/2 text-zinc-500 pointer-events-none" />
            <Input
              type="text"
              placeholder="Search JSON keys/values..."
              value={searchFilter}
              onChange={(e) => setSearchFilter(e.target.value)}
              className="pl-8 h-8 text-xs bg-zinc-900 border-zinc-800"
            />
          </div>

          <Button
            variant="outline"
            size="sm"
            onClick={handleCopy}
            className="flex items-center gap-1.5 h-8 text-xs"
          >
            {isCopied ? (
              <>
                <Check className="w-3.5 h-3.5 text-emerald-400" />
                <span className="text-emerald-400">Copied</span>
              </>
            ) : (
              <>
                <Copy className="w-3.5 h-3.5" />
                <span>Copy JSON</span>
              </>
            )}
          </Button>

          {onRefresh && (
            <Button
              variant="ghost"
              size="sm"
              onClick={onRefresh}
              disabled={isLoading}
              className="h-8 w-8 p-0 text-zinc-400 hover:text-zinc-200"
              title="Refresh Live Config"
            >
              <RefreshCw className={`w-3.5 h-3.5 ${isLoading ? 'animate-spin text-cyan-400' : ''}`} />
            </Button>
          )}
        </div>
      </div>

      {/* Code Display Container */}
      <div className="relative rounded-xl border border-zinc-800/80 bg-zinc-950/90 overflow-hidden shadow-inner">
        <div className="flex items-center justify-between px-4 py-2 bg-zinc-900/70 border-b border-zinc-800/80 text-[11px] font-mono text-zinc-400">
          <span>caddy.json (In-Memory Dynamic Admin Tree)</span>
          <span>{displayLines.length} lines</span>
        </div>

        <div className="p-4 max-h-[480px] overflow-y-auto font-mono text-xs text-zinc-300 leading-relaxed space-y-0.5">
          {isLoading ? (
            <div className="py-12 text-center text-zinc-500 text-xs">
              <RefreshCw className="w-5 h-5 animate-spin mx-auto mb-2 text-cyan-400 opacity-60" />
              Fetching live Caddy REST state...
            </div>
          ) : displayLines.length === 0 ? (
            <div className="py-8 text-center text-zinc-500 text-xs">
              No matching lines in Caddy JSON configuration for "{searchFilter}"
            </div>
          ) : (
            displayLines.map((line, idx) => (
              <div key={idx} className="hover:bg-zinc-900/40 px-1 rounded flex">
                <span className="w-10 select-none text-zinc-600 text-right pr-3 shrink-0">
                  {idx + 1}
                </span>
                <span className="whitespace-pre overflow-x-auto text-cyan-200/90">
                  {line}
                </span>
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  );
}
