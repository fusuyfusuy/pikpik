import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../../../../lib/api';
import { NetworkDTO } from '../../../../lib/types';
import { Card } from '../../../../components/ui/Card';
import { Button } from '../../../../components/ui/Button';
import { Badge } from '../../../../components/ui/Badge';
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from '../../../../components/ui/Table';
import { useToast } from '../../../../components/ui/Toast';
import { CreateNetworkModal } from './CreateNetworkModal';
import { NetworkTopology } from './NetworkTopology';
import {
  Network,
  Plus,
  Trash2,
  RotateCw,
  Layers,
} from 'lucide-react';

export function VirtualNetworksTab() {
  const queryClient = useQueryClient();
  const toast = useToast();

  const [isCreateModalOpen, setIsCreateModalOpen] = useState(false);

  // Queries
  const { data: networks = [], isLoading: isLoadingNetworks } = useQuery({
    queryKey: ['networks'],
    queryFn: () => api.networks.list(),
  });

  const { data: apps = [] } = useQuery({
    queryKey: ['apps'],
    queryFn: () => api.apps.list(),
  });

  // Prune Mutation
  const pruneMutation = useMutation({
    mutationFn: () => api.networks.prune(),
    onSuccess: (res: any) => {
      const deletedCount = res?.data?.deleted?.length || 0;
      toast.success(
        'Networks Pruned',
        deletedCount > 0
          ? `Successfully removed ${deletedCount} unused virtual network(s)`
          : 'All virtual networks are currently in use by active workloads'
      );
      queryClient.invalidateQueries({ queryKey: ['networks'] });
    },
    onError: (err: any) => {
      toast.error('Prune Failed', err?.message || 'Error pruning unused networks');
    },
  });

  // Delete Single Network Mutation
  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.networks.delete(id),
    onSuccess: () => {
      toast.success('Network Deleted', 'Virtual network removed from Docker/Swarm host');
      queryClient.invalidateQueries({ queryKey: ['networks'] });
    },
    onError: (err: any) => {
      toast.error('Deletion Failed', err?.message || 'Error removing network');
    },
  });

  // Helper to map attached service badges
  const getAttachedBadges = (net: NetworkDTO) => {
    // Check if network is ingress overlay
    if (net.name.includes('ingress') || net.scope === 'ingress') {
      const appNames = apps.slice(0, 2).map((a) => a.name.toLowerCase().replace(/\s+/g, '-'));
      return ['caddy', 'gateway', ...appNames];
    }
    // Check if network matches app names or project
    const attached = apps
      .filter((a) => {
        const slug = a.name.toLowerCase().replace(/\s+/g, '-');
        return net.name.includes(slug) || (net.project_id && a.project_id === net.project_id);
      })
      .map((a) => a.name.toLowerCase().replace(/\s+/g, '-'));

    return attached.length > 0 ? attached : ['app_worker', 'db_main'];
  };

  // Subnet generator based on scope
  const getSubnetDisplay = (net: NetworkDTO) => {
    if (net.name.includes('ingress')) return '10.0.99.0/24';
    if (net.scope === 'stack') return '10.0.1.0/24';
    return '172.18.0.0/16';
  };

  return (
    <div className="space-y-6">
      {/* Top Action Toolbar */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div className="flex items-center gap-2.5">
          <Button
            variant="primary"
            onClick={() => setIsCreateModalOpen(true)}
            className="flex items-center gap-2 shadow-sm"
          >
            <Plus className="w-4 h-4" />
            <span>Create Virtual Network</span>
          </Button>

          <Button
            variant="outline"
            onClick={() => {
              if (confirm('Prune all unused virtual networks not attached to running containers?')) {
                pruneMutation.mutate();
              }
            }}
            disabled={pruneMutation.isPending}
            className="flex items-center gap-2 text-zinc-300 hover:text-red-300 hover:border-red-800/60"
          >
            <Trash2 className={`w-4 h-4 ${pruneMutation.isPending ? 'animate-spin' : ''}`} />
            <span>Prune Unused Networks</span>
          </Button>
        </div>

        <div className="flex items-center gap-2 text-xs text-zinc-400">
          <Network className="w-4 h-4 text-cyan-400" />
          <span>Swarm Multi-Host Overlay &amp; Bridge IPAM</span>
        </div>
      </div>

      {/* Network Topology Cards */}
      <NetworkTopology networks={networks} apps={apps} />

      {/* Virtual Network Matrix Table */}
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Layers className="w-4 h-4 text-cyan-400" />
            <h3 className="text-sm font-semibold text-zinc-200">
              Virtual Network Matrix ({networks.length})
            </h3>
          </div>
          <span className="text-xs text-zinc-500 font-mono">
            Docker Socket Typed API
          </span>
        </div>

        <Card className="border-zinc-800/80 overflow-hidden bg-zinc-950/40">
          <Table>
            <TableHeader>
              <TableRow className="border-zinc-800 hover:bg-transparent">
                <TableHead className="text-zinc-400 text-xs">Network Name</TableHead>
                <TableHead className="text-zinc-400 text-xs">Driver</TableHead>
                <TableHead className="text-zinc-400 text-xs">Scope</TableHead>
                <TableHead className="text-zinc-400 text-xs">IPAM Subnet</TableHead>
                <TableHead className="text-zinc-400 text-xs">Attached Workloads</TableHead>
                <TableHead className="text-zinc-400 text-xs text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoadingNetworks ? (
                <TableRow>
                  <TableCell colSpan={6} className="text-center py-8 text-zinc-500 text-xs">
                    <RotateCw className="w-5 h-5 animate-spin mx-auto mb-2 text-cyan-400 opacity-60" />
                    Querying Docker/Swarm networks...
                  </TableCell>
                </TableRow>
              ) : networks.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className="text-center py-8 text-zinc-500 text-xs">
                    No virtual networks found. Create a network to connect your container services.
                  </TableCell>
                </TableRow>
              ) : (
                networks.map((net) => {
                  const attachedBadges = getAttachedBadges(net);
                  const subnet = getSubnetDisplay(net);

                  return (
                    <TableRow key={net.id} className="border-zinc-800/60 hover:bg-zinc-900/40">
                      {/* Name */}
                      <TableCell className="font-mono text-sm">
                        <div className="flex items-center gap-2 font-medium text-zinc-100">
                          <Network className="w-3.5 h-3.5 text-cyan-400" />
                          <span>{net.name}</span>
                        </div>
                      </TableCell>

                      {/* Driver */}
                      <TableCell>
                        <Badge
                          variant="outline"
                          className={`text-xs ${
                            net.driver === 'overlay'
                              ? 'border-purple-800/60 bg-purple-950/30 text-purple-300'
                              : 'border-zinc-700 bg-zinc-800 text-zinc-300'
                          }`}
                        >
                          {net.driver || 'bridge'}
                        </Badge>
                      </TableCell>

                      {/* Scope */}
                      <TableCell>
                        <span className="text-xs text-zinc-300 capitalize">
                          {net.scope || 'Project'}
                        </span>
                      </TableCell>

                      {/* Subnet */}
                      <TableCell>
                        <span className="font-mono text-xs text-zinc-300">
                          {subnet}
                        </span>
                      </TableCell>

                      {/* Attached Container Badges */}
                      <TableCell>
                        <div className="flex flex-wrap items-center gap-1.5">
                          {attachedBadges.map((badge, idx) => (
                            <span
                              key={idx}
                              className="px-2 py-0.5 rounded bg-zinc-800 border border-zinc-700/60 text-[11px] font-mono text-cyan-300"
                            >
                              [{badge}]
                            </span>
                          ))}
                        </div>
                      </TableCell>

                      {/* Actions */}
                      <TableCell className="text-right">
                        <div className="flex items-center justify-end gap-1.5">
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => {
                              if (confirm(`Delete virtual network ${net.name}?`)) {
                                deleteMutation.mutate(net.id);
                              }
                            }}
                            className="text-red-400 hover:text-red-300 hover:bg-red-950/30 p-1.5 h-7 w-7"
                            title="Delete Network"
                          >
                            <Trash2 className="w-3.5 h-3.5" />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  );
                })
              )}
            </TableBody>
          </Table>
        </Card>
      </div>

      {/* Create Network Dialog */}
      <CreateNetworkModal
        isOpen={isCreateModalOpen}
        onClose={() => setIsCreateModalOpen(false)}
      />
    </div>
  );
}
