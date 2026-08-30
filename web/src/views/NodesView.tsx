import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../lib/api';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Badge } from '../components/ui/Badge';
import { Modal } from '../components/ui/Modal';
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '../components/ui/Table';
import { useToast } from '../components/ui/Toast';
import { formatBytes, formatDate } from '../lib/utils';
import {
  Server,
  Key,
  Copy,
  Check,
  Crown,
} from 'lucide-react';

export function NodesView() {
  const queryClient = useQueryClient();
  const toast = useToast();

  const [isJoinModalOpen, setIsJoinModalOpen] = useState(false);
  const [copiedType, setCopiedType] = useState<'worker' | 'manager' | null>(null);

  const { data: nodes, isLoading } = useQuery({
    queryKey: ['nodes'],
    queryFn: api.nodes.list,
  });

  const { data: joinTokens } = useQuery({
    queryKey: ['joinTokens'],
    queryFn: api.nodes.getJoinTokens,
    enabled: isJoinModalOpen,
  });

  const updateNodeMutation = useMutation({
    mutationFn: ({ id, availability }: { id: string; availability: 'active' | 'pause' | 'drain' }) =>
      api.nodes.update(id, { availability }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['nodes'] });
      toast.success('Node Updated', 'Swarm node availability modified');
    },
    onError: (err: Error) => toast.error('Update Failed', err.message),
  });

  const copyToClipboard = (text: string, type: 'worker' | 'manager') => {
    navigator.clipboard.writeText(text);
    setCopiedType(type);
    toast.success('Copied to Clipboard', `Join command copied`);
    setTimeout(() => setCopiedType(null), 2000);
  };

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-zinc-100 flex items-center gap-2">
            <Server className="h-5 w-5 text-cyan-400" />
            <span>Swarm Nodes</span>
          </h1>
          <p className="text-xs text-zinc-400 mt-0.5">
            Distributed multi-node compute topology with automatic failover
          </p>
        </div>

        <Button
          variant="primary"
          size="sm"
          onClick={() => setIsJoinModalOpen(true)}
          leftIcon={<Key className="h-4 w-4" />}
        >
          Join Tokens & Commands
        </Button>
      </div>

      {/* Nodes Table */}
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
            {isLoading ? (
              <TableRow>
                <TableCell colSpan={9} className="text-center py-8 text-zinc-500">
                  Loading swarm nodes...
                </TableCell>
              </TableRow>
            ) : (!nodes || nodes.length === 0) ? (
              <TableRow>
                <TableCell colSpan={9} className="text-center py-10 text-zinc-500">
                  No swarm nodes discovered. Initializing primary manager...
                </TableCell>
              </TableRow>
            ) : (
              nodes.map((node) => (
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
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </Card>

      {/* Join Tokens Modal */}
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
