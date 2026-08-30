import { useState, useEffect } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../lib/api';
import {
  BindDomainRequest,
  CertificateUploadRequest,
  TrafficSplitConfig,
  BlueGreenDeployRequest,
  BlueGreenDeployResponse,
} from '../lib/types';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Badge } from '../components/ui/Badge';
import { Input, Textarea } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from '../components/ui/Table';
import { useToast } from '../components/ui/Toast';
import { formatDate } from '../lib/utils';
import {
  Globe,
  Plus,
  Shield,
  ShieldCheck,
  RotateCw,
  Trash2,
  Lock,
  Sliders,
  CheckCircle2,
  ArrowRightLeft,
  Sparkles,
  Zap,
} from 'lucide-react';

export function IngressView() {
  const queryClient = useQueryClient();
  const toast = useToast();

  const [activeTab, setActiveTab] = useState<'domains' | 'traffic' | 'bluegreen'>('domains');

  // Domain modal states
  const [isBindModalOpen, setIsBindModalOpen] = useState(false);
  const [isCertModalOpen, setIsCertModalOpen] = useState(false);

  // Form states
  const [domain, setDomain] = useState('');
  const [appId, setAppId] = useState('');
  const [autoTLS, setAutoTLS] = useState(true);

  // Cert states
  const [certDomain, setCertDomain] = useState('');
  const [certPem, setCertPem] = useState('');
  const [keyPem, setKeyPem] = useState('');

  // Traffic Splitting states
  const [selectedAppId, setSelectedAppId] = useState<string>('');
  const [trafficDomain, setTrafficDomain] = useState<string>('');
  const [stableUpstream, setStableUpstream] = useState<string>('');
  const [canaryUpstream, setCanaryUpstream] = useState<string>('');
  const [canaryPercent, setCanaryPercent] = useState<number>(0);
  const [headerKey, setHeaderKey] = useState<string>('X-Canary');
  const [headerValue, setHeaderValue] = useState<string>('true');
  const [pathMatch, setPathMatch] = useState<string>('');

  // Blue-Green Deploy states
  const [bgAppId, setBgAppId] = useState<string>('');
  const [bgImage, setBgImage] = useState<string>('');
  const [bgDomain, setBgDomain] = useState<string>('');
  const [bgPort, setBgPort] = useState<number>(8080);
  const [bgHealthPath, setBgHealthPath] = useState<string>('/healthz');
  const [bgProbeTimeout, setBgProbeTimeout] = useState<number>(10);
  const [bgDrainPeriod, setBgDrainPeriod] = useState<number>(5);
  const [bgResult, setBgResult] = useState<BlueGreenDeployResponse | null>(null);

  // Queries
  const { data: domains, isLoading } = useQuery({
    queryKey: ['domains'],
    queryFn: api.ingress.listDomains,
  });

  const { data: apps } = useQuery({
    queryKey: ['apps'],
    queryFn: api.apps.list,
  });

  // Query traffic config when selectedAppId changes
  const { data: currentTraffic } = useQuery({
    queryKey: ['traffic', selectedAppId],
    queryFn: () => (selectedAppId ? api.traffic.get(selectedAppId) : null),
    enabled: Boolean(selectedAppId),
  });

  useEffect(() => {
    if (apps && apps.length > 0 && !selectedAppId) {
      setSelectedAppId(apps[0].id);
      setBgAppId(apps[0].id);
    }
  }, [apps, selectedAppId]);

  useEffect(() => {
    if (currentTraffic) {
      setTrafficDomain(currentTraffic.domain || '');
      setStableUpstream(currentTraffic.stable_upstream || '');
      setCanaryUpstream(currentTraffic.canary_upstream || '');
      setCanaryPercent(currentTraffic.canary_percent || 0);
      if (currentTraffic.headers && Object.keys(currentTraffic.headers).length > 0) {
        const firstKey = Object.keys(currentTraffic.headers)[0];
        setHeaderKey(firstKey);
        setHeaderValue(currentTraffic.headers[firstKey]);
      }
      if (currentTraffic.paths && currentTraffic.paths.length > 0) {
        setPathMatch(currentTraffic.paths.join(', '));
      }
    } else if (selectedAppId) {
      const app = apps?.find((a) => a.id === selectedAppId);
      if (app) {
        setTrafficDomain(app.domains?.[0] || `${app.name}.example.com`);
        setStableUpstream(`${app.name}_blue:8080`);
        setCanaryUpstream(`${app.name}_green:8080`);
        setCanaryPercent(0);
      }
    }
  }, [currentTraffic, selectedAppId, apps]);

  // Mutations
  const bindMutation = useMutation({
    mutationFn: (req: BindDomainRequest) => api.ingress.bindDomain(req),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['domains'] });
      toast.success('Domain Bound', `Domain ${domain} bound to application`);
      setIsBindModalOpen(false);
      setDomain('');
    },
    onError: (err: Error) => toast.error('Binding Failed', err.message),
  });

  const uploadCertMutation = useMutation({
    mutationFn: (req: CertificateUploadRequest) => api.ingress.uploadCertificate(req),
    onSuccess: () => {
      toast.success('Certificate Uploaded', 'Custom TLS certificate installed in Caddy');
      setIsCertModalOpen(false);
      setCertPem('');
      setKeyPem('');
    },
    onError: (err: Error) => toast.error('Upload Failed', err.message),
  });

  const deleteDomainMutation = useMutation({
    mutationFn: (id: string) => api.ingress.deleteDomain(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['domains'] });
      toast.success('Domain Removed', 'Domain binding removed from ingress router');
    },
    onError: (err: Error) => toast.error('Delete Failed', err.message),
  });

  const reconcileMutation = useMutation({
    mutationFn: api.ingress.reconcile,
    onSuccess: () => {
      toast.success('Ingress Reconciled', 'Caddy dynamic routes synchronization complete');
    },
    onError: (err: Error) => toast.error('Reconcile Failed', err.message),
  });

  const setTrafficMutation = useMutation({
    mutationFn: (req: Partial<TrafficSplitConfig>) => api.traffic.set(selectedAppId, req),
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ['traffic', selectedAppId] });
      toast.success(
        'Traffic Split Applied',
        `Routed ${100 - res.canary_percent}% Stable / ${res.canary_percent}% Canary traffic`
      );
    },
    onError: (err: Error) => toast.error('Failed to set traffic split', err.message),
  });

  const blueGreenMutation = useMutation({
    mutationFn: (req: BlueGreenDeployRequest) => api.traffic.deployBlueGreen(bgAppId, req),
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ['apps'] });
      queryClient.invalidateQueries({ queryKey: ['traffic', bgAppId] });
      setBgResult(res);
      toast.success(
        'Blue-Green Cutover Completed',
        `Swapped traffic to green container ${res.green_container_id.slice(0, 12)} in ${res.duration_ms}ms`
      );
    },
    onError: (err: Error) => toast.error('Blue-Green Deploy Failed', err.message),
  });

  const handleApplyTrafficSplit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedAppId) return;

    const headersMap: Record<string, string> = {};
    if (headerKey.trim() && headerValue.trim()) {
      headersMap[headerKey.trim()] = headerValue.trim();
    }

    const pathsList = pathMatch
      ? pathMatch
          .split(',')
          .map((p) => p.trim())
          .filter(Boolean)
      : [];

    setTrafficMutation.mutate({
      domain: trafficDomain.trim(),
      stable_upstream: stableUpstream.trim(),
      canary_upstream: canaryUpstream.trim(),
      canary_percent: Number(canaryPercent),
      headers: Object.keys(headersMap).length > 0 ? headersMap : undefined,
      paths: pathsList.length > 0 ? pathsList : undefined,
    });
  };

  const handleBlueGreenSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!bgAppId || !bgImage.trim()) return;

    blueGreenMutation.mutate({
      image: bgImage.trim(),
      domain: bgDomain.trim() || undefined,
      container_port: Number(bgPort),
      health_check_path: bgHealthPath.trim(),
      probe_timeout_sec: Number(bgProbeTimeout),
      drain_period_sec: Number(bgDrainPeriod),
    });
  };

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-zinc-100 flex items-center gap-2">
            <Globe className="h-5 w-5 text-cyan-400" />
            <span>Ingress, Canary & Blue-Green Traffic</span>
          </h1>
          <p className="text-xs text-zinc-400 mt-0.5">
            Caddy dynamic reverse-proxy with automated TLS, fine-grained Canary weight sliders, and zero-downtime Blue-Green cutovers
          </p>
        </div>

        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => reconcileMutation.mutate()}
            isLoading={reconcileMutation.isPending}
            leftIcon={<RotateCw className="h-3.5 w-3.5" />}
          >
            Reconcile Caddy
          </Button>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => setIsCertModalOpen(true)}
            leftIcon={<Shield className="h-3.5 w-3.5" />}
          >
            Upload Cert
          </Button>
          <Button
            variant="primary"
            size="sm"
            onClick={() => setIsBindModalOpen(true)}
            leftIcon={<Plus className="h-4 w-4" />}
          >
            Bind Domain
          </Button>
        </div>
      </div>

      {/* Tabs */}
      <div className="flex items-center gap-1 border-b border-zinc-800 pb-2">
        <button
          onClick={() => setActiveTab('domains')}
          className={`flex items-center gap-2 px-3.5 py-1.5 rounded-lg text-xs font-medium transition-all ${
            activeTab === 'domains'
              ? 'bg-zinc-800 text-cyan-300 font-semibold'
              : 'text-zinc-400 hover:text-zinc-200'
          }`}
        >
          <Globe className="h-3.5 w-3.5" />
          <span>Domain Bindings & TLS</span>
          <span className="text-[10px] bg-zinc-900 px-1.5 py-0.5 rounded text-zinc-400">
            {domains?.length || 0}
          </span>
        </button>

        <button
          onClick={() => setActiveTab('traffic')}
          className={`flex items-center gap-2 px-3.5 py-1.5 rounded-lg text-xs font-medium transition-all ${
            activeTab === 'traffic'
              ? 'bg-zinc-800 text-cyan-300 font-semibold'
              : 'text-zinc-400 hover:text-zinc-200'
          }`}
        >
          <Sliders className="h-3.5 w-3.5" />
          <span>Canary Traffic Splitting</span>
        </button>

        <button
          onClick={() => setActiveTab('bluegreen')}
          className={`flex items-center gap-2 px-3.5 py-1.5 rounded-lg text-xs font-medium transition-all ${
            activeTab === 'bluegreen'
              ? 'bg-zinc-800 text-cyan-300 font-semibold'
              : 'text-zinc-400 hover:text-zinc-200'
          }`}
        >
          <ArrowRightLeft className="h-3.5 w-3.5" />
          <span>Blue-Green Zero-Downtime Rollout</span>
        </button>
      </div>

      {/* TAB 1: Domains */}
      {activeTab === 'domains' && (
        <Card className="p-0 overflow-hidden">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Domain Name</TableHead>
                <TableHead>Target App / Service</TableHead>
                <TableHead>TLS Provider</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Registered</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading ? (
                <TableRow>
                  <TableCell colSpan={6} className="text-center py-8 text-zinc-500">
                    Loading domain routes...
                  </TableCell>
                </TableRow>
              ) : !domains || domains.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className="text-center py-10 text-zinc-500">
                    No ingress domains mapped yet. Click "Bind Domain" to route HTTP traffic.
                  </TableCell>
                </TableRow>
              ) : (
                domains.map((binding) => {
                  const targetApp = apps?.find((a) => a.id === binding.app_id);
                  return (
                    <TableRow key={binding.id}>
                      <TableCell className="font-semibold text-zinc-100">
                        <div className="flex items-center gap-2">
                          <Globe className="h-4 w-4 text-cyan-400 shrink-0" />
                          <span className="font-mono">{binding.domain}</span>
                        </div>
                      </TableCell>
                      <TableCell className="font-mono text-xs text-zinc-300">
                        {targetApp ? targetApp.name : binding.app_id}
                      </TableCell>
                      <TableCell>
                        {binding.auto_tls ? (
                          <span className="inline-flex items-center gap-1 text-xs text-emerald-400 font-medium">
                            <ShieldCheck className="h-3.5 w-3.5" />
                            Auto Let's Encrypt
                          </span>
                        ) : (
                          <span className="inline-flex items-center gap-1 text-xs text-zinc-400 font-medium">
                            <Lock className="h-3.5 w-3.5" />
                            Custom TLS
                          </span>
                        )}
                      </TableCell>
                      <TableCell>
                        <Badge variant={binding.status === 'active' ? 'success' : 'warning'} dot>
                          {binding.status}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-xs text-zinc-500">
                        {formatDate(binding.created_at)}
                      </TableCell>
                      <TableCell className="text-right">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => {
                            if (confirm(`Remove domain ${binding.domain}?`)) {
                              deleteDomainMutation.mutate(binding.id);
                            }
                          }}
                        >
                          <Trash2 className="h-3.5 w-3.5 text-rose-400" />
                        </Button>
                      </TableCell>
                    </TableRow>
                  );
                })
              )}
            </TableBody>
          </Table>
        </Card>
      )}

      {/* TAB 2: Traffic Splitting */}
      {activeTab === 'traffic' && (
        <div className="space-y-6">
          <form onSubmit={handleApplyTrafficSplit} className="space-y-6">
            <Card className="space-y-6">
              {/* App selector */}
              <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 pb-4 border-b border-zinc-800">
                <div>
                  <h3 className="text-sm font-bold text-zinc-100 flex items-center gap-2">
                    <Sliders className="h-4 w-4 text-cyan-400" />
                    <span>Canary Ingress Traffic Weighting</span>
                  </h3>
                  <p className="text-xs text-zinc-400 mt-0.5">
                    Split incoming HTTP/HTTPS requests between Stable (Blue) and Canary (Green) upstreams
                  </p>
                </div>

                <div className="w-full sm:w-64">
                  <select
                    value={selectedAppId}
                    onChange={(e) => setSelectedAppId(e.target.value)}
                    className="w-full rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-1.5 text-xs text-zinc-100 font-mono focus:border-cyan-500 focus:outline-none"
                  >
                    {apps?.map((app) => (
                      <option key={app.id} value={app.id}>
                        {app.name} ({app.id})
                      </option>
                    ))}
                  </select>
                </div>
              </div>

              {/* Visual Traffic Distribution Bar */}
              <div className="space-y-2">
                <div className="flex justify-between items-center text-xs font-mono font-semibold">
                  <span className="text-cyan-400">
                    Stable Upstream: {100 - canaryPercent}%
                  </span>
                  <span className="text-emerald-400">
                    Canary Upstream: {canaryPercent}%
                  </span>
                </div>

                <div className="h-4 w-full bg-zinc-900 rounded-full overflow-hidden flex border border-zinc-800 p-0.5">
                  <div
                    style={{ width: `${100 - canaryPercent}%` }}
                    className="h-full bg-gradient-to-r from-cyan-600 to-cyan-400 rounded-l-full transition-all duration-300"
                  />
                  <div
                    style={{ width: `${canaryPercent}%` }}
                    className="h-full bg-gradient-to-r from-emerald-500 to-emerald-400 rounded-r-full transition-all duration-300"
                  />
                </div>
              </div>

              {/* Interactive Slider */}
              <div className="space-y-3 p-4 bg-zinc-950/80 rounded-xl border border-zinc-800/80">
                <div className="flex items-center justify-between">
                  <label className="text-xs font-semibold text-zinc-200">
                    Canary Traffic Percentage (0% to 100%)
                  </label>
                  <span className="px-2 py-0.5 rounded bg-zinc-900 border border-zinc-800 font-mono text-xs font-bold text-cyan-300">
                    {canaryPercent}% Canary
                  </span>
                </div>

                <input
                  type="range"
                  min="0"
                  max="100"
                  step="1"
                  value={canaryPercent}
                  onChange={(e) => setCanaryPercent(Number(e.target.value))}
                  className="w-full h-2 bg-zinc-800 rounded-lg appearance-none cursor-pointer accent-cyan-400"
                />

                {/* Preset Pills */}
                <div className="flex flex-wrap gap-2 pt-1">
                  {[
                    { label: '0% (100% Stable)', val: 0 },
                    { label: '10% Canary', val: 10 },
                    { label: '25% Canary', val: 25 },
                    { label: '50% Split', val: 50 },
                    { label: '75% Canary', val: 75 },
                    { label: '100% Canary', val: 100 },
                  ].map((preset) => (
                    <button
                      type="button"
                      key={preset.val}
                      onClick={() => setCanaryPercent(preset.val)}
                      className={`px-2.5 py-1 rounded text-[11px] font-mono transition-all ${
                        canaryPercent === preset.val
                          ? 'bg-cyan-500/20 text-cyan-300 border border-cyan-500/50 font-bold'
                          : 'bg-zinc-900 text-zinc-400 hover:text-zinc-200 border border-zinc-800'
                      }`}
                    >
                      {preset.label}
                    </button>
                  ))}
                </div>
              </div>

              {/* Upstream & Domain Config */}
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                <Input
                  label="Target Domain Name"
                  placeholder="app.example.com"
                  value={trafficDomain}
                  onChange={(e) => setTrafficDomain(e.target.value)}
                  required
                />
                <Input
                  label="Stable Upstream (Blue)"
                  placeholder="app_blue:8080"
                  value={stableUpstream}
                  onChange={(e) => setStableUpstream(e.target.value)}
                  required
                />
                <Input
                  label="Canary Upstream (Green)"
                  placeholder="app_green:8080"
                  value={canaryUpstream}
                  onChange={(e) => setCanaryUpstream(e.target.value)}
                  required
                />
              </div>

              {/* Advanced Header & Path Matchers */}
              <div className="p-4 bg-zinc-950/60 rounded-xl border border-zinc-800/60 space-y-3">
                <div className="text-xs font-semibold text-zinc-300 flex items-center gap-1.5">
                  <Sparkles className="h-3.5 w-3.5 text-cyan-400" />
                  <span>Header & Path-Based Matchers (Deterministic Canary)</span>
                </div>

                <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                  <Input
                    label="Header Key"
                    placeholder="X-Canary"
                    value={headerKey}
                    onChange={(e) => setHeaderKey(e.target.value)}
                  />
                  <Input
                    label="Header Value"
                    placeholder="true"
                    value={headerValue}
                    onChange={(e) => setHeaderValue(e.target.value)}
                  />
                  <Input
                    label="Path Prefix Match"
                    placeholder="/v2, /beta"
                    value={pathMatch}
                    onChange={(e) => setPathMatch(e.target.value)}
                  />
                </div>
              </div>

              <div className="flex justify-end gap-2 pt-2 border-t border-zinc-800">
                <Button
                  type="submit"
                  variant="primary"
                  size="sm"
                  isLoading={setTrafficMutation.isPending}
                  leftIcon={<Sliders className="h-3.5 w-3.5" />}
                >
                  Apply Traffic Split
                </Button>
              </div>
            </Card>
          </form>
        </div>
      )}

      {/* TAB 3: Blue-Green Rollouts */}
      {activeTab === 'bluegreen' && (
        <div className="space-y-6">
          <Card className="space-y-6">
            <div>
              <h3 className="text-sm font-bold text-zinc-100 flex items-center gap-2">
                <ArrowRightLeft className="h-4 w-4 text-cyan-400" />
                <span>Zero-Downtime Blue-Green Rollout</span>
              </h3>
              <p className="text-xs text-zinc-400 mt-0.5">
                Spawns a Green container, performs health probing, cuts over Caddy proxy traffic atomically, and gracefully drains Blue connections
              </p>
            </div>

            <form onSubmit={handleBlueGreenSubmit} className="space-y-4">
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <div className="space-y-1.5">
                  <label className="block text-xs font-medium text-zinc-300">Target Application</label>
                  <select
                    value={bgAppId}
                    onChange={(e) => {
                      setBgAppId(e.target.value);
                      const app = apps?.find((a) => a.id === e.target.value);
                      if (app && app.domains?.length) {
                        setBgDomain(app.domains[0]);
                      }
                    }}
                    className="w-full rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-xs text-zinc-100 focus:border-cyan-500 focus:outline-none"
                    required
                  >
                    {apps?.map((app) => (
                      <option key={app.id} value={app.id}>
                        {app.name} ({app.id})
                      </option>
                    ))}
                  </select>
                </div>

                <Input
                  label="Target Domain"
                  placeholder="api.example.com"
                  value={bgDomain}
                  onChange={(e) => setBgDomain(e.target.value)}
                />
              </div>

              <Input
                label="New Green Release Image"
                placeholder="my-service:v2.0.0"
                value={bgImage}
                onChange={(e) => setBgImage(e.target.value)}
                required
              />

              <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
                <Input
                  label="Container Port"
                  type="number"
                  value={bgPort}
                  onChange={(e) => setBgPort(Number(e.target.value))}
                  required
                />
                <Input
                  label="Health Check Path"
                  placeholder="/healthz"
                  value={bgHealthPath}
                  onChange={(e) => setBgHealthPath(e.target.value)}
                />
                <Input
                  label="Probe Timeout (s)"
                  type="number"
                  value={bgProbeTimeout}
                  onChange={(e) => setBgProbeTimeout(Number(e.target.value))}
                />
                <Input
                  label="Drain Period (s)"
                  type="number"
                  value={bgDrainPeriod}
                  onChange={(e) => setBgDrainPeriod(Number(e.target.value))}
                />
              </div>

              <div className="flex justify-end gap-2 pt-4 border-t border-zinc-800">
                <Button
                  type="submit"
                  variant="primary"
                  size="sm"
                  isLoading={blueGreenMutation.isPending}
                  leftIcon={<Zap className="h-3.5 w-3.5 fill-current" />}
                >
                  Initiate Blue-Green Cutover
                </Button>
              </div>
            </form>
          </Card>

          {/* Rollout Result Summary */}
          {bgResult && (
            <Card className="p-5 bg-emerald-950/20 border-emerald-500/30 space-y-4">
              <div className="flex items-center gap-3">
                <CheckCircle2 className="h-6 w-6 text-emerald-400" />
                <div>
                  <h4 className="text-sm font-bold text-emerald-200">
                    Blue-Green Cutover Succeeded
                  </h4>
                  <p className="text-xs text-emerald-300/80">
                    Traffic seamlessly redirected to green container in {bgResult.duration_ms}ms
                  </p>
                </div>
              </div>

              <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 text-xs font-mono">
                <div className="p-3 bg-zinc-950 rounded-lg border border-zinc-800">
                  <div className="text-zinc-500 text-[10px]">Active Container</div>
                  <div className="text-cyan-300 mt-1 truncate">
                    {bgResult.active_container_id.slice(0, 12)}
                  </div>
                </div>
                <div className="p-3 bg-zinc-950 rounded-lg border border-zinc-800">
                  <div className="text-zinc-500 text-[10px]">Status</div>
                  <div className="text-emerald-400 capitalize mt-1">{bgResult.status}</div>
                </div>
                <div className="p-3 bg-zinc-950 rounded-lg border border-zinc-800">
                  <div className="text-zinc-500 text-[10px]">Domain</div>
                  <div className="text-zinc-200 mt-1 truncate">{bgResult.domain}</div>
                </div>
                <div className="p-3 bg-zinc-950 rounded-lg border border-zinc-800">
                  <div className="text-zinc-500 text-[10px]">Cutover Duration</div>
                  <div className="text-purple-300 mt-1">{bgResult.duration_ms} ms</div>
                </div>
              </div>
            </Card>
          )}
        </div>
      )}

      {/* Bind Domain Modal */}
      <Modal
        isOpen={isBindModalOpen}
        onClose={() => setIsBindModalOpen(false)}
        title="Bind Custom Domain"
        description="Route external traffic through Caddy with automated HTTPS certificate provisioning"
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            bindMutation.mutate({ app_id: appId, domain, auto_tls: autoTLS });
          }}
          className="space-y-4"
        >
          <Input
            label="Domain Name (FQDN)"
            placeholder="api.yourdomain.com"
            value={domain}
            onChange={(e) => setDomain(e.target.value)}
            required
          />

          <div className="space-y-1.5">
            <label className="block text-xs font-medium text-zinc-300">Target Application</label>
            <select
              value={appId}
              onChange={(e) => setAppId(e.target.value)}
              className="w-full rounded-lg border border-zinc-800 bg-zinc-950 px-3.5 py-2 text-sm text-zinc-100 focus:border-cyan-500 focus:outline-none"
              required
            >
              <option value="">Select an application...</option>
              {apps?.map((app) => (
                <option key={app.id} value={app.id}>
                  {app.name} ({app.id})
                </option>
              ))}
            </select>
          </div>

          <div className="flex items-center gap-2 pt-2">
            <input
              type="checkbox"
              id="autoTLS"
              checked={autoTLS}
              onChange={(e) => setAutoTLS(e.target.checked)}
              className="h-4 w-4 rounded border-zinc-700 bg-zinc-900 text-cyan-500 focus:ring-cyan-500"
            />
            <label htmlFor="autoTLS" className="text-xs text-zinc-300 select-none">
              Automatically issue and manage free SSL/TLS certificate (Let's Encrypt / ACME)
            </label>
          </div>

          <div className="flex justify-end gap-2 pt-4 border-t border-zinc-800">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setIsBindModalOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              variant="primary"
              size="sm"
              isLoading={bindMutation.isPending}
            >
              Bind Domain
            </Button>
          </div>
        </form>
      </Modal>

      {/* Upload Cert Modal */}
      <Modal
        isOpen={isCertModalOpen}
        onClose={() => setIsCertModalOpen(false)}
        title="Upload Custom SSL/TLS Certificate"
        description="Install custom wildcard or enterprise certificates"
        size="lg"
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            uploadCertMutation.mutate({ domain: certDomain, cert_pem: certPem, key_pem: keyPem });
          }}
          className="space-y-4"
        >
          <Input
            label="Domain / Wildcard Pattern"
            placeholder="*.example.com"
            value={certDomain}
            onChange={(e) => setCertDomain(e.target.value)}
            required
          />

          <Textarea
            label="Certificate PEM (fullchain.pem)"
            rows={5}
            placeholder="-----BEGIN CERTIFICATE-----&#10;...&#10;-----END CERTIFICATE-----"
            value={certPem}
            onChange={(e) => setCertPem(e.target.value)}
            required
          />

          <Textarea
            label="Private Key PEM (privkey.pem)"
            rows={5}
            placeholder="-----BEGIN PRIVATE KEY-----&#10;...&#10;-----END PRIVATE KEY-----"
            value={keyPem}
            onChange={(e) => setKeyPem(e.target.value)}
            required
          />

          <div className="flex justify-end gap-2 pt-4 border-t border-zinc-800">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setIsCertModalOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              variant="primary"
              size="sm"
              isLoading={uploadCertMutation.isPending}
            >
              Install Certificate
            </Button>
          </div>
        </form>
      </Modal>
    </div>
  );
}
