import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../lib/api';
import { Stack, CreateStackRequest, CreateNetworkRequest, CreateVolumeRequest } from '../lib/types';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Badge } from '../components/ui/Badge';
import { Input, Textarea } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { Tabs } from '../components/ui/Tabs';
import { useToast } from '../components/ui/Toast';
import { QueryErrorAlert } from '../components/ui/QueryErrorAlert';
import { formatDate } from '../lib/utils';
import {
  Layers,
  Network,
  HardDrive,
  Plus,
  Play,
  RotateCw,
  Square,
  Trash2,
  Code2,
  Server,
  Activity,
  Boxes,
  RefreshCw,
  CheckCircle,
  AlertCircle,
} from 'lucide-react';

const SAMPLE_COMPOSE = `version: '3.8'
services:
  web:
    image: nginx:alpine
    ports:
      - "80:80"
    deploy:
      replicas: 2
      restart_policy:
        condition: on-failure
  redis:
    image: redis:7-alpine
    deploy:
      replicas: 1
`;

export function StacksView() {
  const queryClient = useQueryClient();
  const toast = useToast();

  const [activeTab, setActiveTab] = useState<'stacks' | 'networks' | 'volumes'>('stacks');

  // Stacks State
  const [isCreateStackModalOpen, setIsCreateStackModalOpen] = useState(false);
  const [selectedStack, setSelectedStack] = useState<Stack | null>(null);
  const [inspectTab, setInspectTab] = useState<'overview' | 'yaml' | 'containers'>('overview');
  const [stackName, setStackName] = useState('');
  const [composeYAML, setComposeYAML] = useState(SAMPLE_COMPOSE);

  // Networks State
  const [isCreateNetModalOpen, setIsCreateNetModalOpen] = useState(false);
  const [netName, setNetName] = useState('');
  const [netDriver, setNetDriver] = useState('bridge');
  const [netScope, setNetScope] = useState('project');

  // Volumes State
  const [isCreateVolModalOpen, setIsCreateVolModalOpen] = useState(false);
  const [volName, setVolName] = useState('');
  const [volDriver, setVolDriver] = useState('local');

  // --- Queries ---
  const {
    data: stacks,
    isLoading: isStacksLoading,
    isError: isStacksError,
    error: stacksError,
    refetch: refetchStacks,
  } = useQuery({
    queryKey: ['stacks'],
    queryFn: api.stacks.list,
  });

  const {
    data: networks,
    isLoading: isNetworksLoading,
    isError: isNetworksError,
    error: networksError,
    refetch: refetchNetworks,
  } = useQuery({
    queryKey: ['networks'],
    queryFn: () => api.networks.list(),
  });

  const {
    data: volumes,
    isLoading: isVolumesLoading,
    isError: isVolumesError,
    error: volumesError,
    refetch: refetchVolumes,
  } = useQuery({
    queryKey: ['volumes'],
    queryFn: () => api.volumes.list(),
  });

  const safeStacks = stacks || [];
  const safeNetworks = networks || [];
  const safeVolumes = volumes || [];

  // --- Stack Mutations ---
  const createStackMutation = useMutation({
    mutationFn: (req: CreateStackRequest) => api.stacks.create(req),
    onSuccess: (newStack) => {
      queryClient.invalidateQueries({ queryKey: ['stacks'] });
      toast.success('Stack Created', `Stack ${newStack.name} created successfully`);
      setIsCreateStackModalOpen(false);
      setStackName('');
      setComposeYAML(SAMPLE_COMPOSE);
    },
    onError: (err: Error) => toast.error('Create Stack Failed', err.message),
  });

  const deployStackMutation = useMutation({
    mutationFn: (id: string) => api.stacks.deploy(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['stacks'] });
      toast.success('Stack Deployed', 'Stack deployment rollout initiated');
    },
    onError: (err: Error) => toast.error('Deploy Failed', err.message),
  });

  const restartStackMutation = useMutation({
    mutationFn: (id: string) => api.stacks.restart(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['stacks'] });
      toast.success('Stack Restarted', 'All stack service containers restarted');
    },
    onError: (err: Error) => toast.error('Restart Failed', err.message),
  });

  const stopStackMutation = useMutation({
    mutationFn: (id: string) => api.stacks.stop(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['stacks'] });
      toast.success('Stack Stopped', 'All stack service containers stopped');
    },
    onError: (err: Error) => toast.error('Stop Failed', err.message),
  });

  const deleteStackMutation = useMutation({
    mutationFn: (id: string) => api.stacks.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['stacks'] });
      toast.success('Stack Deleted', 'Stack services removed');
      if (selectedStack) setSelectedStack(null);
    },
    onError: (err: Error) => toast.error('Delete Failed', err.message),
  });

  // --- Network Mutations ---
  const createNetMutation = useMutation({
    mutationFn: (req: CreateNetworkRequest) => api.networks.create(req),
    onSuccess: (net) => {
      queryClient.invalidateQueries({ queryKey: ['networks'] });
      toast.success('Network Created', `Network ${net.name} created`);
      setIsCreateNetModalOpen(false);
      setNetName('');
    },
    onError: (err: Error) => toast.error('Create Network Failed', err.message),
  });

  const deleteNetMutation = useMutation({
    mutationFn: (id: string) => api.networks.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['networks'] });
      toast.success('Network Removed', 'Virtual network deleted');
    },
    onError: (err: Error) => toast.error('Delete Failed', err.message),
  });

  const pruneNetMutation = useMutation({
    mutationFn: () => api.networks.prune(),
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ['networks'] });
      toast.success('Networks Pruned', `Cleaned ${res.deleted.length} unused networks`);
    },
    onError: (err: Error) => toast.error('Prune Failed', err.message),
  });

  // --- Volume Mutations ---
  const createVolMutation = useMutation({
    mutationFn: (req: CreateVolumeRequest) => api.volumes.create(req),
    onSuccess: (vol) => {
      queryClient.invalidateQueries({ queryKey: ['volumes'] });
      toast.success('Volume Created', `Volume ${vol.name} created`);
      setIsCreateVolModalOpen(false);
      setVolName('');
    },
    onError: (err: Error) => toast.error('Create Volume Failed', err.message),
  });

  const deleteVolMutation = useMutation({
    mutationFn: (id: string) => api.volumes.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['volumes'] });
      toast.success('Volume Removed', 'Persistent volume deleted');
    },
    onError: (err: Error) => toast.error('Delete Failed', err.message),
  });

  const pruneVolMutation = useMutation({
    mutationFn: () => api.volumes.prune(),
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ['volumes'] });
      toast.success('Volumes Pruned', `Cleaned ${res.deleted.length} unused volumes`);
    },
    onError: (err: Error) => toast.error('Prune Failed', err.message),
  });

  return (
    <div className="space-y-6">
      {/* Top Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-zinc-100 flex items-center gap-2">
            <Layers className="h-5 w-5 text-cyan-400" />
            <span>Multi-Container Stacks & Infrastructure</span>
          </h1>
          <p className="text-xs text-zinc-400 mt-0.5">
            Docker Compose v2 deployments, dual-tier mesh networking, and persistent volume management
          </p>
        </div>

        <div className="flex items-center gap-2">
          {activeTab === 'stacks' && (
            <Button
              variant="primary"
              size="sm"
              onClick={() => setIsCreateStackModalOpen(true)}
              leftIcon={<Plus className="h-4 w-4" />}
            >
              New Stack
            </Button>
          )}
          {activeTab === 'networks' && (
            <>
              <Button
                variant="outline"
                size="sm"
                onClick={() => pruneNetMutation.mutate()}
                isLoading={pruneNetMutation.isPending}
                leftIcon={<RefreshCw className="h-3.5 w-3.5" />}
              >
                Prune Unused
              </Button>
              <Button
                variant="primary"
                size="sm"
                onClick={() => setIsCreateNetModalOpen(true)}
                leftIcon={<Plus className="h-4 w-4" />}
              >
                Create Network
              </Button>
            </>
          )}
          {activeTab === 'volumes' && (
            <>
              <Button
                variant="outline"
                size="sm"
                onClick={() => pruneVolMutation.mutate()}
                isLoading={pruneVolMutation.isPending}
                leftIcon={<RefreshCw className="h-3.5 w-3.5" />}
              >
                Prune Unused
              </Button>
              <Button
                variant="primary"
                size="sm"
                onClick={() => setIsCreateVolModalOpen(true)}
                leftIcon={<Plus className="h-4 w-4" />}
              >
                Create Volume
              </Button>
            </>
          )}
        </div>
      </div>

      {/* Main Navigation Tabs */}
      <Tabs
        variant="pills"
        activeTab={activeTab}
        onChange={(tabId) => setActiveTab(tabId as any)}
        tabs={[
          {
            id: 'stacks',
            label: 'Compose Stacks',
            icon: <Layers className="h-4 w-4" />,
            count: stacks?.length,
          },
          {
            id: 'networks',
            label: 'Virtual Networks',
            icon: <Network className="h-4 w-4" />,
            count: networks?.length,
          },
          {
            id: 'volumes',
            label: 'Storage Volumes',
            icon: <HardDrive className="h-4 w-4" />,
            count: volumes?.length,
          },
        ]}
      />

      {/* ========================================================================= */}
      {/* 1. STACKS VIEW */}
      {/* ========================================================================= */}
      {activeTab === 'stacks' && (
        <div>
          {isStacksError ? (
            <QueryErrorAlert
              title="Failed to load compose stacks"
              error={stacksError}
              onRetry={refetchStacks}
            />
          ) : isStacksLoading ? (
            <div className="text-center py-12 text-zinc-500 text-xs">Loading stacks...</div>
          ) : !stacks || stacks.length === 0 ? (
            <Card className="text-center py-12">
              <Layers className="h-8 w-8 text-zinc-600 mx-auto mb-2" />
              <h3 className="text-sm font-semibold text-zinc-200">No Compose Stacks Found</h3>
              <p className="text-xs text-zinc-500 mt-1 max-w-sm mx-auto">
                Define multi-container topologies using standard docker-compose.yml files with automatic dual networking.
              </p>
              <div className="mt-4">
                <Button variant="primary" size="sm" onClick={() => setIsCreateStackModalOpen(true)}>
                  Deploy First Stack
                </Button>
              </div>
            </Card>
          ) : (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
              {safeStacks.map((stack) => (
                <Card
                  key={stack.id}
                  className="flex flex-col justify-between hover:border-zinc-700 cursor-pointer transition-all"
                  onClick={() => {
                    setSelectedStack(stack);
                    setInspectTab('overview');
                  }}
                >
                  <div>
                    <div className="flex items-start justify-between">
                      <div className="flex items-center gap-2.5">
                        <div className="p-2 rounded-lg bg-zinc-800 text-cyan-400 border border-zinc-700/50">
                          <Layers className="h-4 w-4" />
                        </div>
                        <div>
                          <h3 className="text-sm font-bold text-zinc-100">{stack.name}</h3>
                          <span className="text-[11px] text-zinc-500 font-mono">
                            {formatDate(stack.created_at)}
                          </span>
                        </div>
                      </div>
                      <Badge
                        variant={
                          stack.status === 'running'
                            ? 'success'
                            : stack.status === 'deploying'
                            ? 'warning'
                            : stack.status === 'failed'
                            ? 'error'
                            : 'default'
                        }
                        dot
                      >
                        {stack.status}
                      </Badge>
                    </div>

                    <div className="mt-4">
                      <div className="flex items-center justify-between text-xs text-zinc-500 font-medium mb-1.5">
                        <span>Services:</span>
                        <span className="text-[11px] font-mono text-zinc-400">
                          {stack.services?.length || 0} defined
                        </span>
                      </div>
                      <div className="flex flex-wrap gap-1.5">
                        {stack.services && stack.services.length > 0 ? (
                          stack.services.map((s) => (
                            <span
                              key={s}
                              className="px-2 py-0.5 rounded bg-zinc-950 border border-zinc-800 text-zinc-300 font-mono text-[11px]"
                            >
                              {s}
                            </span>
                          ))
                        ) : (
                          <span className="text-xs text-zinc-600 font-mono">default_stack</span>
                        )}
                      </div>
                    </div>
                  </div>

                  <div
                    className="flex items-center justify-between pt-4 mt-4 border-t border-zinc-800/60"
                    onClick={(e) => e.stopPropagation()}
                  >
                    <div className="flex items-center gap-1">
                      <Button
                        variant="subtle"
                        size="sm"
                        onClick={() => deployStackMutation.mutate(stack.id)}
                        isLoading={deployStackMutation.isPending && deployStackMutation.variables === stack.id}
                        title="Deploy / Rollout"
                        leftIcon={<Play className="h-3 w-3" />}
                      >
                        Deploy
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => restartStackMutation.mutate(stack.id)}
                        isLoading={restartStackMutation.isPending && restartStackMutation.variables === stack.id}
                        title="Restart"
                      >
                        <RotateCw className="h-3.5 w-3.5 text-zinc-400" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => stopStackMutation.mutate(stack.id)}
                        isLoading={stopStackMutation.isPending && stopStackMutation.variables === stack.id}
                        title="Stop"
                      >
                        <Square className="h-3.5 w-3.5 text-amber-400" />
                      </Button>
                    </div>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => {
                        if (confirm(`Delete stack ${stack.name}? This will remove all associated containers.`)) {
                          deleteStackMutation.mutate(stack.id);
                        }
                      }}
                    >
                      <Trash2 className="h-3.5 w-3.5 text-rose-400" />
                    </Button>
                  </div>
                </Card>
              ))}
            </div>
          )}
        </div>
      )}

      {/* ========================================================================= */}
      {/* 2. NETWORKS VIEW */}
      {/* ========================================================================= */}
      {activeTab === 'networks' && (
        <div className="space-y-4">
          {isNetworksError ? (
            <QueryErrorAlert
              title="Failed to load virtual networks"
              error={networksError}
              onRetry={refetchNetworks}
            />
          ) : isNetworksLoading ? (
            <div className="text-center py-12 text-zinc-500 text-xs">Loading networks...</div>
          ) : !networks || networks.length === 0 ? (
            <Card className="text-center py-12">
              <Network className="h-8 w-8 text-zinc-600 mx-auto mb-2" />
              <h3 className="text-sm font-semibold text-zinc-200">No Managed Virtual Networks</h3>
              <p className="text-xs text-zinc-500 mt-1 max-w-sm mx-auto">
                Virtual networks isolate application containers and enable inter-service discovery across projects.
              </p>
              <div className="mt-4">
                <Button variant="primary" size="sm" onClick={() => setIsCreateNetModalOpen(true)}>
                  Create Network
                </Button>
              </div>
            </Card>
          ) : (
            <Card className="p-0 overflow-hidden">
              <div className="overflow-x-auto">
                <table className="w-full text-left text-xs">
                  <thead className="bg-zinc-900/60 border-b border-zinc-800 text-zinc-400 uppercase text-[10px] tracking-wider">
                    <tr>
                      <th className="py-3 px-4">Network Name</th>
                      <th className="py-3 px-4">Scope</th>
                      <th className="py-3 px-4">Driver</th>
                      <th className="py-3 px-4">Project</th>
                      <th className="py-3 px-4">Created</th>
                      <th className="py-3 px-4 text-right">Actions</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-zinc-800/60 font-mono">
                    {safeNetworks.map((net) => (
                      <tr key={net.id} className="hover:bg-zinc-900/30 transition-colors">
                        <td className="py-3 px-4 font-semibold text-zinc-200 flex items-center gap-2">
                          <Network className="h-3.5 w-3.5 text-cyan-400" />
                          <span>{net.name}</span>
                        </td>
                        <td className="py-3 px-4">
                          <Badge variant={net.scope === 'project' ? 'info' : 'default'}>
                            {net.scope}
                          </Badge>
                        </td>
                        <td className="py-3 px-4 text-zinc-400">{net.driver}</td>
                        <td className="py-3 px-4 text-zinc-400">{net.project_id || 'prj_default'}</td>
                        <td className="py-3 px-4 text-zinc-500 text-[11px]">
                          {formatDate(net.created_at)}
                        </td>
                        <td className="py-3 px-4 text-right">
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => {
                              if (confirm(`Remove network ${net.name}?`)) {
                                deleteNetMutation.mutate(net.id);
                              }
                            }}
                          >
                            <Trash2 className="h-3.5 w-3.5 text-rose-400" />
                          </Button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </Card>
          )}
        </div>
      )}

      {/* ========================================================================= */}
      {/* 3. VOLUMES VIEW */}
      {/* ========================================================================= */}
      {activeTab === 'volumes' && (
        <div className="space-y-4">
          {isVolumesError ? (
            <QueryErrorAlert
              title="Failed to load storage volumes"
              error={volumesError}
              onRetry={refetchVolumes}
            />
          ) : isVolumesLoading ? (
            <div className="text-center py-12 text-zinc-500 text-xs">Loading volumes...</div>
          ) : !volumes || volumes.length === 0 ? (
            <Card className="text-center py-12">
              <HardDrive className="h-8 w-8 text-zinc-600 mx-auto mb-2" />
              <h3 className="text-sm font-semibold text-zinc-200">No Persistent Storage Volumes</h3>
              <p className="text-xs text-zinc-500 mt-1 max-w-sm mx-auto">
                Persistent volumes preserve stateful container database files and application uploads across restarts.
              </p>
              <div className="mt-4">
                <Button variant="primary" size="sm" onClick={() => setIsCreateVolModalOpen(true)}>
                  Create Volume
                </Button>
              </div>
            </Card>
          ) : (
            <Card className="p-0 overflow-hidden">
              <div className="overflow-x-auto">
                <table className="w-full text-left text-xs">
                  <thead className="bg-zinc-900/60 border-b border-zinc-800 text-zinc-400 uppercase text-[10px] tracking-wider">
                    <tr>
                      <th className="py-3 px-4">Volume Name</th>
                      <th className="py-3 px-4">Driver</th>
                      <th className="py-3 px-4">Project</th>
                      <th className="py-3 px-4">Created</th>
                      <th className="py-3 px-4 text-right">Actions</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-zinc-800/60 font-mono">
                    {safeVolumes.map((vol) => (
                      <tr key={vol.id} className="hover:bg-zinc-900/30 transition-colors">
                        <td className="py-3 px-4 font-semibold text-zinc-200 flex items-center gap-2">
                          <HardDrive className="h-3.5 w-3.5 text-amber-400" />
                          <span>{vol.name}</span>
                        </td>
                        <td className="py-3 px-4 text-zinc-400">{vol.driver}</td>
                        <td className="py-3 px-4 text-zinc-400">{vol.project_id || 'prj_default'}</td>
                        <td className="py-3 px-4 text-zinc-500 text-[11px]">
                          {formatDate(vol.created_at)}
                        </td>
                        <td className="py-3 px-4 text-right">
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => {
                              if (confirm(`Delete volume ${vol.name}? Data may be permanently lost.`)) {
                                deleteVolMutation.mutate(vol.id);
                              }
                            }}
                          >
                            <Trash2 className="h-3.5 w-3.5 text-rose-400" />
                          </Button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </Card>
          )}
        </div>
      )}

      {/* ========================================================================= */}
      {/* STACK INSPECTOR & TOPOLOGY MODAL */}
      {/* ========================================================================= */}
      {selectedStack && (
        <Modal
          isOpen={Boolean(selectedStack)}
          onClose={() => setSelectedStack(null)}
          title={
            <div className="flex items-center gap-2">
              <Layers className="h-5 w-5 text-cyan-400" />
              <span>{selectedStack.name} — Stack Topology & Blueprint</span>
            </div>
          }
          size="lg"
        >
          <div className="space-y-4">
            {/* Modal Sub-Tabs */}
            <div className="flex border-b border-zinc-800 gap-4 text-xs font-medium">
              <button
                className={`pb-2 transition-colors flex items-center gap-1.5 ${
                  inspectTab === 'overview'
                    ? 'border-b-2 border-cyan-400 text-cyan-400 font-semibold'
                    : 'text-zinc-400 hover:text-zinc-200'
                }`}
                onClick={() => setInspectTab('overview')}
              >
                <Boxes className="h-3.5 w-3.5" />
                Service Topology
              </button>
              <button
                className={`pb-2 transition-colors flex items-center gap-1.5 ${
                  inspectTab === 'containers'
                    ? 'border-b-2 border-cyan-400 text-cyan-400 font-semibold'
                    : 'text-zinc-400 hover:text-zinc-200'
                }`}
                onClick={() => setInspectTab('containers')}
              >
                <Activity className="h-3.5 w-3.5" />
                Containers ({selectedStack.containers?.length || selectedStack.services?.length || 0})
              </button>
              <button
                className={`pb-2 transition-colors flex items-center gap-1.5 ${
                  inspectTab === 'yaml'
                    ? 'border-b-2 border-cyan-400 text-cyan-400 font-semibold'
                    : 'text-zinc-400 hover:text-zinc-200'
                }`}
                onClick={() => setInspectTab('yaml')}
              >
                <Code2 className="h-3.5 w-3.5" />
                Compose YAML
              </button>
            </div>

            {/* Tab: Topology Overview */}
            {inspectTab === 'overview' && (
              <div className="space-y-4">
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                  {selectedStack.services && selectedStack.services.length > 0 ? (
                    selectedStack.services.map((svc) => (
                      <div
                        key={svc}
                        className="p-3 rounded-lg bg-zinc-950 border border-zinc-800 flex items-start gap-3"
                      >
                        <div className="p-2 rounded bg-zinc-900 text-cyan-400 border border-zinc-800">
                          <Server className="h-4 w-4" />
                        </div>
                        <div className="flex-1 min-w-0">
                          <div className="flex items-center justify-between">
                            <h4 className="text-xs font-bold text-zinc-100 font-mono">{svc}</h4>
                            <Badge variant="success">
                              Healthy
                            </Badge>
                          </div>
                          <p className="text-[11px] text-zinc-400 mt-1 font-mono">
                            Dual-net: pikpik_net_stack_{selectedStack.name}
                          </p>
                        </div>
                      </div>
                    ))
                  ) : (
                    <div className="text-xs text-zinc-500 italic">No services detected in blueprint.</div>
                  )}
                </div>

                <div className="p-3 rounded-lg bg-zinc-900/50 border border-zinc-800/80 text-xs text-zinc-300 space-y-1.5">
                  <div className="font-semibold text-zinc-200 flex items-center gap-1.5">
                    <CheckCircle className="h-3.5 w-3.5 text-emerald-400" />
                    Dual-Tier Network Isolation Active
                  </div>
                  <p className="text-zinc-400 text-[11px]">
                    Internal traffic travels over private bridge network{' '}
                    <code className="text-cyan-400 font-mono">pikpik_net_stack_{selectedStack.name}</code>,
                    with optional project-wide mesh interconnect.
                  </p>
                </div>
              </div>
            )}

            {/* Tab: Containers */}
            {inspectTab === 'containers' && (
              <div className="space-y-3">
                {selectedStack.containers && selectedStack.containers.length > 0 ? (
                  <div className="overflow-x-auto">
                    <table className="w-full text-left text-xs font-mono">
                      <thead className="bg-zinc-950 border-b border-zinc-800 text-zinc-500 uppercase text-[10px]">
                        <tr>
                          <th className="py-2 px-3">Container</th>
                          <th className="py-2 px-3">Service</th>
                          <th className="py-2 px-3">State</th>
                          <th className="py-2 px-3">Status</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-zinc-900">
                        {selectedStack.containers.map((c) => (
                          <tr key={c.id}>
                            <td className="py-2 px-3 text-zinc-200">{c.name || c.id.substring(0, 12)}</td>
                            <td className="py-2 px-3 text-cyan-400">{c.service}</td>
                            <td className="py-2 px-3">
                              <Badge variant={c.state === 'running' ? 'success' : 'warning'}>
                                {c.state}
                              </Badge>
                            </td>
                            <td className="py-2 px-3 text-zinc-400 text-[11px]">{c.status}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                ) : (
                  <div className="p-4 rounded-lg bg-zinc-950 border border-zinc-800 text-center text-xs text-zinc-400">
                    <AlertCircle className="h-5 w-5 text-zinc-500 mx-auto mb-1.5" />
                    No live containers currently attached. Click "Deploy Rollout" to instantiate services.
                  </div>
                )}
              </div>
            )}

            {/* Tab: Compose YAML */}
            {inspectTab === 'yaml' && (
              <div className="space-y-3">
                <pre className="p-4 rounded-lg bg-zinc-950 border border-zinc-800 font-mono text-xs text-zinc-300 leading-relaxed overflow-x-auto max-h-96">
                  {selectedStack.compose_yaml}
                </pre>
              </div>
            )}

            {/* Modal Footer Actions */}
            <div className="flex items-center justify-between pt-3 border-t border-zinc-800">
              <div className="flex items-center gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => restartStackMutation.mutate(selectedStack.id)}
                  isLoading={restartStackMutation.isPending}
                  leftIcon={<RotateCw className="h-3.5 w-3.5" />}
                >
                  Restart
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => stopStackMutation.mutate(selectedStack.id)}
                  isLoading={stopStackMutation.isPending}
                  leftIcon={<Square className="h-3.5 w-3.5 text-amber-400" />}
                >
                  Stop
                </Button>
              </div>

              <div className="flex items-center gap-2">
                <Button
                  variant="primary"
                  size="sm"
                  onClick={() => {
                    deployStackMutation.mutate(selectedStack.id);
                  }}
                  isLoading={deployStackMutation.isPending}
                  leftIcon={<Play className="h-3.5 w-3.5" />}
                >
                  Deploy Rollout
                </Button>
              </div>
            </div>
          </div>
        </Modal>
      )}

      {/* ========================================================================= */}
      {/* MODAL: CREATE STACK */}
      {/* ========================================================================= */}
      <Modal
        isOpen={isCreateStackModalOpen}
        onClose={() => setIsCreateStackModalOpen(false)}
        title="Create Docker Compose Stack"
        description="Deploy multi-service YAML directly to Swarm with isolated dual networking"
        size="lg"
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            createStackMutation.mutate({ name: stackName, compose_yaml: composeYAML });
          }}
          className="space-y-4"
        >
          <Input
            label="Stack Name"
            placeholder="e.g. analytics-stack"
            value={stackName}
            onChange={(e) => setStackName(e.target.value)}
            required
          />

          <Textarea
            label="docker-compose.yml"
            rows={12}
            value={composeYAML}
            onChange={(e) => setComposeYAML(e.target.value)}
            required
          />

          <div className="flex justify-end gap-2 pt-4 border-t border-zinc-800">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setIsCreateStackModalOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              variant="primary"
              size="sm"
              isLoading={createStackMutation.isPending}
            >
              Create Stack
            </Button>
          </div>
        </form>
      </Modal>

      {/* ========================================================================= */}
      {/* MODAL: CREATE NETWORK */}
      {/* ========================================================================= */}
      <Modal
        isOpen={isCreateNetModalOpen}
        onClose={() => setIsCreateNetModalOpen(false)}
        title="Create Managed Virtual Network"
        description="Configure bridge or overlay virtual networking for multi-container services"
        size="md"
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            createNetMutation.mutate({
              name: netName,
              driver: netDriver,
              scope: netScope,
            });
          }}
          className="space-y-4"
        >
          <Input
            label="Network Name"
            placeholder="e.g. billing_mesh"
            value={netName}
            onChange={(e) => setNetName(e.target.value)}
            required
          />

          <div>
            <label className="block text-xs font-medium text-zinc-300 mb-1.5">Driver</label>
            <select
              className="w-full bg-zinc-950 border border-zinc-800 rounded-md px-3 py-2 text-xs text-zinc-100 focus:outline-none focus:border-cyan-500"
              value={netDriver}
              onChange={(e) => setNetDriver(e.target.value)}
            >
              <option value="bridge">bridge (Single Host / Local)</option>
              <option value="overlay">overlay (Multi-Node Swarm Mesh)</option>
            </select>
          </div>

          <div>
            <label className="block text-xs font-medium text-zinc-300 mb-1.5">Scope</label>
            <select
              className="w-full bg-zinc-950 border border-zinc-800 rounded-md px-3 py-2 text-xs text-zinc-100 focus:outline-none focus:border-cyan-500"
              value={netScope}
              onChange={(e) => setNetScope(e.target.value)}
            >
              <option value="project">project (Inter-Service Project Mesh)</option>
              <option value="custom">custom (Isolated)</option>
            </select>
          </div>

          <div className="flex justify-end gap-2 pt-4 border-t border-zinc-800">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setIsCreateNetModalOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              variant="primary"
              size="sm"
              isLoading={createNetMutation.isPending}
            >
              Create Network
            </Button>
          </div>
        </form>
      </Modal>

      {/* ========================================================================= */}
      {/* MODAL: CREATE VOLUME */}
      {/* ========================================================================= */}
      <Modal
        isOpen={isCreateVolModalOpen}
        onClose={() => setIsCreateVolModalOpen(false)}
        title="Create Persistent Storage Volume"
        description="Allocate named local storage volume scoped for database or app persistence"
        size="md"
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            createVolMutation.mutate({
              name: volName,
              driver: volDriver,
            });
          }}
          className="space-y-4"
        >
          <Input
            label="Volume Name"
            placeholder="e.g. pgdata_persistent"
            value={volName}
            onChange={(e) => setVolName(e.target.value)}
            required
          />

          <div>
            <label className="block text-xs font-medium text-zinc-300 mb-1.5">Driver</label>
            <select
              className="w-full bg-zinc-950 border border-zinc-800 rounded-md px-3 py-2 text-xs text-zinc-100 focus:outline-none focus:border-cyan-500"
              value={volDriver}
              onChange={(e) => setVolDriver(e.target.value)}
            >
              <option value="local">local (Standard Docker volume)</option>
            </select>
          </div>

          <div className="flex justify-end gap-2 pt-4 border-t border-zinc-800">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setIsCreateVolModalOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              variant="primary"
              size="sm"
              isLoading={createVolMutation.isPending}
            >
              Create Volume
            </Button>
          </div>
        </form>
      </Modal>
    </div>
  );
}
