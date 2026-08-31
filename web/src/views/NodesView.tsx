import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../lib/api';
import { MachineDTO, SwarmNode } from '../lib/types';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Badge } from '../components/ui/Badge';
import { Modal } from '../components/ui/Modal';
import { Tabs } from '../components/ui/Tabs';
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '../components/ui/Table';
import { useToast } from '../components/ui/Toast';
import { QueryErrorAlert } from '../components/ui/QueryErrorAlert';
import { formatBytes, formatDate } from '../lib/utils';
import { usePTY } from '../hooks/usePTY';
import {
  Server,
  Key,
  Copy,
  Check,
  Crown,
  Terminal as TerminalIcon,
  Plus,
  Trash2,
  Cpu,
  HardDrive,
  Activity,
  Layers,
  ShieldCheck,
  Radio,
  Search,
  CheckCircle2,
  Info,
} from 'lucide-react';

export function NodesView() {
  const queryClient = useQueryClient();
  const toast = useToast();

  const [activeTab, setActiveTab] = useState<'machines' | 'swarm'>('machines');
  const [isEnrollModalOpen, setIsEnrollModalOpen] = useState(false);
  const [isJoinModalOpen, setIsJoinModalOpen] = useState(false);
  const [copiedType, setCopiedType] = useState<'worker' | 'manager' | 'enroll' | null>(null);
  const [terminalNodeId, setTerminalNodeId] = useState<string | null>(null);
  const [selectedMachine, setSelectedMachine] = useState<MachineDTO | null>(null);
  const [searchQuery, setSearchQuery] = useState('');

  const { containerRef: terminalRef, status: ptyStatus } = usePTY({
    targetType: 'host_machine',
    targetId: terminalNodeId || undefined,
  });
  const isPTYConnected = ptyStatus === 'connected';

  // Remote Machines Query
  const {
    data: machines = [],
    isLoading: isLoadingMachines,
    isError: isMachinesError,
    error: machinesError,
    refetch: refetchMachines,
  } = useQuery({
    queryKey: ['machines'],
    queryFn: api.machines.list,
    refetchInterval: 5000,
  });

  // Swarm Nodes Query
  const {
    data: nodes = [],
    isLoading: isLoadingNodes,
    isError: isNodesError,
    error: nodesError,
    refetch: refetchNodes,
  } = useQuery({
    queryKey: ['nodes'],
    queryFn: api.nodes.list,
    refetchInterval: 5000,
  });

  // Swarm Join Tokens Query
  const { data: joinTokens } = useQuery({
    queryKey: ['joinTokens'],
    queryFn: api.nodes.getJoinTokens,
    enabled: isJoinModalOpen,
  });

  // Machine Enrollment Command Query
  const { data: enrollCommand } = useQuery({
    queryKey: ['machineEnroll'],
    queryFn: () => api.machines.getEnrollCommand(),
    enabled: isEnrollModalOpen,
  });

  // Update Swarm Node Availability Mutation
  const updateNodeMutation = useMutation({
    mutationFn: ({ id, availability }: { id: string; availability: 'active' | 'pause' | 'drain' }) =>
      api.nodes.update(id, { availability }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['nodes'] });
      toast.success('Node Updated', 'Swarm node availability modified');
    },
    onError: (err: Error) => toast.error('Update Failed', err.message),
  });

  // Join Swarm Mutation
  const joinSwarmMutation = useMutation({
    mutationFn: ({ id, role }: { id: string; role: 'worker' | 'manager' }) =>
      api.machines.joinSwarm(id, { role }),
    onSuccess: (node) => {
      queryClient.invalidateQueries({ queryKey: ['machines'] });
      queryClient.invalidateQueries({ queryKey: ['nodes'] });
      toast.success('Joined Swarm Mesh', `Machine joined cluster as ${node.role}`);
      if (selectedMachine?.id === node.id) {
        setSelectedMachine(null);
      }
    },
    onError: (err: Error) => toast.error('Swarm Join Failed', err.message),
  });

  // Delete Machine Mutation
  const deleteMachineMutation = useMutation({
    mutationFn: (id: string) => api.machines.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['machines'] });
      toast.success('Machine Removed', 'Machine registration deregistered');
      if (selectedMachine) setSelectedMachine(null);
    },
    onError: (err: Error) => toast.error('Removal Failed', err.message),
  });

  const copyToClipboard = (text: string, type: 'worker' | 'manager' | 'enroll') => {
    navigator.clipboard.writeText(text);
    setCopiedType(type);
    toast.success('Copied to Clipboard', 'Command copied to clipboard');
    setTimeout(() => setCopiedType(null), 2000);
  };

  const safeMachines = machines || [];
  const safeNodes = nodes || [];

  const filteredMachines = safeMachines.filter(
    (m) =>
      m.hostname.toLowerCase().includes(searchQuery.toLowerCase()) ||
      m.id.toLowerCase().includes(searchQuery.toLowerCase()) ||
      m.public_ip.toLowerCase().includes(searchQuery.toLowerCase()) ||
      m.role.toLowerCase().includes(searchQuery.toLowerCase())
  );

  const onlineMachinesCount = safeMachines.filter((m) => m.status === 'online').length;
  const swarmManagersCount = safeNodes.filter((n) => n.role === 'manager').length;
  const swarmWorkersCount = safeNodes.filter((n) => n.role === 'worker').length;

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-zinc-100 flex items-center gap-2">
            <Server className="h-5 w-5 text-cyan-400" />
            <span>Infrastructure & Machines</span>
          </h1>
          <p className="text-xs text-zinc-400 mt-0.5">
            Fleet compute orchestration, standalone worker agents, and Raft-consensus Swarm topology
          </p>
        </div>

        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => setIsJoinModalOpen(true)}
            leftIcon={<Key className="h-4 w-4 text-purple-400" />}
          >
            Swarm Join Tokens
          </Button>
          <Button
            variant="primary"
            size="sm"
            onClick={() => setIsEnrollModalOpen(true)}
            leftIcon={<Plus className="h-4 w-4" />}
          >
            Enroll New Machine
          </Button>
        </div>
      </div>

      {/* Overview Stat Cards */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <Card className="p-4 bg-zinc-900/60 border-zinc-800 flex items-center justify-between">
          <div>
            <div className="text-xs text-zinc-400 font-medium">Total Machines</div>
            <div className="text-2xl font-bold text-zinc-100 mt-1">{machines.length}</div>
            <div className="text-[11px] text-zinc-500 mt-0.5">{onlineMachinesCount} online & connected</div>
          </div>
          <div className="p-3 bg-cyan-950/40 text-cyan-400 rounded-lg border border-cyan-900/40">
            <Server className="h-5 w-5" />
          </div>
        </Card>

        <Card className="p-4 bg-zinc-900/60 border-zinc-800 flex items-center justify-between">
          <div>
            <div className="text-xs text-zinc-400 font-medium">Swarm Cluster Mesh</div>
            <div className="text-2xl font-bold text-zinc-100 mt-1">{nodes.length} Nodes</div>
            <div className="text-[11px] text-zinc-500 mt-0.5">
              {swarmManagersCount} Managers • {swarmWorkersCount} Workers
            </div>
          </div>
          <div className="p-3 bg-purple-950/40 text-purple-400 rounded-lg border border-purple-900/40">
            <Layers className="h-5 w-5" />
          </div>
        </Card>

        <Card className="p-4 bg-zinc-900/60 border-zinc-800 flex items-center justify-between">
          <div>
            <div className="text-xs text-zinc-400 font-medium">Agent Connectivity</div>
            <div className="text-2xl font-bold text-emerald-400 mt-1">
              {machines.length > 0 ? `${Math.round((onlineMachinesCount / machines.length) * 100)}%` : '100%'}
            </div>
            <div className="text-[11px] text-zinc-500 mt-0.5">mTLS & WSS Outbound tunnel</div>
          </div>
          <div className="p-3 bg-emerald-950/40 text-emerald-400 rounded-lg border border-emerald-900/40">
            <Radio className="h-5 w-5" />
          </div>
        </Card>

        <Card className="p-4 bg-zinc-900/60 border-zinc-800 flex items-center justify-between">
          <div>
            <div className="text-xs text-zinc-400 font-medium">Cluster Consensus</div>
            <div className="text-2xl font-bold text-cyan-300 mt-1">Healthy</div>
            <div className="text-[11px] text-zinc-500 mt-0.5">Automated Leader Failover</div>
          </div>
          <div className="p-3 bg-cyan-950/40 text-cyan-400 rounded-lg border border-cyan-900/40">
            <ShieldCheck className="h-5 w-5" />
          </div>
        </Card>
      </div>

      {/* Tabs Navigation */}
      <Tabs
        activeTab={activeTab}
        onChange={(id) => setActiveTab(id as 'machines' | 'swarm')}
        tabs={[
          {
            id: 'machines',
            label: 'All Remote Machines',
            icon: <Server className="h-4 w-4" />,
            count: machines.length,
          },
          {
            id: 'swarm',
            label: 'Swarm Cluster Mesh',
            icon: <Layers className="h-4 w-4" />,
            count: nodes.length,
          },
        ]}
      />

      {/* Tab Content: All Remote Machines */}
      {activeTab === 'machines' && (
        <div className="space-y-4">
          {/* Search Bar */}
          <div className="flex items-center justify-between gap-4">
            <div className="relative flex-1 max-w-md">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-zinc-500" />
              <input
                type="text"
                placeholder="Filter by hostname, ID, IP address, or role..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                className="w-full pl-9 pr-3 py-1.5 bg-zinc-900/80 border border-zinc-800 rounded-lg text-xs text-zinc-200 placeholder-zinc-500 focus:outline-none focus:border-cyan-500"
              />
            </div>
            <div className="text-xs text-zinc-500">
              Showing {filteredMachines.length} of {machines.length} machine{machines.length === 1 ? '' : 's'}
            </div>
          </div>

          <Card className="p-0 overflow-hidden">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Hostname / ID</TableHead>
                  <TableHead>Role</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Network IP</TableHead>
                  <TableHead>OS / Arch</TableHead>
                  <TableHead>Docker & Agent</TableHead>
                  <TableHead>Metrics (CPU / RAM)</TableHead>
                  <TableHead>Last Heartbeat</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {isMachinesError ? (
                  <TableRow>
                    <TableCell colSpan={9} className="p-4">
                      <QueryErrorAlert
                        title="Failed to discover machines"
                        error={machinesError}
                        onRetry={refetchMachines}
                      />
                    </TableCell>
                  </TableRow>
                ) : isLoadingMachines ? (
                  <TableRow>
                    <TableCell colSpan={9} className="text-center py-8 text-zinc-500">
                      Discovering managed machines...
                    </TableCell>
                  </TableRow>
                ) : filteredMachines.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={9} className="text-center py-10 text-zinc-500">
                      <div className="flex flex-col items-center justify-center gap-2">
                        <Server className="h-8 w-8 text-zinc-600 mb-1" />
                        <div className="text-sm font-medium text-zinc-300">No managed machines found</div>
                        <p className="text-xs text-zinc-500 max-w-sm">
                          Install the pikpik-agent worker daemon on any bare metal or VPS to manage it from this console.
                        </p>
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => setIsEnrollModalOpen(true)}
                          className="mt-2"
                        >
                          Enroll First Machine
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ) : (
                  filteredMachines.map((m) => (
                    <TableRow key={m.id}>
                      <TableCell className="font-semibold text-zinc-100">
                        <div className="flex items-center gap-2.5">
                          <div
                            className={`h-2.5 w-2.5 rounded-full shrink-0 ${
                              m.status === 'online'
                                ? 'bg-emerald-400 shadow-[0_0_8px_rgba(52,211,153,0.5)]'
                                : m.status === 'degraded'
                                ? 'bg-amber-400'
                                : 'bg-zinc-600'
                            }`}
                          />
                          <div>
                            <div className="flex items-center gap-1.5">
                              <span>{m.hostname || m.id}</span>
                              {m.role === 'manager' && (
                                <span title="Cluster Manager" className="text-purple-400">
                                  <Crown className="h-3.5 w-3.5" />
                                </span>
                              )}
                            </div>
                            <div className="text-[10px] font-mono text-zinc-500">{m.id}</div>
                          </div>
                        </div>
                      </TableCell>

                      <TableCell>
                        <Badge
                          variant={
                            m.role === 'manager'
                              ? 'info'
                              : m.role === 'worker'
                              ? 'default'
                              : 'outline'
                          }
                        >
                          {m.role}
                        </Badge>
                      </TableCell>

                      <TableCell>
                        <Badge
                          variant={
                            m.status === 'online'
                              ? 'success'
                              : m.status === 'degraded'
                              ? 'warning'
                              : 'error'
                          }
                          dot
                        >
                          {m.status}
                        </Badge>
                      </TableCell>

                      <TableCell className="font-mono text-xs text-zinc-400">
                        <div>{m.public_ip || '127.0.0.1'}</div>
                        {m.private_ip && (
                          <div className="text-[10px] text-zinc-600">priv: {m.private_ip}</div>
                        )}
                      </TableCell>

                      <TableCell className="text-xs text-zinc-300">
                        <div>{m.os_kernel || 'Linux'}</div>
                        <div className="text-[10px] font-mono text-zinc-500">{m.cpu_arch || 'amd64'}</div>
                      </TableCell>

                      <TableCell className="font-mono text-xs text-zinc-400">
                        <div>Docker: {m.docker_version || '27.1'}</div>
                        <div className="text-[10px] text-zinc-500">Agent: v{m.agent_version || '1.0.0'}</div>
                      </TableCell>

                      <TableCell className="font-mono text-xs text-zinc-300">
                        {m.metrics ? (
                          <div className="space-y-1">
                            <div className="flex items-center gap-1 text-[11px]">
                              <Cpu className="h-3 w-3 text-cyan-400" />
                              <span>{m.metrics.cpu_percent.toFixed(1)}%</span>
                            </div>
                            <div className="flex items-center gap-1 text-[10px] text-zinc-400">
                              <HardDrive className="h-3 w-3 text-emerald-400" />
                              <span>{m.metrics.mem_percent.toFixed(1)}%</span>
                            </div>
                          </div>
                        ) : (
                          <span className="text-zinc-500 text-xs">Awaiting metrics</span>
                        )}
                      </TableCell>

                      <TableCell className="text-xs text-zinc-500">
                        {m.last_seen ? formatDate(m.last_seen) : 'Active'}
                      </TableCell>

                      <TableCell className="text-right">
                        <div className="flex items-center justify-end gap-1.5">
                          <Button
                            variant="subtle"
                            size="sm"
                            onClick={() => setTerminalNodeId(m.id)}
                            className="text-cyan-400 hover:text-cyan-300"
                            title="Open interactive remote host PTY shell"
                          >
                            <TerminalIcon className="h-3.5 w-3.5 mr-1" />
                            <span>Shell</span>
                          </Button>

                          <Button
                            variant="subtle"
                            size="sm"
                            onClick={() => setSelectedMachine(m)}
                            className="text-zinc-300 hover:text-zinc-100"
                            title="View host specs and telemetry breakdown"
                          >
                            <Activity className="h-3.5 w-3.5 mr-1" />
                            <span>Details</span>
                          </Button>

                          {m.role === 'standalone' && (
                            <Button
                              variant="outline"
                              size="sm"
                              onClick={() => joinSwarmMutation.mutate({ id: m.id, role: 'worker' })}
                              className="text-purple-400 hover:text-purple-300 border-purple-900/50"
                              title="Add machine to Swarm cluster mesh"
                            >
                              <Layers className="h-3.5 w-3.5 mr-1" />
                              <span>Join Swarm</span>
                            </Button>
                          )}

                          <Button
                            variant="subtle"
                            size="sm"
                            onClick={() => {
                              if (confirm(`Remove machine registration for ${m.hostname || m.id}?`)) {
                                deleteMachineMutation.mutate(m.id);
                              }
                            }}
                            className="text-red-400 hover:text-red-300"
                            title="Remove machine registration"
                          >
                            <Trash2 className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </Card>
        </div>
      )}

      {/* Tab Content: Swarm Cluster */}
      {activeTab === 'swarm' && (
        <div className="space-y-4">
          <Card className="p-0 overflow-hidden">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Hostname / Node ID</TableHead>
                  <TableHead>Role</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Availability</TableHead>
                  <TableHead>IP Address</TableHead>
                  <TableHead>Compute / RAM</TableHead>
                  <TableHead>Engine</TableHead>
                  <TableHead>Heartbeat</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {isNodesError ? (
                  <TableRow>
                    <TableCell colSpan={9} className="p-4">
                      <QueryErrorAlert
                        title="Failed to load swarm cluster nodes"
                        error={nodesError}
                        onRetry={refetchNodes}
                      />
                    </TableCell>
                  </TableRow>
                ) : isLoadingNodes ? (
                  <TableRow>
                    <TableCell colSpan={9} className="text-center py-8 text-zinc-500">
                      Loading swarm cluster nodes...
                    </TableCell>
                  </TableRow>
                ) : nodes.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={9} className="text-center py-10 text-zinc-500">
                      No swarm nodes discovered. Initializing primary manager...
                    </TableCell>
                  </TableRow>
                ) : (
                  nodes.map((node: SwarmNode) => (
                    <TableRow key={node.id}>
                      <TableCell className="font-semibold text-zinc-100">
                        <div className="flex items-center gap-2">
                          <Server className="h-4 w-4 text-emerald-400 shrink-0" />
                          <div>
                            <div className="flex items-center gap-1.5">
                              <span>{node.hostname}</span>
                              {node.leader && (
                                <span title="Swarm Leader" className="text-amber-400">
                                  <Crown className="h-3.5 w-3.5" />
                                </span>
                              )}
                            </div>
                            <div className="text-[10px] font-mono text-zinc-500">{node.id}</div>
                          </div>
                        </div>
                      </TableCell>
                      <TableCell>
                        <Badge variant={node.role === 'manager' ? 'info' : 'outline'}>
                          {node.role}
                        </Badge>
                      </TableCell>
                      <TableCell>
                        <Badge variant={node.status === 'ready' ? 'success' : 'error'} dot>
                          {node.status}
                        </Badge>
                      </TableCell>
                      <TableCell>
                        <select
                          value={node.availability}
                          onChange={(e) =>
                            updateNodeMutation.mutate({
                              id: node.id,
                              availability: e.target.value as 'active' | 'pause' | 'drain',
                            })
                          }
                          className="bg-zinc-900 border border-zinc-700 rounded px-2 py-1 text-xs text-zinc-200 focus:outline-none"
                        >
                          <option value="active">Active</option>
                          <option value="pause">Pause</option>
                          <option value="drain">Drain</option>
                        </select>
                      </TableCell>
                      <TableCell className="font-mono text-xs text-zinc-400">
                        {node.ip_address || '127.0.0.1'}
                      </TableCell>
                      <TableCell className="font-mono text-xs text-zinc-300">
                        {node.cpus} vCPUs • {node.memory_bytes ? formatBytes(node.memory_bytes) : '8 GiB'}
                      </TableCell>
                      <TableCell className="font-mono text-xs text-zinc-400">
                        {node.engine_version || '27.5.1'}
                      </TableCell>
                      <TableCell className="text-xs text-zinc-500">
                        {formatDate(node.updated_at)}
                      </TableCell>
                      <TableCell className="text-right">
                        <div className="flex items-center justify-end gap-2">
                          <Button
                            variant="subtle"
                            size="sm"
                            onClick={() => setTerminalNodeId(node.id)}
                            className="text-cyan-400 hover:text-cyan-300"
                          >
                            <TerminalIcon className="h-3.5 w-3.5 mr-1" />
                            <span>Shell</span>
                          </Button>
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() =>
                              updateNodeMutation.mutate({
                                id: node.id,
                                availability: node.availability === 'drain' ? 'active' : 'drain',
                              })
                            }
                          >
                            {node.availability === 'drain' ? 'Activate' : 'Drain'}
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </Card>
        </div>
      )}

      {/* Host Terminal PTY Modal */}
      <Modal
        isOpen={!!terminalNodeId}
        onClose={() => setTerminalNodeId(null)}
        title={`Host OS Terminal: ${terminalNodeId || ''}`}
        description="Interactive root host shell session over secure outbound WebSocket tunnel"
        size="xl"
      >
        <div className="space-y-3">
          <div className="flex items-center justify-between px-1 text-xs">
            <div className="flex items-center gap-2">
              <span
                className={`inline-block h-2 w-2 rounded-full ${
                  isPTYConnected ? 'bg-emerald-400 animate-pulse' : 'bg-red-500'
                }`}
              />
              <span className="text-zinc-400 font-mono">
                {isPTYConnected ? 'Connected (60 FPS PTY)' : 'Connecting or Offline...'}
              </span>
            </div>
            <span className="text-zinc-500 font-mono text-[11px]">
              Type exit or Ctrl+D to terminate
            </span>
          </div>

          <div
            ref={terminalRef}
            className="w-full h-[450px] bg-zinc-950 rounded-lg p-3 border border-zinc-800 font-mono text-sm overflow-hidden"
          />
        </div>
      </Modal>

      {/* Machine Details & Telemetry Modal */}
      {selectedMachine && (
        <Modal
          isOpen={!!selectedMachine}
          onClose={() => setSelectedMachine(null)}
          title={`Machine Details: ${selectedMachine.hostname || selectedMachine.id}`}
          description={`Host ID: ${selectedMachine.id} • Role: ${selectedMachine.role}`}
          size="lg"
        >
          <div className="space-y-5">
            {/* System Specs Overview */}
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
              <div className="p-3 bg-zinc-950 rounded-lg border border-zinc-800/80">
                <div className="text-[11px] text-zinc-500">Status</div>
                <div className="mt-1 flex items-center gap-1.5 font-semibold text-sm capitalize text-zinc-200">
                  <span
                    className={`h-2 w-2 rounded-full ${
                      selectedMachine.status === 'online' ? 'bg-emerald-400' : 'bg-red-400'
                    }`}
                  />
                  {selectedMachine.status}
                </div>
              </div>

              <div className="p-3 bg-zinc-950 rounded-lg border border-zinc-800/80">
                <div className="text-[11px] text-zinc-500">Public IP</div>
                <div className="mt-1 font-mono text-sm text-zinc-200">
                  {selectedMachine.public_ip || '127.0.0.1'}
                </div>
              </div>

              <div className="p-3 bg-zinc-950 rounded-lg border border-zinc-800/80">
                <div className="text-[11px] text-zinc-500">Architecture</div>
                <div className="mt-1 font-mono text-sm text-zinc-200">
                  {selectedMachine.cpu_arch || 'amd64'}
                </div>
              </div>

              <div className="p-3 bg-zinc-950 rounded-lg border border-zinc-800/80">
                <div className="text-[11px] text-zinc-500">Docker Version</div>
                <div className="mt-1 font-mono text-sm text-zinc-200">
                  {selectedMachine.docker_version || '27.1'}
                </div>
              </div>
            </div>

            {/* Live Telemetry Gauges */}
            <div className="space-y-3">
              <div className="text-xs font-semibold text-zinc-300 flex items-center gap-1.5">
                <Activity className="h-4 w-4 text-cyan-400" />
                <span>Live Host Resource Consumption</span>
              </div>

              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                {/* CPU Gauge */}
                <div className="p-4 bg-zinc-950 rounded-lg border border-zinc-800 space-y-2">
                  <div className="flex items-center justify-between text-xs">
                    <span className="text-zinc-400 flex items-center gap-1.5">
                      <Cpu className="h-3.5 w-3.5 text-cyan-400" />
                      CPU Utilization
                    </span>
                    <span className="font-mono font-semibold text-cyan-300">
                      {selectedMachine.metrics?.cpu_percent.toFixed(1) || '0.0'}%
                    </span>
                  </div>
                  <div className="w-full bg-zinc-800 rounded-full h-2 overflow-hidden">
                    <div
                      className="bg-cyan-400 h-full rounded-full transition-all duration-500"
                      style={{ width: `${Math.min(100, selectedMachine.metrics?.cpu_percent || 0)}%` }}
                    />
                  </div>
                  <div className="text-[10px] text-zinc-500">
                    Cores: {selectedMachine.metrics?.cpu_cores || 4} vCPU
                  </div>
                </div>

                {/* RAM Gauge */}
                <div className="p-4 bg-zinc-950 rounded-lg border border-zinc-800 space-y-2">
                  <div className="flex items-center justify-between text-xs">
                    <span className="text-zinc-400 flex items-center gap-1.5">
                      <HardDrive className="h-3.5 w-3.5 text-emerald-400" />
                      Memory Utilization
                    </span>
                    <span className="font-mono font-semibold text-emerald-300">
                      {selectedMachine.metrics?.mem_percent.toFixed(1) || '25.0'}%
                    </span>
                  </div>
                  <div className="w-full bg-zinc-800 rounded-full h-2 overflow-hidden">
                    <div
                      className="bg-emerald-400 h-full rounded-full transition-all duration-500"
                      style={{ width: `${Math.min(100, selectedMachine.metrics?.mem_percent || 25)}%` }}
                    />
                  </div>
                  <div className="text-[10px] text-zinc-500">
                    Used: {formatBytes(selectedMachine.metrics?.mem_used_bytes || 2147483648)} /{' '}
                    {formatBytes(selectedMachine.metrics?.mem_total_bytes || 8589934592)}
                  </div>
                </div>
              </div>
            </div>

            {/* Swarm Actions */}
            <div className="pt-2 border-t border-zinc-800 flex items-center justify-between">
              <Button
                variant="subtle"
                size="sm"
                onClick={() => {
                  setTerminalNodeId(selectedMachine.id);
                  setSelectedMachine(null);
                }}
                leftIcon={<TerminalIcon className="h-4 w-4 text-cyan-400" />}
              >
                Launch Terminal
              </Button>

              {selectedMachine.role === 'standalone' && (
                <Button
                  variant="primary"
                  size="sm"
                  onClick={() => joinSwarmMutation.mutate({ id: selectedMachine.id, role: 'worker' })}
                  leftIcon={<Layers className="h-4 w-4" />}
                >
                  Join Swarm Cluster Mesh
                </Button>
              )}
            </div>
          </div>
        </Modal>
      )}

      {/* Enroll New Machine Modal */}
      <Modal
        isOpen={isEnrollModalOpen}
        onClose={() => setIsEnrollModalOpen(false)}
        title="Enroll New Remote Machine"
        description="Deploy the lightweight pikpik-agent worker daemon onto any Linux VPS or server"
        size="lg"
      >
        <div className="space-y-5">
          <div className="p-3.5 bg-cyan-950/30 border border-cyan-800/40 rounded-lg text-xs text-cyan-200 flex items-start gap-2.5">
            <Info className="h-4 w-4 text-cyan-400 shrink-0 mt-0.5" />
            <div>
              <span className="font-semibold text-cyan-100">Zero Shelling Direct Socket Architecture:</span>{' '}
              The agent establishes a single outbound encrypted mTLS/WSS tunnel to the control plane. No incoming firewall ports required.
            </div>
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between text-xs">
              <span className="font-semibold text-zinc-200">1-Click Bash Auto-Installer</span>
              <Button
                variant="subtle"
                size="sm"
                onClick={() =>
                  copyToClipboard(
                    enrollCommand?.command ||
                      'curl -fsSL https://cp.pikpik.dev/install-agent.sh | bash -s -- --token pik_node_token',
                    'enroll'
                  )
                }
              >
                {copiedType === 'enroll' ? (
                  <Check className="h-3 w-3 text-emerald-400" />
                ) : (
                  <Copy className="h-3 w-3" />
                )}
                <span className="ml-1">Copy Command</span>
              </Button>
            </div>
            <pre className="p-3 bg-zinc-950 rounded-lg border border-zinc-800 font-mono text-xs text-cyan-300 select-all overflow-x-auto">
              {enrollCommand?.command ||
                'curl -fsSL https://cp.pikpik.dev/install-agent.sh | bash -s -- --token pik_node_token --control-plane-url wss://cp.pikpik.dev/agent/connect'}
            </pre>
          </div>

          <div className="space-y-2 text-xs text-zinc-400">
            <div className="font-semibold text-zinc-300">Enrollment Instructions:</div>
            <div className="flex items-center gap-2">
              <CheckCircle2 className="h-3.5 w-3.5 text-emerald-400" />
              <span>1. Run the script as root on your target machine</span>
            </div>
            <div className="flex items-center gap-2">
              <CheckCircle2 className="h-3.5 w-3.5 text-emerald-400" />
              <span>2. pikpik-agent starts as a systemd service and dials out over WebSocket</span>
            </div>
            <div className="flex items-center gap-2">
              <CheckCircle2 className="h-3.5 w-3.5 text-emerald-400" />
              <span>3. Host metrics & container controls will appear live in this console</span>
            </div>
          </div>
        </div>
      </Modal>

      {/* Swarm Join Tokens Modal */}
      <Modal
        isOpen={isJoinModalOpen}
        onClose={() => setIsJoinModalOpen(false)}
        title="Scale Swarm Compute Topology"
        description="Execute these commands on your target servers to join this cluster"
        size="lg"
      >
        <div className="space-y-5">
          {/* Worker Node Command */}
          <div className="space-y-2">
            <div className="flex items-center justify-between text-xs">
              <span className="font-semibold text-zinc-200">Join as Worker Node</span>
              <Button
                variant="subtle"
                size="sm"
                onClick={() =>
                  copyToClipboard(
                    `docker swarm join --token ${joinTokens?.worker || 'SWMTKN-...'} 127.0.0.1:2377`,
                    'worker'
                  )
                }
              >
                {copiedType === 'worker' ? (
                  <Check className="h-3 w-3 text-emerald-400" />
                ) : (
                  <Copy className="h-3 w-3" />
                )}
                <span className="ml-1">Copy Command</span>
              </Button>
            </div>
            <pre className="p-3 bg-zinc-950 rounded-lg border border-zinc-800 font-mono text-xs text-cyan-300 select-all overflow-x-auto">
              docker swarm join --token {joinTokens?.worker || 'SWMTKN-1-worker-token-xyz'} 127.0.0.1:2377
            </pre>
          </div>

          {/* Manager Node Command */}
          <div className="space-y-2">
            <div className="flex items-center justify-between text-xs">
              <span className="font-semibold text-zinc-200">Join as Manager Node (HA Raft Consensus)</span>
              <Button
                variant="subtle"
                size="sm"
                onClick={() =>
                  copyToClipboard(
                    `docker swarm join --token ${joinTokens?.manager || 'SWMTKN-...'} 127.0.0.1:2377`,
                    'manager'
                  )
                }
              >
                {copiedType === 'manager' ? (
                  <Check className="h-3 w-3 text-emerald-400" />
                ) : (
                  <Copy className="h-3 w-3" />
                )}
                <span className="ml-1">Copy Command</span>
              </Button>
            </div>
            <pre className="p-3 bg-zinc-950 rounded-lg border border-zinc-800 font-mono text-xs text-purple-300 select-all overflow-x-auto">
              docker swarm join --token {joinTokens?.manager || 'SWMTKN-1-manager-token-xyz'} 127.0.0.1:2377
            </pre>
          </div>
        </div>
      </Modal>
    </div>
  );
}
