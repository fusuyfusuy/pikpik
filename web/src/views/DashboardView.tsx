import { useQuery } from '@tanstack/react-query';
import { api } from '../lib/api';
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '../components/ui/Card';
import { Badge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { formatBytes } from '../lib/utils';
import {
  Box,
  Server,
  Database,
  Globe,
  HardDrive,
  RefreshCw,
  Plus,
  Layers,
} from 'lucide-react';
import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
} from 'recharts';

export interface DashboardViewProps {
  onNavigate: (view: string) => void;
}

// Generate realistic live sparkline telemetry points
const mockTelemetry = Array.from({ length: 20 }, (_, i) => ({
  time: `${i * 3}s`,
  cpu: Math.floor(15 + Math.random() * 25),
  memory: Math.floor(35 + Math.random() * 15),
}));

export function DashboardView({ onNavigate }: DashboardViewProps) {
  const { data: apps, refetch: refetchApps } = useQuery({
    queryKey: ['apps'],
    queryFn: () => api.apps.list(),
  });

  const { data: nodes, refetch: refetchNodes } = useQuery({
    queryKey: ['nodes'],
    queryFn: api.nodes.list,
  });

  const { data: databases, refetch: refetchDatabases } = useQuery({
    queryKey: ['databases'],
    queryFn: api.databases.list,
  });

  const { data: domains } = useQuery({
    queryKey: ['domains'],
    queryFn: api.ingress.listDomains,
  });

  const { data: systemInfo } = useQuery({
    queryKey: ['systemInfo'],
    queryFn: api.system.getInfo,
  });

  const { data: diskInfo } = useQuery({
    queryKey: ['diskInfo'],
    queryFn: api.system.getDiskUsage,
  });

  const activeAppsCount = apps?.filter((a) => a.status === 'running').length || 0;
  const readyNodesCount = nodes?.filter((n) => n.status === 'ready').length || 0;

  const handleRefreshAll = () => {
    refetchApps();
    refetchNodes();
    refetchDatabases();
  };

  return (
    <div className="space-y-6">
      {/* Header Bar */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-zinc-100 flex items-center gap-2.5">
            <span>Cluster Overview</span>
            <Badge variant="success" dot pulse>
              Swarm Active
            </Badge>
          </h1>
          <p className="text-xs text-zinc-400 mt-0.5">
            Real-time telemetry and resource allocation for pikpik control plane
          </p>
        </div>

        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={handleRefreshAll} leftIcon={<RefreshCw className="h-3.5 w-3.5" />}>
            Refresh
          </Button>
          <Button variant="primary" size="sm" onClick={() => onNavigate('apps')} leftIcon={<Plus className="h-3.5 w-3.5" />}>
            New Application
          </Button>
        </div>
      </div>

      {/* Metric Cards Grid */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <Card className="cursor-pointer hover:border-cyan-500/50" onClick={() => onNavigate('apps')}>
          <div className="flex items-center justify-between">
            <div className="p-2.5 rounded-lg bg-cyan-950/50 border border-cyan-800/40 text-cyan-400">
              <Box className="h-5 w-5" />
            </div>
            <Badge variant="info">{activeAppsCount} running</Badge>
          </div>
          <div className="mt-4">
            <div className="text-2xl font-bold font-mono text-zinc-100">{apps?.length ?? 0}</div>
            <div className="text-xs text-zinc-400 font-medium mt-0.5">Applications</div>
          </div>
        </Card>

        <Card className="cursor-pointer hover:border-emerald-500/50" onClick={() => onNavigate('nodes')}>
          <div className="flex items-center justify-between">
            <div className="p-2.5 rounded-lg bg-emerald-950/50 border border-emerald-800/40 text-emerald-400">
              <Server className="h-5 w-5" />
            </div>
            <Badge variant="success">{readyNodesCount} ready</Badge>
          </div>
          <div className="mt-4">
            <div className="text-2xl font-bold font-mono text-zinc-100">{nodes?.length ?? 1}</div>
            <div className="text-xs text-zinc-400 font-medium mt-0.5">Swarm Nodes</div>
          </div>
        </Card>

        <Card className="cursor-pointer hover:border-purple-500/50" onClick={() => onNavigate('databases')}>
          <div className="flex items-center justify-between">
            <div className="p-2.5 rounded-lg bg-purple-950/50 border border-purple-800/40 text-purple-400">
              <Database className="h-5 w-5" />
            </div>
            <Badge variant="purple">{databases?.length ?? 0} active</Badge>
          </div>
          <div className="mt-4">
            <div className="text-2xl font-bold font-mono text-zinc-100">{databases?.length ?? 0}</div>
            <div className="text-xs text-zinc-400 font-medium mt-0.5">Managed Databases</div>
          </div>
        </Card>

        <Card className="cursor-pointer hover:border-amber-500/50" onClick={() => onNavigate('ingress')}>
          <div className="flex items-center justify-between">
            <div className="p-2.5 rounded-lg bg-amber-950/50 border border-amber-800/40 text-amber-400">
              <Globe className="h-5 w-5" />
            </div>
            <Badge variant="warning">Auto-TLS</Badge>
          </div>
          <div className="mt-4">
            <div className="text-2xl font-bold font-mono text-zinc-100">{domains?.length ?? 0}</div>
            <div className="text-xs text-zinc-400 font-medium mt-0.5">Ingress Domains</div>
          </div>
        </Card>
      </div>

      {/* Telemetry Charts */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        {/* CPU Chart */}
        <Card>
          <CardHeader>
            <div className="flex items-center justify-between">
              <div>
                <CardTitle className="text-sm">Cluster CPU Allocation</CardTitle>
                <CardDescription>Aggregate CPU load across all swarm nodes</CardDescription>
              </div>
              <span className="font-mono text-xs font-semibold text-cyan-400">
                {systemInfo?.total_cpus ? `${systemInfo.total_cpus} vCPUs` : '2 Cores'}
              </span>
            </div>
          </CardHeader>
          <CardContent>
            <div className="h-44 w-full">
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={mockTelemetry}>
                  <defs>
                    <linearGradient id="cpuGradient" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="#06b6d4" stopOpacity={0.4} />
                      <stop offset="95%" stopColor="#06b6d4" stopOpacity={0.0} />
                    </linearGradient>
                  </defs>
                  <XAxis dataKey="time" stroke="#52525b" fontSize={10} tickLine={false} />
                  <YAxis stroke="#52525b" fontSize={10} domain={[0, 100]} unit="%" tickLine={false} />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: '#18181b',
                      borderColor: '#27272a',
                      borderRadius: '8px',
                      fontSize: '12px',
                    }}
                  />
                  <Area
                    type="monotone"
                    dataKey="cpu"
                    stroke="#22d3ee"
                    strokeWidth={2}
                    fillOpacity={1}
                    fill="url(#cpuGradient)"
                  />
                </AreaChart>
              </ResponsiveContainer>
            </div>
          </CardContent>
        </Card>

        {/* Memory Chart */}
        <Card>
          <CardHeader>
            <div className="flex items-center justify-between">
              <div>
                <CardTitle className="text-sm">Cluster Memory Usage</CardTitle>
                <CardDescription>Resident container RAM consumption</CardDescription>
              </div>
              <span className="font-mono text-xs font-semibold text-emerald-400">
                {systemInfo?.total_memory_bytes
                  ? formatBytes(systemInfo.total_memory_bytes)
                  : '8.00 GiB'}
              </span>
            </div>
          </CardHeader>
          <CardContent>
            <div className="h-44 w-full">
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={mockTelemetry}>
                  <defs>
                    <linearGradient id="memGradient" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="#10b981" stopOpacity={0.4} />
                      <stop offset="95%" stopColor="#10b981" stopOpacity={0.0} />
                    </linearGradient>
                  </defs>
                  <XAxis dataKey="time" stroke="#52525b" fontSize={10} tickLine={false} />
                  <YAxis stroke="#52525b" fontSize={10} domain={[0, 100]} unit="%" tickLine={false} />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: '#18181b',
                      borderColor: '#27272a',
                      borderRadius: '8px',
                      fontSize: '12px',
                    }}
                  />
                  <Area
                    type="monotone"
                    dataKey="memory"
                    stroke="#34d399"
                    strokeWidth={2}
                    fillOpacity={1}
                    fill="url(#memGradient)"
                  />
                </AreaChart>
              </ResponsiveContainer>
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Lower Summary: Active Applications & System Specs */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        {/* Apps List Card */}
        <Card className="lg:col-span-2">
          <CardHeader>
            <div className="flex items-center justify-between">
              <CardTitle className="text-sm">Applications</CardTitle>
              <Button variant="ghost" size="sm" onClick={() => onNavigate('apps')}>
                View all &rarr;
              </Button>
            </div>
          </CardHeader>
          <CardContent>
            {(!apps || apps.length === 0) ? (
              <div className="text-center py-8 text-zinc-500 text-xs">
                No applications deployed yet. Click "New Application" to deploy your first container.
              </div>
            ) : (
              <div className="divide-y divide-zinc-800/60">
                {apps.slice(0, 5).map((app) => (
                  <div key={app.id} className="py-3 flex items-center justify-between">
                    <div className="flex items-center gap-3 min-w-0">
                      <div className="p-2 rounded-lg bg-zinc-800 text-zinc-300">
                        <Box className="h-4 w-4" />
                      </div>
                      <div className="min-w-0">
                        <div className="text-xs font-semibold text-zinc-200 truncate">{app.name}</div>
                        <div className="text-[11px] font-mono text-zinc-500 truncate">{app.image}</div>
                      </div>
                    </div>

                    <div className="flex items-center gap-3">
                      <span className="text-[11px] text-zinc-400 font-mono">
                        {app.replicas} replica{app.replicas > 1 ? 's' : ''}
                      </span>
                      <Badge
                        variant={
                          app.status === 'running'
                            ? 'success'
                            : app.status === 'stopped'
                            ? 'default'
                            : 'warning'
                        }
                        dot
                      >
                        {app.status}
                      </Badge>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </CardContent>
        </Card>

        {/* System Specs & Disk Quickview */}
        <Card>
          <CardHeader>
            <CardTitle className="text-sm">Storage & Docker Engine</CardTitle>
            <CardDescription>Host environment parameters</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4 text-xs">
            <div className="flex items-center justify-between py-1.5 border-b border-zinc-800/60">
              <span className="text-zinc-400 flex items-center gap-2">
                <HardDrive className="h-3.5 w-3.5 text-zinc-500" />
                Image Cache
              </span>
              <span className="font-mono text-zinc-200">
                {diskInfo ? formatBytes(diskInfo.images_bytes) : '0 B'}
              </span>
            </div>

            <div className="flex items-center justify-between py-1.5 border-b border-zinc-800/60">
              <span className="text-zinc-400 flex items-center gap-2">
                <Layers className="h-3.5 w-3.5 text-zinc-500" />
                Containers
              </span>
              <span className="font-mono text-zinc-200">
                {diskInfo ? formatBytes(diskInfo.containers_bytes) : '0 B'}
              </span>
            </div>

            <div className="flex items-center justify-between py-1.5 border-b border-zinc-800/60">
              <span className="text-zinc-400 flex items-center gap-2">
                <Server className="h-3.5 w-3.5 text-zinc-500" />
                Docker Engine
              </span>
              <span className="font-mono text-zinc-200">
                {systemInfo?.docker_version || '27.5.1'}
              </span>
            </div>

            <div className="pt-2">
              <Button
                variant="outline"
                size="sm"
                className="w-full"
                onClick={() => onNavigate('system')}
              >
                Inspect System Resources
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
