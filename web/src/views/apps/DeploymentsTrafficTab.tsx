import { useState, useEffect, useRef } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../../lib/api';
import { App, Build, UpstreamWeight, SetTrafficSplitRequest } from '../../lib/types';
import { Card } from '../../components/ui/Card';
import { Button } from '../../components/ui/Button';
import { Badge } from '../../components/ui/Badge';
import { Input } from '../../components/ui/Input';
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
  Sliders,
  Shuffle,
  Plus,
  Trash2,
  Globe,
  CheckCircle2,
  Server,
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

  const safeBuilds = builds || [];

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

      {/* 2. Canary Traffic Splitting & Upstream Load Balancing */}
      <CanaryTrafficControlCard app={app} />

      {/* 3. Git Deployments & Build History */}
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
              {safeBuilds.map((b) => (
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

function CanaryTrafficControlCard({ app }: { app: App }) {
  const queryClient = useQueryClient();
  const toast = useToast();

  const defaultPort = app.container_port || 80;
  const defaultStableUpstream = `${app.name}:${defaultPort}`;
  const defaultCanaryUpstream = `${app.name}-canary:${defaultPort}`;

  // Query Traffic Split
  const { data: trafficData, isLoading: isTrafficLoading } = useQuery({
    queryKey: ['app-traffic', app.id],
    queryFn: () => api.apps.getTraffic(app.id),
    refetchInterval: 5000,
  });

  const [splits, setSplits] = useState<UpstreamWeight[]>([
    { upstream: defaultStableUpstream, weight: 100 },
  ]);
  const [isDirty, setIsDirty] = useState(false);

  useEffect(() => {
    if (trafficData?.splits && !isDirty) {
      if (trafficData.splits.length === 0) {
        setSplits([{ upstream: defaultStableUpstream, weight: 100 }]);
      } else {
        setSplits(trafficData.splits);
      }
    } else if (!trafficData?.splits && !isDirty) {
      setSplits([{ upstream: defaultStableUpstream, weight: 100 }]);
    }
  }, [trafficData, isDirty, defaultStableUpstream]);

  // Mutations
  const trafficMutation = useMutation({
    mutationFn: (req: SetTrafficSplitRequest) => api.apps.setTraffic(app.id, req),
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ['app-traffic', app.id] });
      setIsDirty(false);
      const splitSummary = res.splits.map((s) => `${s.upstream}: ${s.weight}`).join(', ');
      toast.success(
        'Traffic Split Applied',
        `Dynamic ingress updated in <15ms (${splitSummary})`
      );
    },
    onError: (err: Error) => toast.error('Traffic Split Failed', err.message),
  });

  const resetTrafficMutation = useMutation({
    mutationFn: () => api.apps.setTraffic(app.id, { reset: true }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['app-traffic', app.id] });
      setIsDirty(false);
      setSplits([{ upstream: defaultStableUpstream, weight: 100 }]);
      toast.info('Traffic Reset', 'Ingress routed 100% to primary stable upstream');
    },
    onError: (err: Error) => toast.error('Reset Failed', err.message),
  });

  // Calculate weights & percentages
  const totalWeight = splits.reduce((acc, curr) => acc + (Math.max(0, curr.weight) || 0), 0);

  // Dual Upstream logic (Stable vs Canary)
  const isDual = splits.length === 2;
  const stableWeight = splits[0]?.weight ?? 100;
  const canaryWeight = isDual ? (splits[1]?.weight ?? 0) : 0;
  const dualTotal = stableWeight + canaryWeight;
  const canaryPct = dualTotal > 0 ? Math.round((canaryWeight / dualTotal) * 100) : 0;
  const stablePct = 100 - canaryPct;

  const handleSliderChange = (newCanaryVal: number) => {
    setIsDirty(true);
    const newStableVal = 100 - newCanaryVal;
    if (splits.length < 2) {
      setSplits([
        { upstream: splits[0]?.upstream || defaultStableUpstream, weight: newStableVal },
        { upstream: defaultCanaryUpstream, weight: newCanaryVal },
      ]);
    } else {
      setSplits((prev) => [
        { ...prev[0], weight: newStableVal },
        { ...prev[1], weight: newCanaryVal },
        ...prev.slice(2),
      ]);
    }
  };

  const handlePreset = (stableW: number, canaryW: number) => {
    setIsDirty(true);
    const u1 = splits[0]?.upstream || defaultStableUpstream;
    const u2 = splits[1]?.upstream || defaultCanaryUpstream;
    if (canaryW === 0 && splits.length <= 2) {
      setSplits([
        { upstream: u1, weight: 100 },
        { upstream: u2, weight: 0 },
      ]);
    } else {
      setSplits([
        { upstream: u1, weight: stableW },
        { upstream: u2, weight: canaryW },
      ]);
    }
  };

  const handleAddressChange = (index: number, newAddress: string) => {
    setIsDirty(true);
    setSplits((prev) => {
      const copy = [...prev];
      copy[index] = { ...copy[index], upstream: newAddress };
      return copy;
    });
  };

  const handleWeightChange = (index: number, newWeightStr: string) => {
    setIsDirty(true);
    const parsed = parseInt(newWeightStr, 10);
    const weight = isNaN(parsed) ? 0 : Math.max(0, parsed);
    setSplits((prev) => {
      const copy = [...prev];
      copy[index] = { ...copy[index], weight };
      return copy;
    });
  };

  const handleAddTarget = () => {
    setIsDirty(true);
    const nextIdx = splits.length;
    const suggested = nextIdx === 1
      ? defaultCanaryUpstream
      : `${app.name}-v${nextIdx + 1}:${defaultPort}`;
    setSplits((prev) => [...prev, { upstream: suggested, weight: 0 }]);
  };

  const handleRemoveTarget = (index: number) => {
    if (splits.length <= 1) return;
    setIsDirty(true);
    setSplits((prev) => prev.filter((_, i) => i !== index));
  };

  const handleApply = () => {
    const emptyAddr = splits.some((s) => !s.upstream.trim());
    if (emptyAddr) {
      toast.error('Validation Error', 'All upstream targets must have a valid host:port address');
      return;
    }
    if (totalWeight <= 0) {
      toast.error('Validation Error', 'Sum of upstream weights must be greater than 0');
      return;
    }
    trafficMutation.mutate({
      splits: splits.map((s) => ({ upstream: s.upstream.trim(), weight: Math.max(0, s.weight) })),
    });
  };

  const isPresetActive = (sW: number, cW: number) => {
    if (splits.length !== 2) return false;
    const t = (splits[0].weight || 0) + (splits[1].weight || 0);
    if (t === 0) return false;
    const actualC = Math.round(((splits[1].weight || 0) / t) * 100);
    const targetC = Math.round((cW / (sW + cW)) * 100);
    return actualC === targetC;
  };

  const isCanaryActive = splits.length === 2 && (splits[1].weight || 0) > 0;
  const isMultiCustom = splits.length > 2;

  const segmentColors = [
    'from-cyan-500 to-cyan-600',
    'from-purple-500 to-indigo-600',
    'from-amber-500 to-amber-600',
    'from-rose-500 to-rose-600',
    'from-emerald-500 to-emerald-600',
  ];

  return (
    <Card className="p-5 bg-zinc-950/60 border-zinc-800 space-y-5">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 border-b border-zinc-800 pb-3.5">
        <div className="flex items-center gap-2.5">
          <div className="p-2 rounded-lg bg-purple-950/40 border border-purple-800/30 text-purple-400">
            <Sliders className="h-4 w-4" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <span className="font-semibold text-sm text-zinc-100">
                Canary & Traffic Splitting
              </span>
              {isTrafficLoading ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin text-zinc-500" />
              ) : isCanaryActive ? (
                <Badge variant="purple" dot pulse>
                  Canary Active ({canaryPct}%)
                </Badge>
              ) : isMultiCustom ? (
                <Badge variant="info" dot>
                  Multi-Target Split ({splits.length})
                </Badge>
              ) : (
                <Badge variant="outline">
                  100% Stable
                </Badge>
              )}
            </div>
            <p className="text-xs text-zinc-400 mt-0.5">
              Dynamic weighted load balancing via Caddy Dynamic Admin API (Invariant 3) in &lt;15ms with zero downtime.
            </p>
          </div>
        </div>

        <div className="flex items-center gap-2 self-start sm:self-auto">
          <Button
            variant="outline"
            size="sm"
            onClick={() => resetTrafficMutation.mutate()}
            isLoading={resetTrafficMutation.isPending}
            disabled={resetTrafficMutation.isPending || (!isCanaryActive && !isMultiCustom && !isDirty)}
            leftIcon={<RotateCw className="h-3 w-3" />}
          >
            Reset to Stable
          </Button>
        </div>
      </div>

      {/* Visual Progress Bar */}
      <div className="space-y-2">
        <div className="flex items-center justify-between text-xs">
          <span className="font-medium text-zinc-300 flex items-center gap-1.5">
            <Shuffle className="h-3.5 w-3.5 text-cyan-400" />
            Live Ingress Distribution
          </span>
          <span className="text-[11px] font-mono text-zinc-400">
            {totalWeight > 0 ? `${totalWeight} total weight points` : '0 weight configured'}
          </span>
        </div>

        {/* Multi-segment visualizer bar */}
        <div className="h-3.5 w-full bg-zinc-900 rounded-full overflow-hidden flex border border-zinc-800 shadow-inner">
          {splits.map((s, idx) => {
            const pct = totalWeight > 0 ? ((Math.max(0, s.weight) || 0) / totalWeight) * 100 : 0;
            if (pct <= 0) return null;
            const gradient = segmentColors[idx % segmentColors.length];
            return (
              <div
                key={idx}
                style={{ width: `${pct}%` }}
                className={`h-full bg-gradient-to-r ${gradient} transition-all duration-300 flex items-center justify-center text-[9px] font-bold text-zinc-950 truncate px-1`}
                title={`${s.upstream || `Target #${idx + 1}`}: ${s.weight} (${pct.toFixed(1)}%)`}
              >
                {pct >= 12 ? `${pct.toFixed(0)}%` : ''}
              </div>
            );
          })}
        </div>

        {/* Legend */}
        <div className="flex flex-wrap items-center gap-3 pt-0.5 text-xs">
          {splits.map((s, idx) => {
            const pct = totalWeight > 0 ? (((Math.max(0, s.weight) || 0) / totalWeight) * 100).toFixed(1) : '0.0';
            const dotColor = idx === 0
              ? 'bg-cyan-400'
              : idx === 1
              ? 'bg-purple-400'
              : idx === 2
              ? 'bg-amber-400'
              : 'bg-rose-400';
            return (
              <div key={idx} className="flex items-center gap-1.5 text-zinc-400 font-mono text-[11px]">
                <span className={`h-2 w-2 rounded-full ${dotColor}`} />
                <span className="text-zinc-300 font-medium truncate max-w-[140px] sm:max-w-[200px]">
                  {s.upstream || `Target #${idx + 1}`}
                </span>
                <span className="text-zinc-500">({pct}%)</span>
              </div>
            );
          })}
        </div>
      </div>

      {/* Quick Presets */}
      <div className="space-y-2 pt-1">
        <span className="text-[11px] font-semibold uppercase text-zinc-500 tracking-wider block">
          Rollout & Canary Presets
        </span>
        <div className="grid grid-cols-2 sm:grid-cols-5 gap-2">
          <button
            type="button"
            onClick={() => handlePreset(100, 0)}
            className={`px-3 py-2 rounded-lg text-xs font-medium border transition-all text-left flex flex-col justify-between ${
              isPresetActive(100, 0)
                ? 'bg-cyan-950/40 border-cyan-500/60 text-cyan-200 shadow-sm shadow-cyan-500/10 ring-1 ring-cyan-500/30'
                : 'bg-zinc-900/60 border-zinc-800 text-zinc-300 hover:bg-zinc-900 hover:border-zinc-700'
            }`}
          >
            <span className="font-semibold text-zinc-100">100% Stable</span>
            <span className="text-[10px] text-zinc-400 font-mono mt-0.5">100 / 0</span>
          </button>

          <button
            type="button"
            onClick={() => handlePreset(90, 10)}
            className={`px-3 py-2 rounded-lg text-xs font-medium border transition-all text-left flex flex-col justify-between ${
              isPresetActive(90, 10)
                ? 'bg-purple-950/40 border-purple-500/60 text-purple-200 shadow-sm shadow-purple-500/10 ring-1 ring-purple-500/30'
                : 'bg-zinc-900/60 border-zinc-800 text-zinc-300 hover:bg-zinc-900 hover:border-zinc-700'
            }`}
          >
            <span className="font-semibold text-zinc-100">90 / 10 Canary</span>
            <span className="text-[10px] text-zinc-400 font-mono mt-0.5">10% Probation</span>
          </button>

          <button
            type="button"
            onClick={() => handlePreset(80, 20)}
            className={`px-3 py-2 rounded-lg text-xs font-medium border transition-all text-left flex flex-col justify-between ${
              isPresetActive(80, 20)
                ? 'bg-purple-950/40 border-purple-500/60 text-purple-200 shadow-sm shadow-purple-500/10 ring-1 ring-purple-500/30'
                : 'bg-zinc-900/60 border-zinc-800 text-zinc-300 hover:bg-zinc-900 hover:border-zinc-700'
            }`}
          >
            <span className="font-semibold text-zinc-100">80 / 20 Canary</span>
            <span className="text-[10px] text-zinc-400 font-mono mt-0.5">20% Soak Test</span>
          </button>

          <button
            type="button"
            onClick={() => handlePreset(50, 50)}
            className={`px-3 py-2 rounded-lg text-xs font-medium border transition-all text-left flex flex-col justify-between ${
              isPresetActive(50, 50)
                ? 'bg-indigo-950/40 border-indigo-500/60 text-indigo-200 shadow-sm shadow-indigo-500/10 ring-1 ring-indigo-500/30'
                : 'bg-zinc-900/60 border-zinc-800 text-zinc-300 hover:bg-zinc-900 hover:border-zinc-700'
            }`}
          >
            <span className="font-semibold text-zinc-100">50 / 50 Blue-Green</span>
            <span className="text-[10px] text-zinc-400 font-mono mt-0.5">Equal Load</span>
          </button>

          <button
            type="button"
            onClick={() => handlePreset(0, 100)}
            className={`px-3 py-2 rounded-lg text-xs font-medium border transition-all text-left flex flex-col justify-between ${
              isPresetActive(0, 100)
                ? 'bg-purple-950/40 border-purple-500/60 text-purple-200 shadow-sm shadow-purple-500/10 ring-1 ring-purple-500/30'
                : 'bg-zinc-900/60 border-zinc-800 text-zinc-300 hover:bg-zinc-900 hover:border-zinc-700'
            }`}
          >
            <span className="font-semibold text-zinc-100">100% Canary</span>
            <span className="text-[10px] text-zinc-400 font-mono mt-0.5">Full Cutover</span>
          </button>
        </div>
      </div>

      {/* Dual Weight Slider */}
      <div className="p-4 bg-zinc-900/40 border border-zinc-800/80 rounded-xl space-y-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <span className="text-xs font-semibold text-zinc-200">
              Interactive Canary Weight Slider
            </span>
            <span className="text-[11px] text-zinc-500">
              (Two-way synchronized)
            </span>
          </div>
          <div className="flex items-center gap-3 font-mono text-xs">
            <span className="text-cyan-400 font-medium">
              Stable: {stablePct}%
            </span>
            <span className="text-zinc-600">|</span>
            <span className="text-purple-400 font-medium">
              Canary: {canaryPct}%
            </span>
          </div>
        </div>

        <div className="space-y-1">
          <input
            type="range"
            min="0"
            max="100"
            step="1"
            value={canaryPct}
            onChange={(e) => handleSliderChange(parseInt(e.target.value, 10) || 0)}
            className="w-full h-2 bg-zinc-800 rounded-lg appearance-none cursor-pointer accent-purple-500"
          />
          <div className="flex justify-between text-[10px] font-mono text-zinc-500">
            <span>0% Canary (100% Stable)</span>
            <span>25%</span>
            <span>50% (Equal)</span>
            <span>75%</span>
            <span>100% Canary (Full Rollout)</span>
          </div>
        </div>
      </div>

      {/* Dynamic Upstream Targets Table */}
      <div className="space-y-2.5 pt-1">
        <div className="flex items-center justify-between">
          <span className="text-xs font-semibold text-zinc-200 flex items-center gap-1.5">
            <Server className="h-3.5 w-3.5 text-zinc-400" />
            Upstream Target Endpoints
          </span>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={handleAddTarget}
            leftIcon={<Plus className="h-3 w-3 text-cyan-400" />}
            className="text-xs"
          >
            Add Upstream Target
          </Button>
        </div>

        <div className="space-y-2">
          {splits.map((s, idx) => {
            const pct = totalWeight > 0 ? (((Math.max(0, s.weight) || 0) / totalWeight) * 100).toFixed(1) : '0.0';
            const label = idx === 0 ? 'Stable Primary' : idx === 1 ? 'Canary Target' : `Target #${idx + 1}`;
            return (
              <div
                key={idx}
                className="flex flex-col sm:flex-row items-stretch sm:items-center gap-2 p-2.5 bg-zinc-900/60 border border-zinc-800 rounded-lg"
              >
                <div className="flex items-center gap-2 min-w-[120px]">
                  <span
                    className={`h-2 w-2 rounded-full ${
                      idx === 0 ? 'bg-cyan-400' : idx === 1 ? 'bg-purple-400' : 'bg-amber-400'
                    }`}
                  />
                  <span className="text-xs font-semibold text-zinc-300 font-mono">
                    {label}
                  </span>
                </div>

                <div className="flex-1">
                  <Input
                    placeholder="host:port (e.g. app-name:8080 or 10.0.1.25:3000)"
                    value={s.upstream}
                    onChange={(e) => handleAddressChange(idx, e.target.value)}
                    className="font-mono text-xs py-1.5 h-8"
                  />
                </div>

                <div className="flex items-center gap-2 w-full sm:w-auto">
                  <div className="w-24">
                    <Input
                      type="number"
                      min="0"
                      max="10000"
                      placeholder="Weight"
                      value={s.weight}
                      onChange={(e) => handleWeightChange(idx, e.target.value)}
                      className="font-mono text-xs py-1.5 h-8 text-right"
                    />
                  </div>
                  <div className="w-16 text-right font-mono text-xs font-bold text-zinc-300">
                    {pct}%
                  </div>
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={() => handleRemoveTarget(idx)}
                    disabled={splits.length <= 1}
                    className="p-1 h-8 w-8 text-zinc-500 hover:text-rose-400 disabled:opacity-30"
                    title="Remove target"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </div>
              </div>
            );
          })}
        </div>
      </div>

      {/* Footer Actions & Ingress Details */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 pt-3 border-t border-zinc-800/80">
        <div className="flex flex-wrap items-center gap-2 text-xs text-zinc-400">
          <div className="flex items-center gap-1.5 font-mono text-[11px] bg-zinc-900 px-2 py-1 rounded border border-zinc-800">
            <Globe className="h-3 w-3 text-cyan-400" />
            <span>{trafficData?.domain || app.domains?.[0] || 'Default Route'}</span>
          </div>
          <Badge variant="outline" className="text-[10px]">
            &lt;15ms Dynamic Route Reconciler
          </Badge>
          {isDirty && (
            <span className="text-amber-400 text-xs font-medium flex items-center gap-1">
              ● Unsaved Split Changes
            </span>
          )}
        </div>

        <div className="flex items-center gap-2 self-end sm:self-auto">
          {isDirty && (
            <Button
              variant="ghost"
              size="sm"
              onClick={() => {
                if (trafficData?.splits) {
                  setSplits(trafficData.splits);
                } else {
                  setSplits([{ upstream: defaultStableUpstream, weight: 100 }]);
                }
                setIsDirty(false);
              }}
              disabled={trafficMutation.isPending}
            >
              Discard
            </Button>
          )}
          <Button
            variant="primary"
            size="sm"
            onClick={handleApply}
            isLoading={trafficMutation.isPending}
            leftIcon={<CheckCircle2 className="h-3.5 w-3.5" />}
          >
            Apply Traffic Split
          </Button>
        </div>
      </div>
    </Card>
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
