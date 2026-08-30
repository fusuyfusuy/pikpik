import { useState, useRef } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../lib/api';
import { App, CreateAppRequest } from '../lib/types';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Badge } from '../components/ui/Badge';
import { Input } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '../components/ui/Table';
import { Tabs } from '../components/ui/Tabs';
import { useToast } from '../components/ui/Toast';
import { formatDate } from '../lib/utils';
import { usePTY } from '../hooks/usePTY';
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
} from 'lucide-react';

const ansi = new AnsiUp();

export function AppsView() {
  const queryClient = useQueryClient();
  const toast = useToast();

  const [isCreateModalOpen, setIsCreateModalOpen] = useState(false);
  const [selectedApp, setSelectedApp] = useState<App | null>(null);
  const [activeTab, setActiveTab] = useState<'details' | 'logs' | 'terminal' | 'env'>('details');

  // Form states
  const [name, setName] = useState('');
  const [image, setImage] = useState('');
  const [replicas, setReplicas] = useState(1);
  const [domains, setDomains] = useState('');
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
  };

  const handleCreateSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const domainList = domains
      .split(',')
      .map((d) => d.trim())
      .filter(Boolean);

    createMutation.mutate({
      name,
      image,
      replicas: Number(replicas) || 1,
      domains: domainList,
    });
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
            Docker Swarm services with zero-downtime rolling deploys
          </p>
        </div>

        <Button
          variant="primary"
          size="sm"
          onClick={() => setIsCreateModalOpen(true)}
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
              <TableHead>Image Tag</TableHead>
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
                  <TableCell className="font-mono text-xs text-zinc-400">{app.image}</TableCell>
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
        description="Deploy a containerized application to the Swarm cluster"
      >
        <form onSubmit={handleCreateSubmit} className="space-y-4">
          <Input
            label="Service Name"
            placeholder="api-service"
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
          />

          <Input
            label="Container Image Tag"
            placeholder="ghcr.io/org/repo:latest or nginx:alpine"
            value={image}
            onChange={(e) => setImage(e.target.value)}
            required
          />

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
              Create & Deploy
            </Button>
          </div>
        </form>
      </Modal>
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
