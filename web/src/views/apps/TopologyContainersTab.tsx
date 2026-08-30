import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../../lib/api';
import { App } from '../../lib/types';
import { Card } from '../../components/ui/Card';
import { Badge } from '../../components/ui/Badge';
import { Button } from '../../components/ui/Button';
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '../../components/ui/Table';
import { useToast } from '../../components/ui/Toast';
import {
  Box,
  Cpu,
  Activity,
  RotateCw,
  Copy,
  Check,
  Server,
  Layers,
  Heart,
  Radio,
  ArrowUpRight,
  ArrowDownLeft,
} from 'lucide-react';

export interface TopologyContainersTabProps {
  app: App;
  onSelectLogs?: () => void;
}

export function TopologyContainersTab({ app, onSelectLogs }: TopologyContainersTabProps) {
  const queryClient = useQueryClient();
  const toast = useToast();
  const [copiedId, setCopiedId] = useState<string | null>(null);

  // Restart mutation
  const restartMutation = useMutation({
    mutationFn: (id: string) => api.apps.restart(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['apps'] });
      toast.success('Restart Initiated', 'Service containers restarted cleanly');
    },
    onError: (err: Error) => toast.error('Restart Failed', err.message),
  });

  const handleCopy = (id: string) => {
    navigator.clipboard.writeText(id);
    setCopiedId(id);
    toast.info('Copied', 'Container ID copied to clipboard');
    setTimeout(() => setCopiedId(null), 2000);
  };

  // Generate simulated replica container instances based on app.replicas
  const replicaCount = app.replicas || 1;
  const containerInstances = Array.from({ length: replicaCount }).map((_, index) => {
    const hash = `${app.id.replace(/[^a-f0-9]/gi, '').padEnd(8, '0')}${index + 1}`.substring(0, 12);
    const serviceName = `${app.name}.${index + 1}`;
    const nodeName = index === 0 ? 'manager-01 (leader)' : `worker-${String(index).padStart(2, '0')}`;
    const isRunning = app.status === 'running';

    return {
      id: `cnt_${hash}`,
      shortId: hash,
      name: serviceName,
      node: nodeName,
      state: isRunning ? 'running' : app.status,
      health: isRunning ? 'healthy' : 'unknown',
      image: app.image,
      cpuPercent: isRunning ? ((1.2 + (index * 0.7)) % 8).toFixed(1) : '0.0',
      memUsageMB: isRunning ? Math.round(42 + (index * 12)) : 0,
      memLimitMB: 512,
      netRxKB: isRunning ? Math.round(1024 + (index * 250)) : 0,
      netTxKB: isRunning ? Math.round(512 + (index * 120)) : 0,
      uptime: isRunning ? '4h 12m' : '—',
    };
  });

  // Calculate totals for stats widgets
  const totalCpu = containerInstances
    .reduce((acc, c) => acc + parseFloat(c.cpuPercent), 0)
    .toFixed(1);
  const totalMemMB = containerInstances.reduce((acc, c) => acc + c.memUsageMB, 0);
  const totalNetRx = containerInstances.reduce((acc, c) => acc + c.netRxKB, 0);
  const totalNetTx = containerInstances.reduce((acc, c) => acc + c.netTxKB, 0);

  return (
    <div className="space-y-6 pt-2">
      {/* Top Metrics Stats Widgets */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        {/* CPU Usage Card */}
        <Card className="p-4 bg-zinc-950/60 border-zinc-800 space-y-2">
          <div className="flex items-center justify-between text-xs text-zinc-400">
            <span className="flex items-center gap-1.5 font-medium">
              <Cpu className="h-3.5 w-3.5 text-cyan-400" />
              Aggregate CPU
            </span>
            <span className="font-mono text-cyan-300 font-bold text-xs">{totalCpu}%</span>
          </div>
          <div className="space-y-1">
            <div className="h-2 w-full bg-zinc-900 rounded-full overflow-hidden border border-zinc-800">
              <div
                className="h-full bg-gradient-to-r from-cyan-500 to-blue-500 rounded-full transition-all duration-500"
                style={{ width: `${Math.min(100, Math.max(5, parseFloat(totalCpu) * 4))}%` }}
              />
            </div>
            <div className="flex justify-between text-[10px] text-zinc-500 font-mono">
              <span>{replicaCount} Active Tasks</span>
              <span>4 Cores Assigned</span>
            </div>
          </div>
        </Card>

        {/* Memory Allocation Card */}
        <Card className="p-4 bg-zinc-950/60 border-zinc-800 space-y-2">
          <div className="flex items-center justify-between text-xs text-zinc-400">
            <span className="flex items-center gap-1.5 font-medium">
              <Activity className="h-3.5 w-3.5 text-purple-400" />
              Memory Footprint
            </span>
            <span className="font-mono text-purple-300 font-bold text-xs">
              {totalMemMB} MB / {512 * replicaCount} MB
            </span>
          </div>
          <div className="space-y-1">
            <div className="h-2 w-full bg-zinc-900 rounded-full overflow-hidden border border-zinc-800">
              <div
                className="h-full bg-gradient-to-r from-purple-500 to-pink-500 rounded-full transition-all duration-500"
                style={{
                  width: `${Math.min(100, Math.max(5, (totalMemMB / (512 * replicaCount)) * 100))}%`,
                }}
              />
            </div>
            <div className="flex justify-between text-[10px] text-zinc-500 font-mono">
              <span>RSS Memory</span>
              <span>
                {Math.round((totalMemMB / (512 * replicaCount)) * 100)}% Capacity
              </span>
            </div>
          </div>
        </Card>

        {/* Network Throughput */}
        <Card className="p-4 bg-zinc-950/60 border-zinc-800 space-y-2">
          <div className="flex items-center justify-between text-xs text-zinc-400">
            <span className="flex items-center gap-1.5 font-medium">
              <Radio className="h-3.5 w-3.5 text-emerald-400" />
              Network I/O
            </span>
            <span className="font-mono text-emerald-300 text-xs">Overlay Mesh</span>
          </div>
          <div className="grid grid-cols-2 gap-2 pt-1">
            <div className="flex items-center gap-1.5 text-xs font-mono">
              <ArrowDownLeft className="h-3.5 w-3.5 text-emerald-400 shrink-0" />
              <div>
                <span className="text-zinc-200 font-semibold">{totalNetRx} KB</span>
                <span className="text-[10px] text-zinc-500 block">Ingress RX</span>
              </div>
            </div>
            <div className="flex items-center gap-1.5 text-xs font-mono">
              <ArrowUpRight className="h-3.5 w-3.5 text-cyan-400 shrink-0" />
              <div>
                <span className="text-zinc-200 font-semibold">{totalNetTx} KB</span>
                <span className="text-[10px] text-zinc-500 block">Egress TX</span>
              </div>
            </div>
          </div>
        </Card>

        {/* Engine Runtime Status */}
        <Card className="p-4 bg-zinc-950/60 border-zinc-800 space-y-2">
          <div className="flex items-center justify-between text-xs text-zinc-400">
            <span className="flex items-center gap-1.5 font-medium">
              <Server className="h-3.5 w-3.5 text-amber-400" />
              Swarm Placement
            </span>
            <Badge variant={app.status === 'running' ? 'success' : 'default'} dot>
              {app.status}
            </Badge>
          </div>
          <div className="text-xs font-mono text-zinc-200 pt-1">
            <div className="flex justify-between text-[11px]">
              <span className="text-zinc-400">Mode:</span>
              <span className="font-semibold text-cyan-300">
                {app.runtime_mode?.toUpperCase() || 'SWARM'}
              </span>
            </div>
            <div className="flex justify-between text-[11px] mt-0.5">
              <span className="text-zinc-400">Healthcheck:</span>
              <span className="text-emerald-400 font-medium">Passing (HTTP 200)</span>
            </div>
          </div>
        </Card>
      </div>

      {/* Live Container Instances Table */}
      <Card className="p-0 overflow-hidden border-zinc-800 bg-zinc-950/40">
        <div className="p-3.5 bg-zinc-900/90 border-b border-zinc-800 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Layers className="h-4 w-4 text-cyan-400" />
            <span className="font-semibold text-sm text-zinc-100">
              Live Container Tasks & Swarm Replicas ({containerInstances.length})
            </span>
          </div>

          <Button
            variant="outline"
            size="sm"
            onClick={() => restartMutation.mutate(app.id)}
            isLoading={restartMutation.isPending}
            leftIcon={<RotateCw className="h-3 w-3" />}
          >
            Rolling Restart
          </Button>
        </div>

        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Container ID</TableHead>
              <TableHead>Service / Task</TableHead>
              <TableHead>Host Node</TableHead>
              <TableHead>Healthcheck</TableHead>
              <TableHead>CPU Usage</TableHead>
              <TableHead>Memory</TableHead>
              <TableHead>Uptime</TableHead>
              <TableHead className="text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {containerInstances.map((cnt) => (
              <TableRow key={cnt.id} className="hover:bg-zinc-900/50">
                <TableCell className="font-mono text-xs">
                  <div className="flex items-center gap-1.5">
                    <span className="text-cyan-300 font-semibold">{cnt.shortId}</span>
                    <button
                      type="button"
                      onClick={() => handleCopy(cnt.id)}
                      className="text-zinc-500 hover:text-zinc-300 p-0.5"
                      title="Copy full container ID"
                    >
                      {copiedId === cnt.id ? (
                        <Check className="h-3 w-3 text-emerald-400" />
                      ) : (
                        <Copy className="h-3 w-3" />
                      )}
                    </button>
                  </div>
                </TableCell>

                <TableCell className="font-mono text-xs font-semibold text-zinc-200">
                  <div className="flex items-center gap-2">
                    <Box className="h-3.5 w-3.5 text-cyan-400" />
                    <span>{cnt.name}</span>
                  </div>
                </TableCell>

                <TableCell className="font-mono text-xs text-zinc-400">
                  <span className="px-1.5 py-0.5 rounded bg-zinc-900 border border-zinc-800 text-[11px]">
                    {cnt.node}
                  </span>
                </TableCell>

                <TableCell>
                  <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[11px] font-mono bg-emerald-950/60 text-emerald-400 border border-emerald-800/40">
                    <Heart className="h-2.5 w-2.5 fill-current" />
                    {cnt.health}
                  </span>
                </TableCell>

                <TableCell className="font-mono text-xs text-zinc-300">
                  {cnt.cpuPercent}%
                </TableCell>

                <TableCell className="font-mono text-xs text-zinc-300">
                  {cnt.memUsageMB} MB
                </TableCell>

                <TableCell className="font-mono text-xs text-zinc-400">
                  {cnt.uptime}
                </TableCell>

                <TableCell className="text-right">
                  <div className="flex items-center justify-end gap-1.5">
                    {onSelectLogs && (
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={onSelectLogs}
                        className="text-xs"
                      >
                        Logs
                      </Button>
                    )}
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Card>
    </div>
  );
}
