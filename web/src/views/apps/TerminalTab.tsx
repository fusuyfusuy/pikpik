import { usePTY } from '../../hooks/usePTY';
import { Button } from '../../components/ui/Button';
import { Badge } from '../../components/ui/Badge';
import {
  Terminal as TerminalIcon,
  RotateCw,
} from 'lucide-react';

export interface TerminalTabProps {
  appId: string;
  appName?: string;
}

export function TerminalTab({ appId }: TerminalTabProps) {
  const { containerRef, status, error, reconnect } = usePTY({
    targetType: 'container',
    targetId: appId,
  });

  return (
    <div className="space-y-3 pt-2">
      {/* Terminal Header */}
      <div className="p-3 bg-zinc-950/80 rounded-xl border border-zinc-800 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <TerminalIcon className="h-4 w-4 text-cyan-400" />
          <span className="text-xs font-mono font-semibold text-zinc-200">
            Interactive Container Session (/bin/sh)
          </span>
          <Badge
            variant={
              status === 'connected'
                ? 'success'
                : status === 'connecting'
                ? 'warning'
                : 'error'
            }
            dot
          >
            {status}
          </Badge>
        </div>

        <div className="flex items-center gap-2">
          {status !== 'connected' && (
            <Button
              variant="outline"
              size="sm"
              onClick={reconnect}
              leftIcon={<RotateCw className="h-3 w-3" />}
            >
              Reconnect
            </Button>
          )}
        </div>
      </div>

      {error && (
        <div className="p-2.5 rounded-lg bg-rose-950/30 border border-rose-900/50 text-xs font-mono text-rose-400">
          ⚠ Session Error: {error}
        </div>
      )}

      {/* Terminal Viewport */}
      <div
        ref={containerRef}
        className="h-[520px] w-full bg-zinc-950 rounded-xl border border-zinc-800 p-2 overflow-hidden shadow-inner font-mono text-xs"
      />
    </div>
  );
}
