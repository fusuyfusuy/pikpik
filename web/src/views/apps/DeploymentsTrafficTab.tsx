import { useState, useEffect, useRef } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../../lib/api';
import { App, Build } from '../../lib/types';
import { Card } from '../../components/ui/Card';
import { Button } from '../../components/ui/Button';
import { Badge } from '../../components/ui/Badge';
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '../../components/ui/Table';
import { Modal } from '../../components/ui/Modal';
import { useToast } from '../../components/ui/Toast';
import { formatDate, formatDuration } from '../../lib/utils';
import { useSSE } from '../../hooks/useSSE';
import { useVirtualizer } from '@tanstack/react-virtual';
import { AnsiUp } from 'ansi_up';
import {
  Zap,
  RotateCw,
  GitCommit,
  Hammer,
  Loader2,
  FileText,
  Check,
  Copy,
  Layers,
} from 'lucide-react';

const ansi = new AnsiUp();

export interface DeploymentsTrafficTabProps {
  app: App;
}

export function DeploymentsTrafficTab({ app }: DeploymentsTrafficTabProps) {
  const queryClient = useQueryClient();
  const toast = useToast();

  // Selected Build for Stream Modal
  const [selectedBuildForStream, setSelectedBuildForStream] = useState<Build | null>(null);
  const [copiedSha, setCopiedSha] = useState<string | null>(null);

  // Query Builds
  const { data: builds, isLoading: isBuildsLoading } = useQuery({
    queryKey: ['builds', app.id],
    queryFn: () => api.builds.list(app.id),
    refetchInterval: 4000,
  });

  // In-place Redeploy mutation
  const deployMutation = useMutation({
    mutationFn: () => api.apps.deploy(app.id, { image: app.image }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['apps'] });
      queryClient.invalidateQueries({ queryKey: ['builds', app.id] });
      toast.success(
        'Redeployment Initiated',
        `In-place rolling deployment started for ${app.name}`
      );
    },
    onError: (err: Error) => toast.error('Deployment Failed', err.message),
  });

  // Rebuild mutation
  const rebuildMutation = useMutation({
    mutationFn: (buildId: string) => api.builds.rebuild(buildId),
    onSuccess: (newBuild) => {
      queryClient.invalidateQueries({ queryKey: ['builds', app.id] });
      toast.success('Rebuild Triggered', `Build ${newBuild.id.slice(0, 8)} queued`);
      setSelectedBuildForStream(newBuild);
    },
    onError: (err: Error) => toast.error('Rebuild Failed', err.message),
  });

  const copySha = (sha: string) => {
    navigator.clipboard.writeText(sha);
    setCopiedSha(sha);
    toast.info('Copied', 'Commit SHA copied');
    setTimeout(() => setCopiedSha(null), 2000);
  };

  const isAppRunning = app.status === 'running' || app.status === 'active';
  const isAppDeploying = app.status === 'deploying' || deployMutation.isPending;

  return (
    <div className="space-y-6 pt-2">
      {/* 1. Active Release & In-Place Rolling Deployment Control */}
      <Card className="p-5 bg-zinc-950/60 border-zinc-800 space-y-4">
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 border-b border-zinc-800 pb-3">
          <div className="flex items-center gap-2.5">
            <div className="p-2 rounded-lg bg-cyan-950/40 border border-cyan-800/30 text-cyan-400">
              <Layers className="h-4 w-4" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <span className="font-semibold text-sm text-zinc-100">
                  Active Deployment & Release
                </span>
                <Badge
                  variant={isAppRunning ? 'success' : isAppDeploying ? 'info' : 'error'}
                  dot
                >
                  {isAppDeploying ? 'Deploying' : isAppRunning ? 'Active' : app.status}
                </Badge>
              </div>
              <p className="text-xs text-zinc-400 mt-0.5">
                Deterministic in-place rolling update with health check probing & automated rollback.
              </p>
            </div>
          </div>

          <Button
            variant="primary"
            size="sm"
            onClick={() => deployMutation.mutate()}
            isLoading={deployMutation.isPending}
            disabled={isAppDeploying}
            leftIcon={<Zap className="h-3.5 w-3.5" />}
            className="self-start sm:self-auto shadow-sm"
          >
            ⚡ Redeploy
          </Button>
        </div>

        {/* Release Metadata Grid */}
        <div className="grid grid-cols-1 sm:grid-cols-4 gap-3 pt-1">
          <div className="p-3 bg-zinc-900/60 border border-zinc-800/80 rounded-lg">
            <span className="text-[10px] uppercase font-semibold text-zinc-500 block">Image Tag</span>
            <span className="text-xs font-mono text-cyan-300 font-medium truncate block mt-0.5" title={app.image}>
              {app.image || 'latest'}
            </span>
          </div>

          <div className="p-3 bg-zinc-900/60 border border-zinc-800/80 rounded-lg">
            <span className="text-[10px] uppercase font-semibold text-zinc-500 block">Container Port</span>
            <span className="text-xs font-mono text-zinc-200 font-medium block mt-0.5">
              :{app.container_port || 80}
            </span>
          </div>

          <div className="p-3 bg-zinc-900/60 border border-zinc-800/80 rounded-lg">
            <span className="text-[10px] uppercase font-semibold text-zinc-500 block">Replicas</span>
            <span className="text-xs font-mono text-zinc-200 font-medium block mt-0.5">
              {app.replicas || 1} instance{app.replicas !== 1 ? 's' : ''}
            </span>
          </div>

          <div className="p-3 bg-zinc-900/60 border border-zinc-800/80 rounded-lg">
            <span className="text-[10px] uppercase font-semibold text-zinc-500 block">Last Deployed</span>
            <span className="text-xs font-mono text-zinc-400 block mt-0.5">
              {app.updated_at ? formatDate(app.updated_at) : '—'}
            </span>
          </div>
        </div>
      </Card>

      {/* 2. Git Deployments & Build History */}
      <Card className="p-0 overflow-hidden border-zinc-800 bg-zinc-950/40">
        <div className="p-3.5 bg-zinc-900/90 border-b border-zinc-800 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Hammer className="h-4 w-4 text-cyan-400" />
            <span className="font-semibold text-sm text-zinc-100">
              Deployment History
            </span>
          </div>
          <span className="text-xs text-zinc-500 font-mono">
            {builds?.length || 0} Total Releases
          </span>
        </div>

        {isBuildsLoading ? (
          <div className="p-8 text-center text-zinc-500 text-xs">
            <Loader2 className="h-4 w-4 animate-spin mx-auto mb-2 text-cyan-400" />
            Loading deployment history...
          </div>
        ) : !builds || builds.length === 0 ? (
          <div className="p-8 text-center text-zinc-500 text-xs">
            No CI builds or Git deployments recorded yet.
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Commit / Image Tag</TableHead>
                <TableHead>Author</TableHead>
                <TableHead>Branch</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Duration</TableHead>
                <TableHead>Deployed At</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {builds.map((b) => (
                <TableRow key={b.id} className="hover:bg-zinc-900/50">
                  <TableCell className="font-mono text-xs">
                    <div className="flex items-center gap-1.5">
                      <GitCommit className="h-3.5 w-3.5 text-zinc-400" />
                      <span className="text-cyan-300 font-semibold">
                        {b.commit_sha ? b.commit_sha.slice(0, 7) : 'manual'}
                      </span>
                      {b.commit_sha && (
                        <button
                          type="button"
                          onClick={() => copySha(b.commit_sha)}
                          className="text-zinc-500 hover:text-zinc-300 p-0.5"
                        >
                          {copiedSha === b.commit_sha ? (
                            <Check className="h-3 w-3 text-emerald-400" />
                          ) : (
                            <Copy className="h-3 w-3" />
                          )}
                        </button>
                      )}
                    </div>
                    {b.commit_message && (
                      <p className="text-[10px] text-zinc-400 truncate max-w-xs mt-0.5">
                        {b.commit_message}
                      </p>
                    )}
                  </TableCell>

                  <TableCell className="text-xs text-zinc-300 font-mono">
                    {b.commit_author || b.author || 'system'}
                  </TableCell>

                  <TableCell className="text-xs font-mono text-zinc-400">
                    <span className="px-1.5 py-0.5 rounded bg-zinc-900 border border-zinc-800">
                      {b.branch || 'main'}
                    </span>
                  </TableCell>

                  <TableCell>
                    <Badge
                      variant={
                        b.status === 'success'
                          ? 'success'
                          : b.status === 'failed'
                          ? 'error'
                          : 'info'
                      }
                      dot
                    >
                      {b.status === 'success' ? 'Active' : b.status}
                    </Badge>
                  </TableCell>

                  <TableCell className="font-mono text-xs text-zinc-400">
                    {b.duration_ms ? formatDuration(b.duration_ms) : '—'}
                  </TableCell>

                  <TableCell className="font-mono text-xs text-zinc-400">
                    {b.created_at ? formatDate(b.created_at) : '—'}
                  </TableCell>

                  <TableCell className="text-right">
                    <div className="flex items-center justify-end gap-1.5">
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => setSelectedBuildForStream(b)}
                        leftIcon={<FileText className="h-3 w-3 text-cyan-400" />}
                      >
                        Logs
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => rebuildMutation.mutate(b.id)}
                        disabled={rebuildMutation.isPending}
                        title="Rebuild commit"
                      >
                        <RotateCw className="h-3 w-3 text-zinc-400 hover:text-zinc-200" />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Card>

      {/* Build Log Stream Modal */}
      {selectedBuildForStream && (
        <LiveBuildStreamModal
          build={selectedBuildForStream}
          onClose={() => setSelectedBuildForStream(null)}
        />
      )}
    </div>
  );
}

function LiveBuildStreamModal({
  build,
  onClose,
}: {
  build: Build;
  onClose: () => void;
}) {
  const [lines, setLines] = useState<string[]>([]);
  const parentRef = useRef<HTMLDivElement>(null);
  const autoScrollRef = useRef(true);

  const isTerminal = build.status === 'success' || build.status === 'failed' || build.status === 'cancelled';

  const { status } = useSSE<any>({
    endpoint: api.builds.streamUrl(build.id),
    enabled: !isTerminal,
    onMessage: (msg) => {
      let line = '';
      if (typeof msg === 'string') {
        line = msg;
      } else if (msg && typeof msg === 'object') {
        line = msg.log || msg.message || msg.text || JSON.stringify(msg);
      }
      if (line) {
        setLines((prev) => [...prev, line]);
      }
    },
  });

  useEffect(() => {
    if (isTerminal && build.logs) {
      setLines(build.logs.split('\n'));
    }
  }, [isTerminal, build.logs]);

  const rowVirtualizer = useVirtualizer({
    count: lines.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => 20,
    overscan: 10,
  });

  useEffect(() => {
    if (autoScrollRef.current && lines.length > 0 && parentRef.current) {
      rowVirtualizer.scrollToIndex(lines.length - 1, { align: 'end' });
    }
  }, [lines.length, rowVirtualizer]);

  return (
    <Modal
      isOpen={Boolean(build)}
      onClose={onClose}
      title={
        <div className="flex items-center gap-2">
          <Hammer className="h-5 w-5 text-cyan-400" />
          <span>Build Logs #{build.id.slice(0, 8)}</span>
          <Badge
            variant={
              build.status === 'success'
                ? 'success'
                : build.status === 'failed'
                ? 'error'
                : 'info'
            }
            dot
          >
            {build.status}
          </Badge>
        </div>
      }
      description={`Commit: ${build.commit_sha?.slice(0, 7) || 'HEAD'} • Branch: ${build.branch || 'main'}`}
      size="xl"
    >
      <div className="space-y-3">
        <div className="flex items-center justify-between text-xs text-zinc-400">
          <span className="font-mono">{lines.length} lines streamed</span>
          <span
            className={`flex items-center gap-1.5 ${
              status === 'connected'
                ? 'text-emerald-400'
                : status === 'connecting'
                ? 'text-amber-400'
                : 'text-zinc-500'
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
              ? 'Streaming Live'
              : status === 'connecting'
              ? 'Connecting...'
              : 'Complete'}
          </span>
        </div>

        <div
          ref={parentRef}
          className="h-96 w-full bg-zinc-950 rounded-lg border border-zinc-800 p-3 overflow-y-auto font-mono text-xs leading-relaxed select-text"
          onScroll={(e) => {
            const target = e.currentTarget;
            const isAtBottom = target.scrollHeight - target.scrollTop <= target.clientHeight + 40;
            autoScrollRef.current = isAtBottom;
          }}
        >
          {lines.length === 0 ? (
            <div className="h-full flex items-center justify-center text-zinc-600 font-mono text-xs">
              Waiting for build output stream...
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
                  className="truncate text-zinc-300"
                  dangerouslySetInnerHTML={{
                    __html: ansi.ansi_to_html(lines[virtualRow.index]),
                  }}
                />
              ))}
            </div>
          )}
        </div>
      </div>
    </Modal>
  );
}
