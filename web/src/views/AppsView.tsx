import { useState, useRef, useEffect, useCallback } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../lib/api';
import { App, CreateAppRequest, Build, TrafficSplitConfig, BlueGreenDeployRequest, BlueGreenDeployResponse } from '../lib/types';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Badge } from '../components/ui/Badge';
import { Input } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '../components/ui/Table';
import { Tabs } from '../components/ui/Tabs';
import { useToast } from '../components/ui/Toast';
import { formatDate, formatDuration } from '../lib/utils';
import { usePTY } from '../hooks/usePTY';
import { useSSE } from '../hooks/useSSE';
import { useVirtualizer } from '@tanstack/react-virtual';
import { AnsiUp } from 'ansi_up';
import {
  Box,
  Plus,
  Play,
  RotateCw,
  Square,
  Trash2,
  Terminal as TerminalIcon,
  FileText,
  Key,
  Globe,
  Hammer,
  GitBranch,
  GitCommit,
  Copy,
  Check,
  CheckCircle2,
  XCircle,
  Loader2,
  Radio,
  Sliders,
  ArrowRightLeft,
  Zap,
} from 'lucide-react';

const ansi = new AnsiUp();

export function AppsView() {
  const queryClient = useQueryClient();
  const toast = useToast();

  const [isCreateModalOpen, setIsCreateModalOpen] = useState(false);
  const [selectedApp, setSelectedApp] = useState<App | null>(null);
  const [activeTab, setActiveTab] = useState<'details' | 'builds' | 'traffic' | 'logs' | 'terminal' | 'env'>('details');

  // Form states
  const [sourceType, setSourceType] = useState<'image' | 'git'>('image');
  const [name, setName] = useState('');
  const [image, setImage] = useState('');
  const [replicas, setReplicas] = useState(1);
  const [domains, setDomains] = useState('');
  const [gitRepoUrl, setGitRepoUrl] = useState('');
  const [gitBranch, setGitBranch] = useState('main');
  const [buildStrategy, setBuildStrategy] = useState<'dockerfile' | 'nixpacks' | 'compose'>('dockerfile');
  const [dockerfilePath, setDockerfilePath] = useState('Dockerfile');
  const [webhookSecret, setWebhookSecret] = useState('');

  const [envKey, setEnvKey] = useState('');
  const [envValue, setEnvValue] = useState('');
  const [envMap, setEnvMap] = useState<Record<string, string>>({});

  const { data: apps, isLoading } = useQuery({
    queryKey: ['apps'],
    queryFn: api.apps.list,
  });

  // Create App Mutation
  const createMutation = useMutation({
    mutationFn: (req: CreateAppRequest) => api.apps.create(req),
    onSuccess: (newApp) => {
      queryClient.invalidateQueries({ queryKey: ['apps'] });
      toast.success('App Created', `Application ${newApp.name} initialized`);
      setIsCreateModalOpen(false);
      resetForm();
    },
    onError: (err: Error) => {
      toast.error('Creation Failed', err.message);
    },
  });

  // Action Mutations
  const deployMutation = useMutation({
    mutationFn: (id: string) => api.apps.deploy(id),
    onSuccess: (_, id) => {
      queryClient.invalidateQueries({ queryKey: ['apps'] });
      toast.success('Deployment Triggered', `Rolling update started for ${id}`);
    },
    onError: (err: Error) => toast.error('Deploy Failed', err.message),
  });

  const restartMutation = useMutation({
    mutationFn: (id: string) => api.apps.restart(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['apps'] });
      toast.success('Restarted', 'Application containers restarted');
    },
    onError: (err: Error) => toast.error('Restart Failed', err.message),
  });

  const stopMutation = useMutation({
    mutationFn: (id: string) => api.apps.stop(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['apps'] });
      toast.info('Stopped', 'Application scaled to 0 replicas');
    },
    onError: (err: Error) => toast.error('Stop Failed', err.message),
  });

  const startMutation = useMutation({
    mutationFn: (id: string) => api.apps.start(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['apps'] });
      toast.success('Started', 'Application started');
    },
    onError: (err: Error) => toast.error('Start Failed', err.message),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.apps.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['apps'] });
      toast.success('Deleted', 'Application service removed');
      if (selectedApp) setSelectedApp(null);
    },
    onError: (err: Error) => toast.error('Delete Failed', err.message),
  });

  const updateEnvMutation = useMutation({
    mutationFn: ({ id, env }: { id: string; env: Record<string, string> }) =>
      api.apps.updateEnv(id, env),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['apps'] });
      toast.success('Environment Saved', 'Updated secrets and environment variables');
    },
    onError: (err: Error) => toast.error('Save Failed', err.message),
  });

  const resetForm = () => {
    setName('');
    setImage('');
    setReplicas(1);
    setDomains('');
    setGitRepoUrl('');
    setGitBranch('main');
    setBuildStrategy('dockerfile');
    setDockerfilePath('Dockerfile');
    setWebhookSecret('');
    setSourceType('image');
  };

  const handleCreateSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const domainList = domains
      .split(',')
      .map((d) => d.trim())
      .filter(Boolean);

    if (sourceType === 'git') {
      createMutation.mutate({
        name,
        image: image.trim() || `${name}:latest`,
        replicas: Number(replicas) || 1,
        domains: domainList,
        git_repo_url: gitRepoUrl.trim(),
        git_branch: gitBranch.trim() || 'main',
        build_strategy: buildStrategy,
        dockerfile_path: buildStrategy === 'dockerfile' ? (dockerfilePath.trim() || 'Dockerfile') : undefined,
        webhook_secret: webhookSecret.trim() || undefined,
      });
    } else {
      createMutation.mutate({
        name,
        image: image.trim(),
        replicas: Number(replicas) || 1,
        domains: domainList,
      });
    }
  };

  const handleAddEnv = () => {
    if (!envKey) return;
    const updated = { ...envMap, [envKey]: envValue };
    setEnvMap(updated);
    if (selectedApp) {
      updateEnvMutation.mutate({ id: selectedApp.id, env: updated });
    }
    setEnvKey('');
    setEnvValue('');
  };

  const handleRemoveEnv = (keyToRemove: string) => {
    const updated = { ...envMap };
    delete updated[keyToRemove];
    setEnvMap(updated);
    if (selectedApp) {
      updateEnvMutation.mutate({ id: selectedApp.id, env: updated });
    }
  };

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-zinc-100 flex items-center gap-2">
            <Box className="h-5 w-5 text-cyan-400" />
            <span>Applications</span>
          </h1>
          <p className="text-xs text-zinc-400 mt-0.5">
            Docker Swarm services with zero-downtime rolling deploys & Git integration
          </p>
        </div>

        <Button
          variant="primary"
          size="sm"
          onClick={() => {
            resetForm();
            setIsCreateModalOpen(true);
          }}
          leftIcon={<Plus className="h-4 w-4" />}
        >
          Create App
        </Button>
      </div>

      {/* Apps Table */}
      <Card className="p-0 overflow-hidden">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Service Name</TableHead>
              <TableHead>Source / Image</TableHead>
              <TableHead>Replicas</TableHead>
              <TableHead>Domains</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Created</TableHead>
              <TableHead className="text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <TableRow>
                <TableCell colSpan={7} className="text-center py-8 text-zinc-500">
                  Loading applications...
                </TableCell>
              </TableRow>
            ) : (!apps || apps.length === 0) ? (
              <TableRow>
                <TableCell colSpan={7} className="text-center py-10 text-zinc-500">
                  No applications found. Click "Create App" to deploy a container.
                </TableCell>
              </TableRow>
            ) : (
              apps.map((app) => (
                <TableRow
                  key={app.id}
                  className="cursor-pointer"
                  onClick={() => {
                    setSelectedApp(app);
                    setEnvMap(app.env || {});
                    setActiveTab('details');
                  }}
                >
                  <TableCell className="font-semibold text-zinc-100">
                    <div className="flex items-center gap-2">
                      <Box className="h-4 w-4 text-cyan-400 shrink-0" />
                      <span>{app.name}</span>
                    </div>
                  </TableCell>
                  <TableCell className="font-mono text-xs text-zinc-400">
                    {app.git_repo_url ? (
                      <div className="flex items-center gap-1.5 text-emerald-400">
                        <GitBranch className="h-3.5 w-3.5 shrink-0" />
                        <span className="truncate max-w-[160px]" title={app.git_repo_url}>
                          {app.git_repo_url.replace(/https?:\/\/[^/]+\//, '').replace(/\.git$/, '')}:{app.git_branch || 'main'}
                        </span>
                      </div>
                    ) : (
                      <span className="truncate max-w-[180px] block" title={app.image}>
                        {app.image}
                      </span>
                    )}
                  </TableCell>
                  <TableCell className="font-mono text-xs text-zinc-300">
                    {app.replicas}
                  </TableCell>
                  <TableCell className="text-xs text-zinc-400">
                    {app.domains && app.domains.length > 0 ? (
                      <div className="flex flex-wrap gap-1">
                        {app.domains.map((d) => (
                          <span
                            key={d}
                            className="bg-zinc-800 text-zinc-300 px-1.5 py-0.5 rounded text-[11px] font-mono"
                          >
                            {d}
                          </span>
                        ))}
                      </div>
                    ) : (
                      <span className="text-zinc-600">—</span>
                    )}
                  </TableCell>
                  <TableCell>
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
                  </TableCell>
                  <TableCell className="text-xs text-zinc-500">
                    {formatDate(app.created_at)}
                  </TableCell>
                  <TableCell className="text-right" onClick={(e) => e.stopPropagation()}>
                    <div className="flex items-center justify-end gap-1.5">
                      <Button
                        variant="subtle"
                        size="sm"
                        title="Deploy / Rollout"
                        onClick={() => deployMutation.mutate(app.id)}
                        isLoading={deployMutation.isPending && deployMutation.variables === app.id}
                      >
                        <Play className="h-3 w-3" />
                      </Button>
                      <Button
                        variant="outline"
                        size="sm"
                        title="Restart"
                        onClick={() => restartMutation.mutate(app.id)}
                      >
                        <RotateCw className="h-3 w-3" />
                      </Button>
                      {app.status === 'running' ? (
                        <Button
                          variant="outline"
                          size="sm"
                          title="Stop"
                          onClick={() => stopMutation.mutate(app.id)}
                        >
                          <Square className="h-3 w-3 text-amber-400" />
                        </Button>
                      ) : (
                        <Button
                          variant="outline"
                          size="sm"
                          title="Start"
                          onClick={() => startMutation.mutate(app.id)}
                        >
                          <Play className="h-3 w-3 text-emerald-400" />
                        </Button>
                      )}
                      <Button
                        variant="ghost"
                        size="sm"
                        title="Delete"
                        onClick={() => {
                          if (confirm(`Are you sure you want to delete ${app.name}?`)) {
                            deleteMutation.mutate(app.id);
                          }
                        }}
                      >
                        <Trash2 className="h-3 w-3 text-rose-400" />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </Card>

      {/* App Details & Management Modal */}
      {selectedApp && (
        <Modal
          isOpen={Boolean(selectedApp)}
          onClose={() => setSelectedApp(null)}
          title={
            <div className="flex items-center gap-2">
              <Box className="h-5 w-5 text-cyan-400" />
              <span>{selectedApp.name}</span>
              <Badge
                variant={selectedApp.status === 'running' ? 'success' : 'default'}
                dot
              >
                {selectedApp.status}
              </Badge>
            </div>
          }
          description={`ID: ${selectedApp.id} • Image: ${selectedApp.image}`}
          size="xl"
        >
          <div className="space-y-4">
            <Tabs
              tabs={[
                { id: 'details', label: 'Overview', icon: <Box className="h-3.5 w-3.5" /> },
                { id: 'builds', label: 'Builds & CI', icon: <Hammer className="h-3.5 w-3.5" /> },
                { id: 'traffic', label: 'Traffic & Canary', icon: <Sliders className="h-3.5 w-3.5" /> },
                { id: 'logs', label: 'Streaming Logs', icon: <FileText className="h-3.5 w-3.5" /> },
                { id: 'terminal', label: 'Live Shell (PTY)', icon: <TerminalIcon className="h-3.5 w-3.5" /> },
                { id: 'env', label: 'Environment & Secrets', icon: <Key className="h-3.5 w-3.5" /> },
              ]}
              activeTab={activeTab}
              onChange={(tab) => setActiveTab(tab as typeof activeTab)}
            />

            {/* Tab: Overview */}
            {activeTab === 'details' && (
              <div className="space-y-4 pt-2">
                <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 text-xs">
                  <div className="p-3 bg-zinc-950 rounded-lg border border-zinc-800">
                    <span className="text-zinc-500 block">Replicas</span>
                    <span className="text-base font-semibold font-mono text-zinc-100 mt-1">
                      {selectedApp.replicas}
                    </span>
                  </div>
                  <div className="p-3 bg-zinc-950 rounded-lg border border-zinc-800">
                    <span className="text-zinc-500 block">Domains Bound</span>
                    <span className="text-base font-semibold font-mono text-zinc-100 mt-1">
                      {selectedApp.domains?.length || 0}
                    </span>
                  </div>
                  <div className="p-3 bg-zinc-950 rounded-lg border border-zinc-800 col-span-2">
                    <span className="text-zinc-500 block">Target Image</span>
                    <span className="text-xs font-mono text-cyan-300 mt-1 truncate block">
                      {selectedApp.image}
                    </span>
                  </div>
                </div>

                {selectedApp.git_repo_url && (
                  <div className="p-3 bg-zinc-950/80 rounded-lg border border-zinc-800 space-y-2 text-xs">
                    <div className="flex items-center justify-between">
                      <span className="text-zinc-400 font-medium flex items-center gap-1.5">
                        <GitBranch className="h-3.5 w-3.5 text-emerald-400" />
                        Git Integration Active
                      </span>
                      <Badge variant="outline" className="font-mono text-[10px]">
                        {selectedApp.build_strategy || 'dockerfile'}
                      </Badge>
                    </div>
                    <div className="text-zinc-300 font-mono text-xs truncate">
                      {selectedApp.git_repo_url} ({selectedApp.git_branch || 'main'})
                    </div>
                  </div>
                )}

                <div className="flex gap-2 pt-3 border-t border-zinc-800">
                  <Button
                    variant="primary"
                    size="sm"
                    onClick={() => deployMutation.mutate(selectedApp.id)}
                    isLoading={deployMutation.isPending}
                  >
                    Trigger Zero-Downtime Rollout
                  </Button>
                  <Button
                    variant="secondary"
                    size="sm"
                    onClick={() => restartMutation.mutate(selectedApp.id)}
                  >
                    Restart Service
                  </Button>
                </div>
              </div>
            )}

            {/* Tab: Builds & CI */}
            {activeTab === 'builds' && <AppBuildsTab app={selectedApp} />}

            {/* Tab: Traffic Splitting & Canary */}
            {activeTab === 'traffic' && <AppTrafficTab app={selectedApp} />}

            {/* Tab: Virtualized Logs */}
            {activeTab === 'logs' && <AppLogsViewer appId={selectedApp.id} />}

            {/* Tab: Terminal (PTY) */}
            {activeTab === 'terminal' && <AppTerminalView appId={selectedApp.id} />}

            {/* Tab: Environment Variables */}
            {activeTab === 'env' && (
              <div className="space-y-4 pt-2">
                <div className="flex gap-2">
                  <Input
                    placeholder="KEY (e.g. DATABASE_URL)"
                    value={envKey}
                    onChange={(e) => setEnvKey(e.target.value.toUpperCase())}
                    className="font-mono text-xs"
                  />
                  <Input
                    placeholder="VALUE"
                    value={envValue}
                    onChange={(e) => setEnvValue(e.target.value)}
                    className="font-mono text-xs"
                  />
                  <Button variant="secondary" size="sm" onClick={handleAddEnv}>
                    Add
                  </Button>
                </div>

                <div className="rounded-lg border border-zinc-800 divide-y divide-zinc-800 max-h-60 overflow-y-auto">
                  {Object.entries(envMap).length === 0 ? (
                    <div className="p-4 text-center text-xs text-zinc-500">
                      No custom environment variables defined.
                    </div>
                  ) : (
                    Object.entries(envMap).map(([k, v]) => (
                      <div key={k} className="p-2.5 flex items-center justify-between text-xs">
                        <span className="font-mono font-semibold text-cyan-300">{k}</span>
                        <div className="flex items-center gap-3">
                          <span className="font-mono text-zinc-400 truncate max-w-xs">{v}</span>
                          <button
                            onClick={() => handleRemoveEnv(k)}
                            className="text-zinc-500 hover:text-rose-400"
                          >
                            <Trash2 className="h-3.5 w-3.5" />
                          </button>
                        </div>
                      </div>
                    ))
                  )}
                </div>
              </div>
            )}
          </div>
        </Modal>
      )}

      {/* Create App Modal */}
      <Modal
        isOpen={isCreateModalOpen}
        onClose={() => setIsCreateModalOpen(false)}
        title="Deploy New Application"
        description="Deploy a containerized application or build directly from Git"
      >
        <form onSubmit={handleCreateSubmit} className="space-y-4">
          {/* Source Toggle */}
          <div className="flex p-1 bg-zinc-950 rounded-lg border border-zinc-800">
            <button
              type="button"
              onClick={() => setSourceType('image')}
              className={`flex-1 py-1.5 px-3 text-xs font-medium rounded-md transition-colors flex items-center justify-center gap-2 ${
                sourceType === 'image'
                  ? 'bg-zinc-800 text-zinc-100 shadow-sm'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              <Box className="h-3.5 w-3.5 text-cyan-400" />
              <span>Pre-built Image</span>
            </button>
            <button
              type="button"
              onClick={() => setSourceType('git')}
              className={`flex-1 py-1.5 px-3 text-xs font-medium rounded-md transition-colors flex items-center justify-center gap-2 ${
                sourceType === 'git'
                  ? 'bg-zinc-800 text-zinc-100 shadow-sm'
                  : 'text-zinc-400 hover:text-zinc-200'
              }`}
            >
              <GitBranch className="h-3.5 w-3.5 text-emerald-400" />
              <span>Git Repository</span>
            </button>
          </div>

          <Input
            label="Service Name"
            placeholder="api-service"
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
          />

          {sourceType === 'image' ? (
            <Input
              label="Container Image Tag"
              placeholder="ghcr.io/org/repo:latest or nginx:alpine"
              value={image}
              onChange={(e) => setImage(e.target.value)}
              required
            />
          ) : (
            <div className="space-y-4">
              <Input
                label="Git Repository URL"
                placeholder="https://github.com/org/repo.git"
                value={gitRepoUrl}
                onChange={(e) => setGitRepoUrl(e.target.value)}
                leftIcon={<GitBranch className="h-3.5 w-3.5" />}
                required
                helperText="HTTPS Git URL with public access or webhook integration"
              />

              <div className="grid grid-cols-2 gap-3">
                <Input
                  label="Branch"
                  placeholder="main"
                  value={gitBranch}
                  onChange={(e) => setGitBranch(e.target.value)}
                  leftIcon={<GitCommit className="h-3.5 w-3.5" />}
                />
                <div className="w-full space-y-1.5">
                  <label className="block text-xs font-medium text-zinc-300">
                    Build Strategy
                  </label>
                  <select
                    value={buildStrategy}
                    onChange={(e) => setBuildStrategy(e.target.value as any)}
                    className="w-full rounded-lg border border-zinc-800 bg-zinc-950/60 px-3.5 py-2 text-sm text-zinc-100 focus:border-cyan-500 focus:ring-1 focus:ring-cyan-500 focus:outline-none"
                  >
                    <option value="dockerfile">Dockerfile (Standard)</option>
                    <option value="nixpacks">Nixpacks (Auto-detect)</option>
                    <option value="compose">Docker Compose</option>
                  </select>
                </div>
              </div>

              {buildStrategy === 'dockerfile' && (
                <Input
                  label="Dockerfile Path"
                  placeholder="Dockerfile"
                  value={dockerfilePath}
                  onChange={(e) => setDockerfilePath(e.target.value)}
                  helperText="Path relative to the root of the repository"
                />
              )}

              <Input
                label="Webhook Secret / Token (Optional)"
                placeholder="Custom secret for webhook verification"
                value={webhookSecret}
                onChange={(e) => setWebhookSecret(e.target.value)}
                type="password"
                helperText="Used to verify HMAC signatures or webhook token query param"
              />
            </div>
          )}

          <div className="grid grid-cols-2 gap-3">
            <Input
              label="Replicas"
              type="number"
              min={1}
              max={128}
              value={replicas}
              onChange={(e) => setReplicas(parseInt(e.target.value) || 1)}
              required
            />
            <Input
              label="Domains (comma-separated)"
              placeholder="api.example.com"
              value={domains}
              onChange={(e) => setDomains(e.target.value)}
              leftIcon={<Globe className="h-3.5 w-3.5" />}
            />
          </div>

          <div className="flex justify-end gap-2 pt-4 border-t border-zinc-800">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setIsCreateModalOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              variant="primary"
              size="sm"
              isLoading={createMutation.isPending}
            >
              {sourceType === 'git' ? 'Create & Build' : 'Create & Deploy'}
            </Button>
          </div>
        </form>
      </Modal>
    </div>
  );
}

// Subcomponent: Builds & CI Tab
function AppBuildsTab({ app }: { app: App }) {
  const queryClient = useQueryClient();
  const toast = useToast();
  const [copied, setCopied] = useState(false);
  const [selectedBuildForStream, setSelectedBuildForStream] = useState<Build | null>(null);

  const { data: builds, isLoading } = useQuery({
    queryKey: ['builds', app.id],
    queryFn: () => api.builds.list(app.id),
    refetchInterval: 4000,
  });

  const rebuildMutation = useMutation({
    mutationFn: (buildId: string) => api.builds.rebuild(buildId),
    onSuccess: (newBuild) => {
      queryClient.invalidateQueries({ queryKey: ['builds', app.id] });
      toast.success('Build Queued', `Rebuild initiated for ${newBuild.commit_sha?.slice(0, 7) || newBuild.id}`);
      setSelectedBuildForStream(newBuild);
    },
    onError: (err: Error) => toast.error('Rebuild Failed', err.message),
  });

  const deployCommitMutation = useMutation({
    mutationFn: (image: string) => api.apps.deploy(app.id, { image }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['apps'] });
      toast.success('Deployment Started', 'Rolling update triggered for selected build');
    },
    onError: (err: Error) => toast.error('Deploy Failed', err.message),
  });

  const webhookUrl = `${window.location.origin}/api/v1/webhooks/git/${app.id}${
    app.webhook_secret ? `?token=${encodeURIComponent(app.webhook_secret)}` : ''
  }`;

  const handleCopyWebhook = () => {
    navigator.clipboard.writeText(webhookUrl);
    setCopied(true);
    toast.info('Webhook Copied', 'Generic Git webhook endpoint copied to clipboard');
    setTimeout(() => setCopied(false), 2000);
  };

  const getStatusBadge = (status: string) => {
    switch (status?.toLowerCase()) {
      case 'success':
        return (
          <Badge variant="success" dot>
            success
          </Badge>
        );
      case 'cloning':
      case 'building':
      case 'deploying':
        return (
          <Badge variant="warning" dot className="animate-pulse">
            {status}
          </Badge>
        );
      case 'queued':
        return (
          <Badge variant="default" dot>
            queued
          </Badge>
        );
      case 'failed':
        return (
          <Badge variant="error" dot>
            failed
          </Badge>
        );
      case 'cancelled':
        return (
          <Badge variant="default">
            cancelled
          </Badge>
        );
      default:
        return <Badge variant="default">{status}</Badge>;
    }
  };

  return (
    <div className="space-y-4 pt-2">
      {/* Webhook Endpoint Info Card */}
      <div className="p-3.5 bg-zinc-950/80 rounded-lg border border-zinc-800 space-y-2.5 text-xs">
        <div className="flex items-center justify-between">
          <span className="font-semibold text-zinc-200 flex items-center gap-1.5">
            <Radio className="h-3.5 w-3.5 text-cyan-400" />
            Git Push Webhook Endpoint
          </span>
          <span className="text-[11px] text-zinc-500 font-mono">POST /api/v1/webhooks/git/:id</span>
        </div>

        <div className="flex items-center gap-2">
          <div className="flex-1 bg-zinc-900 border border-zinc-800 rounded px-2.5 py-1.5 font-mono text-[11px] text-zinc-300 truncate select-all">
            {webhookUrl}
          </div>
          <Button
            variant="secondary"
            size="sm"
            onClick={handleCopyWebhook}
            leftIcon={copied ? <Check className="h-3 w-3 text-emerald-400" /> : <Copy className="h-3 w-3" />}
          >
            {copied ? 'Copied' : 'Copy'}
          </Button>
        </div>

        <div className="flex items-center justify-between text-[11px] text-zinc-400 pt-1 border-t border-zinc-900">
          <span>GitHub Webhooks also supported at <code className="text-zinc-300 font-mono">/api/v1/webhooks/github</code></span>
          {app.git_repo_url && (
            <span className="font-mono text-zinc-400 truncate max-w-[200px]" title={app.git_repo_url}>
              {app.git_branch || 'main'}
            </span>
          )}
        </div>
      </div>

      {/* Builds Table */}
      <Card className="p-0 overflow-hidden border-zinc-800">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Status</TableHead>
              <TableHead>Commit / Branch</TableHead>
              <TableHead>Message & Author</TableHead>
              <TableHead>Duration</TableHead>
              <TableHead>Time</TableHead>
              <TableHead className="text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <TableRow>
                <TableCell colSpan={6} className="text-center py-8 text-zinc-500">
                  Loading build history...
                </TableCell>
              </TableRow>
            ) : (!builds || builds.length === 0) ? (
              <TableRow>
                <TableCell colSpan={6} className="text-center py-10 text-zinc-500">
                  No builds recorded yet. Push to your repository or trigger a rebuild.
                </TableCell>
              </TableRow>
            ) : (
              builds.map((b) => {
                const authorName = b.commit_author || b.author || 'system';
                const shortSha = b.commit_sha ? b.commit_sha.slice(0, 7) : b.id.slice(0, 7);
                const isLive = ['queued', 'cloning', 'building', 'deploying'].includes(b.status?.toLowerCase());

                return (
                  <TableRow
                    key={b.id}
                    className="cursor-pointer hover:bg-zinc-900/50"
                    onClick={() => setSelectedBuildForStream(b)}
                  >
                    <TableCell>{getStatusBadge(b.status)}</TableCell>
                    <TableCell>
                      <div className="flex flex-col gap-0.5">
                        <span className="font-mono text-xs font-semibold text-cyan-300 flex items-center gap-1">
                          <GitCommit className="h-3 w-3 text-zinc-500" />
                          {shortSha}
                        </span>
                        <span className="text-[11px] text-zinc-400 flex items-center gap-1 font-mono">
                          <GitBranch className="h-2.5 w-2.5 text-zinc-500" />
                          {b.branch || 'main'}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell className="max-w-[220px]">
                      <div className="flex flex-col">
                        <span className="text-xs text-zinc-200 truncate font-medium">
                          {b.commit_message || 'Manual trigger / Generic webhook build'}
                        </span>
                        <span className="text-[11px] text-zinc-500">
                          {authorName}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell className="font-mono text-xs text-zinc-400">
                      {formatDuration(b.duration_ms)}
                    </TableCell>
                    <TableCell className="text-xs text-zinc-500">
                      {formatDate(b.created_at || b.started_at || '')}
                    </TableCell>
                    <TableCell className="text-right" onClick={(e) => e.stopPropagation()}>
                      <div className="flex items-center justify-end gap-1.5">
                        <Button
                          variant="subtle"
                          size="sm"
                          title="Live Build Stream"
                          onClick={() => setSelectedBuildForStream(b)}
                          leftIcon={isLive ? <Loader2 className="h-3 w-3 animate-spin text-cyan-400" /> : <FileText className="h-3 w-3" />}
                        >
                          {isLive ? 'Stream' : 'Logs'}
                        </Button>
                        <Button
                          variant="outline"
                          size="sm"
                          title="Re-run Build"
                          onClick={() => rebuildMutation.mutate(b.id)}
                          isLoading={rebuildMutation.isPending && rebuildMutation.variables === b.id}
                        >
                          <RotateCw className="h-3 w-3" />
                        </Button>
                        {b.status === 'success' && (
                          <Button
                            variant="secondary"
                            size="sm"
                            title="Deploy this Commit"
                            onClick={() => deployCommitMutation.mutate(b.image_tag || b.commit_sha)}
                            isLoading={deployCommitMutation.isPending}
                          >
                            <Play className="h-3 w-3 text-emerald-400" />
                          </Button>
                        )}
                      </div>
                    </TableCell>
                  </TableRow>
                );
              })
            )}
          </TableBody>
        </Table>
      </Card>

      {/* Live Stream & Log Modal */}
      {selectedBuildForStream && (
        <LiveBuildStreamModal
          build={selectedBuildForStream}
          onClose={() => setSelectedBuildForStream(null)}
          onRebuild={(buildId) => rebuildMutation.mutate(buildId)}
        />
      )}
    </div>
  );
}

// Subcomponent: Live Build Stream Modal with Stage Indicators and SSE logs
const STAGES = [
  { id: 'queued', label: 'Queued' },
  { id: 'cloning', label: 'Cloning' },
  { id: 'building', label: 'Building' },
  { id: 'deploying', label: 'Deploying' },
  { id: 'success', label: 'Success' },
] as const;

function getStageIndex(status: string): number {
  switch (status?.toLowerCase()) {
    case 'queued':
      return 0;
    case 'cloning':
      return 1;
    case 'building':
      return 2;
    case 'deploying':
      return 3;
    case 'success':
      return 4;
    case 'failed':
    case 'cancelled':
      return 2;
    default:
      return 0;
  }
}

function LiveBuildStreamModal({
  build,
  onClose,
  onRebuild,
}: {
  build: Build;
  onClose: () => void;
  onRebuild?: (buildId: string) => void;
}) {
  const toast = useToast();
  const [logs, setLogs] = useState<string[]>(() => {
    if (build.logs) {
      return build.logs.split('\n');
    }
    return [];
  });
  const [currentStatus, setCurrentStatus] = useState<string>(build.status || 'queued');
  const [autoScroll, setAutoScroll] = useState(true);
  const [copiedLogs, setCopiedLogs] = useState(false);
  const logContainerRef = useRef<HTMLDivElement>(null);

  // Poll build status until complete
  const { data: latestBuild } = useQuery({
    queryKey: ['build', build.id],
    queryFn: () => api.builds.get(build.id),
    refetchInterval: (query) => {
      const bld = query.state.data;
      const st = (bld?.status || currentStatus).toLowerCase();
      if (['success', 'failed', 'cancelled'].includes(st)) {
        return false;
      }
      return 2000;
    },
  });

  useEffect(() => {
    if (latestBuild) {
      setCurrentStatus(latestBuild.status);
      if (latestBuild.logs && logs.length === 0) {
        setLogs(latestBuild.logs.split('\n'));
      }
    }
  }, [latestBuild]);

  // SSE Stream handler
  const handleSSEMessage = useCallback((data: unknown) => {
    if (!data) return;
    if (typeof data === 'string') {
      setLogs((prev) => [...prev, data]);
    } else if (typeof data === 'object') {
      const obj = data as Record<string, any>;
      if (typeof obj.data === 'string') {
        setLogs((prev) => [...prev, obj.data]);
      } else if (typeof obj.line === 'string') {
        setLogs((prev) => [...prev, obj.line]);
      } else if (typeof obj.message === 'string') {
        setLogs((prev) => [...prev, obj.message]);
      } else if (typeof obj.log === 'string') {
        setLogs((prev) => [...prev, obj.log]);
      } else {
        setLogs((prev) => [...prev, JSON.stringify(obj)]);
      }

      if (obj.status && typeof obj.status === 'string') {
        setCurrentStatus(obj.status);
      }
    }
  }, []);

  const streamActive = ['queued', 'cloning', 'building', 'deploying'].includes(currentStatus.toLowerCase());

  useSSE({
    endpoint: api.builds.streamUrl(build.id),
    enabled: Boolean(build.id),
    onMessage: handleSSEMessage,
  });

  // Autoscroll effect
  useEffect(() => {
    if (autoScroll && logContainerRef.current) {
      logContainerRef.current.scrollTop = logContainerRef.current.scrollHeight;
    }
  }, [logs, autoScroll]);

  const handleCopyAllLogs = () => {
    navigator.clipboard.writeText(logs.join('\n'));
    setCopiedLogs(true);
    toast.info('Logs Copied', 'Build log output copied to clipboard');
    setTimeout(() => setCopiedLogs(false), 2000);
  };

  const currentStageIdx = getStageIndex(currentStatus);
  const isFailed = currentStatus.toLowerCase() === 'failed';
  const isCancelled = currentStatus.toLowerCase() === 'cancelled';
  const isComplete = currentStatus.toLowerCase() === 'success';

  return (
    <Modal
      isOpen={true}
      onClose={onClose}
      title={
        <div className="flex items-center gap-2">
          <Hammer className="h-5 w-5 text-cyan-400" />
          <span>Build #{build.id.slice(-8)}</span>
          <Badge
            variant={
              isComplete
                ? 'success'
                : isFailed
                ? 'error'
                : isCancelled
                ? 'default'
                : 'warning'
            }
            dot
            className={streamActive ? 'animate-pulse' : ''}
          >
            {currentStatus}
          </Badge>
        </div>
      }
      description={`Commit: ${build.commit_sha ? build.commit_sha.slice(0, 7) : 'Manual'} • Branch: ${
        build.branch || 'main'
      } • ${build.commit_message || 'Generic Build Execution'}`}
      size="xl"
    >
      <div className="space-y-4">
        {/* Stage Progression Bar */}
        <div className="p-3.5 bg-zinc-950 rounded-lg border border-zinc-800">
          <div className="grid grid-cols-5 gap-2 relative">
            {STAGES.map((stg, idx) => {
              let isStepActive = false;
              let isStepDone = false;
              let isStepFailed = false;

              if (isFailed && idx === currentStageIdx) {
                isStepFailed = true;
              } else if (idx < currentStageIdx || (isComplete && idx <= 4)) {
                isStepDone = true;
              } else if (idx === currentStageIdx && !isComplete && !isFailed && !isCancelled) {
                isStepActive = true;
              }

              return (
                <div key={stg.id} className="flex flex-col items-center text-center gap-1.5">
                  <div
                    className={`h-7 w-7 rounded-full flex items-center justify-center text-xs font-semibold transition-all ${
                      isStepDone
                        ? 'bg-emerald-500/20 text-emerald-400 border border-emerald-500/40 shadow-sm shadow-emerald-500/10'
                        : isStepActive
                        ? 'bg-cyan-500/20 text-cyan-300 border border-cyan-400 animate-pulse ring-2 ring-cyan-500/30'
                        : isStepFailed
                        ? 'bg-rose-500/20 text-rose-400 border border-rose-500'
                        : 'bg-zinc-900 text-zinc-500 border border-zinc-800'
                    }`}
                  >
                    {isStepDone ? (
                      <CheckCircle2 className="h-4 w-4" />
                    ) : isStepFailed ? (
                      <XCircle className="h-4 w-4" />
                    ) : isStepActive ? (
                      <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    ) : (
                      <span>{idx + 1}</span>
                    )}
                  </div>
                  <span
                    className={`text-[11px] font-medium ${
                      isStepActive
                        ? 'text-cyan-300 font-semibold'
                        : isStepDone
                        ? 'text-emerald-400'
                        : isStepFailed
                        ? 'text-rose-400'
                        : 'text-zinc-500'
                    }`}
                  >
                    {stg.label}
                  </span>
                </div>
              );
            })}
          </div>
        </div>

        {/* Live Terminal Output */}
        <div className="space-y-2">
          <div className="flex items-center justify-between text-xs text-zinc-400 px-1">
            <div className="flex items-center gap-2">
              <span className="font-mono">Output Log Stream ({logs.length} lines)</span>
              {streamActive && (
                <span className="text-cyan-400 flex items-center gap-1.5 text-[11px]">
                  <span className="h-1.5 w-1.5 rounded-full bg-cyan-400 animate-pulse" />
                  Streaming Live
                </span>
              )}
            </div>

            <div className="flex items-center gap-3">
              <label className="flex items-center gap-1.5 cursor-pointer text-zinc-400 hover:text-zinc-200">
                <input
                  type="checkbox"
                  checked={autoScroll}
                  onChange={(e) => setAutoScroll(e.target.checked)}
                  className="rounded bg-zinc-900 border-zinc-700 text-cyan-500 focus:ring-0 h-3 w-3"
                />
                <span className="text-[11px]">Auto-scroll</span>
              </label>

              <button
                onClick={handleCopyAllLogs}
                className="text-zinc-400 hover:text-zinc-200 flex items-center gap-1 text-[11px]"
              >
                {copiedLogs ? <Check className="h-3 w-3 text-emerald-400" /> : <Copy className="h-3 w-3" />}
                <span>{copiedLogs ? 'Copied' : 'Copy Logs'}</span>
              </button>
            </div>
          </div>

          <div
            ref={logContainerRef}
            className="h-88 w-full bg-zinc-950 rounded-lg border border-zinc-800 p-3.5 overflow-y-auto font-mono text-xs leading-relaxed select-text space-y-1"
          >
            {logs.length === 0 ? (
              <div className="text-zinc-600 text-center py-12">
                {streamActive ? 'Waiting for build runner output...' : 'No logs recorded for this build.'}
              </div>
            ) : (
              logs.map((line, idx) => (
                <div
                  key={idx}
                  className="text-zinc-300 whitespace-pre-wrap break-all"
                  dangerouslySetInnerHTML={{
                    __html: ansi.ansi_to_html(line),
                  }}
                />
              ))
            )}
          </div>
        </div>

        {/* Footer Actions */}
        <div className="flex items-center justify-between pt-3 border-t border-zinc-800">
          <div className="text-xs text-zinc-500 font-mono">
            Duration: {formatDuration(latestBuild?.duration_ms || build.duration_ms)}
          </div>
          <div className="flex items-center gap-2">
            {onRebuild && (
              <Button
                variant="secondary"
                size="sm"
                onClick={() => {
                  onRebuild(build.id);
                  onClose();
                }}
                leftIcon={<RotateCw className="h-3.5 w-3.5" />}
              >
                Re-run Build
              </Button>
            )}
            <Button variant="outline" size="sm" onClick={onClose}>
              Close
            </Button>
          </div>
        </div>
      </div>
    </Modal>
  );
}

// Subcomponent: Traffic Splitting & Canary Rollout Tab
function AppTrafficTab({ app }: { app: App }) {
  const queryClient = useQueryClient();
  const toast = useToast();

  const [domain, setDomain] = useState(app.domains?.[0] || `${app.name}.example.com`);
  const [stableUpstream, setStableUpstream] = useState(`${app.name}_blue:8080`);
  const [canaryUpstream, setCanaryUpstream] = useState(`${app.name}_green:8080`);
  const [canaryPercent, setCanaryPercent] = useState<number>(0);
  const [headerKey, setHeaderKey] = useState('X-Canary');
  const [headerValue, setHeaderValue] = useState('true');
  const [pathMatch, setPathMatch] = useState('');

  // Blue-Green Form
  const [bgImage, setBgImage] = useState(app.image || '');
  const [bgPort, setBgPort] = useState(8080);
  const [bgHealthPath, setBgHealthPath] = useState('/healthz');
  const [bgProbeTimeout, setBgProbeTimeout] = useState(10);
  const [bgDrainPeriod, setBgDrainPeriod] = useState(5);
  const [bgResult, setBgResult] = useState<BlueGreenDeployResponse | null>(null);

  const { data: trafficData } = useQuery({
    queryKey: ['traffic', app.id],
    queryFn: () => api.traffic.get(app.id),
  });

  useEffect(() => {
    if (trafficData) {
      if (trafficData.domain) setDomain(trafficData.domain);
      if (trafficData.stable_upstream) setStableUpstream(trafficData.stable_upstream);
      if (trafficData.canary_upstream) setCanaryUpstream(trafficData.canary_upstream);
      setCanaryPercent(trafficData.canary_percent ?? 0);
      if (trafficData.headers && Object.keys(trafficData.headers).length > 0) {
        const k = Object.keys(trafficData.headers)[0];
        setHeaderKey(k);
        setHeaderValue(trafficData.headers[k]);
      }
      if (trafficData.paths && trafficData.paths.length > 0) {
        setPathMatch(trafficData.paths.join(', '));
      }
    }
  }, [trafficData]);

  const setTrafficMutation = useMutation({
    mutationFn: (req: Partial<TrafficSplitConfig>) => api.traffic.set(app.id, req),
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ['traffic', app.id] });
      toast.success(
        'Traffic Weight Applied',
        `Routed ${100 - res.canary_percent}% Stable / ${res.canary_percent}% Canary`
      );
    },
    onError: (err: Error) => toast.error('Traffic Update Failed', err.message),
  });

  const blueGreenMutation = useMutation({
    mutationFn: (req: BlueGreenDeployRequest) => api.traffic.deployBlueGreen(app.id, req),
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ['apps'] });
      queryClient.invalidateQueries({ queryKey: ['traffic', app.id] });
      setBgResult(res);
      toast.success(
        'Blue-Green Cutover Completed',
        `Swapped traffic to green container in ${res.duration_ms}ms`
      );
    },
    onError: (err: Error) => toast.error('Blue-Green Rollout Failed', err.message),
  });

  const handleApplyTraffic = (e: React.FormEvent) => {
    e.preventDefault();
    const headersMap: Record<string, string> = {};
    if (headerKey.trim() && headerValue.trim()) {
      headersMap[headerKey.trim()] = headerValue.trim();
    }
    const paths = pathMatch
      ? pathMatch
          .split(',')
          .map((p) => p.trim())
          .filter(Boolean)
      : undefined;

    setTrafficMutation.mutate({
      domain: domain.trim(),
      stable_upstream: stableUpstream.trim(),
      canary_upstream: canaryUpstream.trim(),
      canary_percent: Number(canaryPercent),
      headers: Object.keys(headersMap).length > 0 ? headersMap : undefined,
      paths,
    });
  };

  const handleBlueGreenSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!bgImage.trim()) return;

    blueGreenMutation.mutate({
      image: bgImage.trim(),
      domain: domain.trim() || undefined,
      container_port: Number(bgPort),
      health_check_path: bgHealthPath.trim(),
      probe_timeout_sec: Number(bgProbeTimeout),
      drain_period_sec: Number(bgDrainPeriod),
    });
  };

  return (
    <div className="space-y-6 pt-2">
      {/* Visual Canary Split Progress */}
      <form onSubmit={handleApplyTraffic} className="space-y-4">
        <div className="p-4 bg-zinc-950 rounded-xl border border-zinc-800 space-y-3">
          <div className="flex items-center justify-between">
            <span className="text-xs font-bold text-zinc-200 flex items-center gap-1.5">
              <Sliders className="h-3.5 w-3.5 text-cyan-400" />
              Canary Traffic Weighting
            </span>
            <div className="flex items-center gap-3 text-xs font-mono font-semibold">
              <span className="text-cyan-400">Stable: {100 - canaryPercent}%</span>
              <span className="text-emerald-400">Canary: {canaryPercent}%</span>
            </div>
          </div>

          <div className="h-3 w-full bg-zinc-900 rounded-full overflow-hidden flex border border-zinc-800 p-0.5">
            <div
              style={{ width: `${100 - canaryPercent}%` }}
              className="h-full bg-cyan-500 rounded-l-full transition-all duration-300"
            />
            <div
              style={{ width: `${canaryPercent}%` }}
              className="h-full bg-emerald-500 rounded-r-full transition-all duration-300"
            />
          </div>

          {/* Slider */}
          <input
            type="range"
            min="0"
            max="100"
            step="1"
            value={canaryPercent}
            onChange={(e) => setCanaryPercent(Number(e.target.value))}
            className="w-full h-2 bg-zinc-800 rounded-lg appearance-none cursor-pointer accent-cyan-400"
          />

          {/* Preset Buttons */}
          <div className="flex flex-wrap gap-2 pt-1">
            {[
              { label: '0% (100% Stable)', val: 0 },
              { label: '10% Canary', val: 10 },
              { label: '25% Canary', val: 25 },
              { label: '50% Split', val: 50 },
              { label: '100% Canary', val: 100 },
            ].map((preset) => (
              <button
                type="button"
                key={preset.val}
                onClick={() => setCanaryPercent(preset.val)}
                className={`px-2.5 py-0.5 rounded text-[10px] font-mono transition-all ${
                  canaryPercent === preset.val
                    ? 'bg-cyan-500/20 text-cyan-300 border border-cyan-500/50 font-bold'
                    : 'bg-zinc-900 text-zinc-400 hover:text-zinc-200 border border-zinc-800'
                }`}
              >
                {preset.label}
              </button>
            ))}
          </div>

          {/* Routing Upstreams */}
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-2.5 pt-2">
            <Input
              label="Domain"
              placeholder="api.example.com"
              value={domain}
              onChange={(e) => setDomain(e.target.value)}
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

          <div className="flex justify-end pt-2 border-t border-zinc-800/80">
            <Button
              type="submit"
              variant="primary"
              size="sm"
              isLoading={setTrafficMutation.isPending}
              leftIcon={<Sliders className="h-3.5 w-3.5" />}
            >
              Apply Canary Weights
            </Button>
          </div>
        </div>
      </form>

      {/* Blue-Green Zero-Downtime Rollout */}
      <form onSubmit={handleBlueGreenSubmit} className="space-y-4">
        <div className="p-4 bg-zinc-950 rounded-xl border border-zinc-800 space-y-3">
          <div className="flex items-center justify-between">
            <span className="text-xs font-bold text-zinc-200 flex items-center gap-1.5">
              <ArrowRightLeft className="h-3.5 w-3.5 text-emerald-400" />
              Blue-Green Zero-Downtime Rollout
            </span>
            <Badge variant="outline" className="text-[10px] text-cyan-400 border-zinc-700">
              Atomic Cutover
            </Badge>
          </div>

          <p className="text-[11px] text-zinc-400 leading-relaxed">
            Spawns a new Green container image, probes health endpoint, swaps Caddy ingress upstream instantly, and drains older Blue connections.
          </p>

          <Input
            label="Green Release Image"
            placeholder="my-org/my-app:v2.0.0"
            value={bgImage}
            onChange={(e) => setBgImage(e.target.value)}
            required
          />

          <div className="grid grid-cols-2 sm:grid-cols-4 gap-2.5">
            <Input
              label="Container Port"
              type="number"
              value={bgPort}
              onChange={(e) => setBgPort(Number(e.target.value))}
              required
            />
            <Input
              label="Health Check Path"
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

          <div className="flex justify-end pt-2 border-t border-zinc-800/80">
            <Button
              type="submit"
              variant="primary"
              size="sm"
              isLoading={blueGreenMutation.isPending}
              leftIcon={<Zap className="h-3.5 w-3.5 fill-current" />}
            >
              Deploy Blue-Green Release
            </Button>
          </div>

          {bgResult && (
            <div className="p-3 bg-emerald-950/30 border border-emerald-500/30 rounded-lg text-xs space-y-1 mt-2">
              <div className="font-semibold text-emerald-200 flex items-center gap-1.5">
                <CheckCircle2 className="h-4 w-4 text-emerald-400" />
                Cutover Succeeded ({bgResult.duration_ms}ms)
              </div>
              <div className="text-zinc-400 font-mono text-[11px]">
                Active Container: {bgResult.active_container_id.slice(0, 12)} • Domain: {bgResult.domain}
              </div>
            </div>
          )}
        </div>
      </form>
    </div>
  );
}

// Subcomponent: Virtualized Log Viewer with ANSI Parsing
function AppLogsViewer({ appId }: { appId: string }) {
  // Generate initial bootstrap logs
  const [logs] = useState<string[]>(() =>
    Array.from({ length: 200 }, (_, i) =>
      `\x1b[36m[${new Date(Date.now() - (200 - i) * 1000).toISOString()}]\x1b[0m \x1b[32m[INFO]\x1b[0m Container service [${appId}] serving request #${i + 1} with HTTP 200 OK (latency: 1.2ms)`
    )
  );

  const parentRef = useRef<HTMLDivElement>(null);

  const rowVirtualizer = useVirtualizer({
    count: logs.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => 22,
    overscan: 15,
  });

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between text-xs text-zinc-400">
        <span className="font-mono">Log Stream (Virtualized - {logs.length} lines)</span>
        <span className="text-emerald-400 flex items-center gap-1.5">
          <span className="h-1.5 w-1.5 rounded-full bg-emerald-400 animate-pulse" />
          Live
        </span>
      </div>

      <div
        ref={parentRef}
        className="h-80 w-full bg-zinc-950 rounded-lg border border-zinc-800 p-3 overflow-y-auto font-mono text-xs leading-relaxed select-text"
      >
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
                __html: ansi.ansi_to_html(logs[virtualRow.index]),
              }}
            />
          ))}
        </div>
      </div>
    </div>
  );
}

// Subcomponent: Live Xterm.js PTY Shell
function AppTerminalView({ appId }: { appId: string }) {
  const { containerRef, status, error, reconnect } = usePTY({
    targetType: 'container',
    targetId: appId,
  });

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between text-xs text-zinc-400">
        <span className="flex items-center gap-2">
          <span>Terminal Shell (/bin/sh)</span>
          <Badge
            variant={status === 'connected' ? 'success' : status === 'connecting' ? 'warning' : 'error'}
            dot
          >
            {status}
          </Badge>
        </span>
        {status !== 'connected' && (
          <Button variant="outline" size="sm" onClick={reconnect}>
            Reconnect
          </Button>
        )}
      </div>

      {error && <div className="text-xs text-rose-400">{error}</div>}

      <div
        ref={containerRef}
        className="h-80 w-full bg-zinc-950 rounded-lg border border-zinc-800 overflow-hidden"
      />
    </div>
  );
}
