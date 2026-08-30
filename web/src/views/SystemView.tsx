import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../lib/api';
import { PruneRequest } from '../lib/types';
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Badge } from '../components/ui/Badge';
import { Modal } from '../components/ui/Modal';
import { useToast } from '../components/ui/Toast';
import { formatBytes } from '../lib/utils';
import {
  Cpu,
  Trash2,
  Server,
  Layers,
  Box,
  RotateCw,
} from 'lucide-react';

export function SystemView() {
  const queryClient = useQueryClient();
  const toast = useToast();

  const [isPruneModalOpen, setIsPruneModalOpen] = useState(false);
  const [pruneAll, setPruneAll] = useState(true);
  const [pruneVolumes, setPruneVolumes] = useState(false);
  const [pruneBuildCache, setPruneBuildCache] = useState(true);

  const { data: info, refetch: refetchInfo } = useQuery({
    queryKey: ['systemInfo'],
    queryFn: api.system.getInfo,
  });

  const { data: disk, refetch: refetchDisk } = useQuery({
    queryKey: ['diskInfo'],
    queryFn: api.system.getDiskUsage,
  });

  const pruneMutation = useMutation({
    mutationFn: (req: PruneRequest) => api.system.prune(req),
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ['diskInfo'] });
      toast.success(
        'System Pruned',
        `Reclaimed ${formatBytes(res.space_reclaimed_bytes)} of host disk space`
      );
      setIsPruneModalOpen(false);
    },
    onError: (err: Error) => toast.error('Prune Failed', err.message),
  });

  const totalUsed =
    (disk?.images_bytes || 0) +
    (disk?.containers_bytes || 0) +
    (disk?.volumes_bytes || 0) +
    (disk?.build_cache_bytes || 0);

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-zinc-100 flex items-center gap-2">
            <Cpu className="h-5 w-5 text-cyan-400" />
            <span>System & Disk Health</span>
          </h1>
          <p className="text-xs text-zinc-400 mt-0.5">
            Host operating environment, daemon parameters, and Docker disk reclamation
          </p>
        </div>

        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              refetchInfo();
              refetchDisk();
            }}
            leftIcon={<RotateCw className="h-3.5 w-3.5" />}
          >
            Refresh
          </Button>
          <Button
            variant="primary"
            size="sm"
            onClick={() => setIsPruneModalOpen(true)}
            leftIcon={<Trash2 className="h-3.5 w-3.5" />}
          >
            Prune System Disk
          </Button>
        </div>
      </div>

      {/* Host System Specs */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <Card>
          <div className="flex items-center justify-between">
            <span className="text-xs text-zinc-400">Host OS</span>
            <Server className="h-4 w-4 text-zinc-500" />
          </div>
          <div className="mt-3 text-sm font-bold font-mono text-zinc-100 truncate">
            {info?.host_os || 'Linux 6.x x86_64'}
          </div>
          <div className="text-[11px] text-zinc-500 mt-1">POSIX Control Plane</div>
        </Card>

        <Card>
          <div className="flex items-center justify-between">
            <span className="text-xs text-zinc-400">Docker Version</span>
            <Box className="h-4 w-4 text-cyan-400" />
          </div>
          <div className="mt-3 text-sm font-bold font-mono text-cyan-300">
            {info?.docker_version || '27.5.1'}
          </div>
          <div className="text-[11px] text-zinc-500 mt-1">Docker API Engine</div>
        </Card>

        <Card>
          <div className="flex items-center justify-between">
            <span className="text-xs text-zinc-400">Swarm State</span>
            <Badge variant="success" dot>
              {info?.swarm_active ? 'Active' : 'Standalone'}
            </Badge>
          </div>
          <div className="mt-3 text-sm font-bold font-mono text-emerald-400">
            {info?.nodes_count || 1} Node(s)
          </div>
          <div className="text-[11px] text-zinc-500 mt-1">Raft consensus leader</div>
        </Card>

        <Card>
          <div className="flex items-center justify-between">
            <span className="text-xs text-zinc-400">Active Containers</span>
            <Layers className="h-4 w-4 text-purple-400" />
          </div>
          <div className="mt-3 text-sm font-bold font-mono text-purple-300">
            {info?.containers_count || 0} Running
          </div>
          <div className="text-[11px] text-zinc-500 mt-1">Managed tasks</div>
        </Card>
      </div>

      {/* Disk Usage Breakdown */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <div>
              <CardTitle className="text-sm">Docker Disk Footprint</CardTitle>
              <CardDescription>
                Storage breakdown across image layers, containers, volumes, and build caches
              </CardDescription>
            </div>
            <div className="text-right">
              <span className="text-sm font-bold font-mono text-zinc-100">
                {formatBytes(totalUsed)}
              </span>
              <span className="text-xs text-zinc-500 block">Total Used</span>
            </div>
          </div>
        </CardHeader>

        <CardContent className="space-y-6">
          {/* Progress Bar Stack */}
          <div className="h-4 w-full bg-zinc-950 rounded-full overflow-hidden flex border border-zinc-800">
            <div
              style={{
                width: `${totalUsed > 0 ? ((disk?.images_bytes || 0) / totalUsed) * 100 : 25}%`,
              }}
              className="bg-cyan-500 transition-all"
              title="Images"
            />
            <div
              style={{
                width: `${totalUsed > 0 ? ((disk?.containers_bytes || 0) / totalUsed) * 100 : 25}%`,
              }}
              className="bg-emerald-500 transition-all"
              title="Containers"
            />
            <div
              style={{
                width: `${totalUsed > 0 ? ((disk?.volumes_bytes || 0) / totalUsed) * 100 : 25}%`,
              }}
              className="bg-purple-500 transition-all"
              title="Volumes"
            />
            <div
              style={{
                width: `${totalUsed > 0 ? ((disk?.build_cache_bytes || 0) / totalUsed) * 100 : 25}%`,
              }}
              className="bg-amber-500 transition-all"
              title="Build Cache"
            />
          </div>

          {/* Details Table */}
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
            <div className="p-3 bg-zinc-950 rounded-lg border border-zinc-800">
              <div className="flex items-center gap-2">
                <span className="h-2.5 w-2.5 rounded-full bg-cyan-500" />
                <span className="text-xs text-zinc-400">Image Layers</span>
              </div>
              <div className="mt-2 text-base font-bold font-mono text-zinc-100">
                {disk ? formatBytes(disk.images_bytes) : '0 B'}
              </div>
            </div>

            <div className="p-3 bg-zinc-950 rounded-lg border border-zinc-800">
              <div className="flex items-center gap-2">
                <span className="h-2.5 w-2.5 rounded-full bg-emerald-500" />
                <span className="text-xs text-zinc-400">Containers</span>
              </div>
              <div className="mt-2 text-base font-bold font-mono text-zinc-100">
                {disk ? formatBytes(disk.containers_bytes) : '0 B'}
              </div>
            </div>

            <div className="p-3 bg-zinc-950 rounded-lg border border-zinc-800">
              <div className="flex items-center gap-2">
                <span className="h-2.5 w-2.5 rounded-full bg-purple-500" />
                <span className="text-xs text-zinc-400">Volumes</span>
              </div>
              <div className="mt-2 text-base font-bold font-mono text-zinc-100">
                {disk ? formatBytes(disk.volumes_bytes) : '0 B'}
              </div>
            </div>

            <div className="p-3 bg-zinc-950 rounded-lg border border-zinc-800">
              <div className="flex items-center gap-2">
                <span className="h-2.5 w-2.5 rounded-full bg-amber-500" />
                <span className="text-xs text-zinc-400">Build Cache</span>
              </div>
              <div className="mt-2 text-base font-bold font-mono text-zinc-100">
                {disk ? formatBytes(disk.build_cache_bytes) : '0 B'}
              </div>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Prune Modal */}
      <Modal
        isOpen={isPruneModalOpen}
        onClose={() => setIsPruneModalOpen(false)}
        title="Prune System Storage"
        description="Clean unused image caches, stopped containers, and build artifacts"
      >
        <div className="space-y-4">
          <div className="space-y-3">
            <label className="flex items-start gap-2.5 cursor-pointer">
              <input
                type="checkbox"
                checked={pruneAll}
                onChange={(e) => setPruneAll(e.target.checked)}
                className="h-4 w-4 mt-0.5 rounded border-zinc-700 bg-zinc-900 text-cyan-500 focus:ring-cyan-500"
              />
              <div>
                <span className="text-xs font-semibold text-zinc-200 block">
                  Prune Unused Images & Containers
                </span>
                <span className="text-[11px] text-zinc-500">
                  Deletes all dangling layers and containers not currently active
                </span>
              </div>
            </label>

            <label className="flex items-start gap-2.5 cursor-pointer">
              <input
                type="checkbox"
                checked={pruneBuildCache}
                onChange={(e) => setPruneBuildCache(e.target.checked)}
                className="h-4 w-4 mt-0.5 rounded border-zinc-700 bg-zinc-900 text-cyan-500 focus:ring-cyan-500"
              />
              <div>
                <span className="text-xs font-semibold text-zinc-200 block">
                  Prune BuildKit Cache
                </span>
                <span className="text-[11px] text-zinc-500">
                  Clears intermediate compilation steps from previous image builds
                </span>
              </div>
            </label>

            <label className="flex items-start gap-2.5 cursor-pointer">
              <input
                type="checkbox"
                checked={pruneVolumes}
                onChange={(e) => setPruneVolumes(e.target.checked)}
                className="h-4 w-4 mt-0.5 rounded border-zinc-700 bg-zinc-900 text-rose-500 focus:ring-rose-500"
              />
              <div>
                <span className="text-xs font-semibold text-rose-300 block">
                  Prune Anonymous Volumes (Caution)
                </span>
                <span className="text-[11px] text-zinc-500">
                  Deletes unnamed volumes not associated with running services
                </span>
              </div>
            </label>
          </div>

          <div className="flex justify-end gap-2 pt-4 border-t border-zinc-800">
            <Button variant="outline" size="sm" onClick={() => setIsPruneModalOpen(false)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              size="sm"
              onClick={() =>
                pruneMutation.mutate({
                  all: pruneAll,
                  volumes: pruneVolumes,
                  build_cache: pruneBuildCache,
                })
              }
              isLoading={pruneMutation.isPending}
            >
              Run Prune Action
            </Button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
