import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../lib/api';
import { Backup } from '../lib/types';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Badge } from '../components/ui/Badge';
import { Modal } from '../components/ui/Modal';
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '../components/ui/Table';
import { useToast } from '../components/ui/Toast';
import { formatBytes, formatDate } from '../lib/utils';
import {
  Archive,
  Plus,
  RotateCcw,
  Trash2,
  HardDrive,
} from 'lucide-react';

export function BackupsView() {
  const queryClient = useQueryClient();
  const toast = useToast();

  const [isCreateModalOpen, setIsCreateModalOpen] = useState(false);
  const [selectedBackupForRestore, setSelectedBackupForRestore] = useState<Backup | null>(null);
  const [serviceId, setServiceId] = useState('');

  const { data: backups, isLoading } = useQuery({
    queryKey: ['backups'],
    queryFn: api.backups.list,
  });

  const { data: databases } = useQuery({
    queryKey: ['databases'],
    queryFn: api.databases.list,
  });

  const { data: destinations } = useQuery({
    queryKey: ['backupDestinations'],
    queryFn: api.backups.listDestinations,
  });

  const createMutation = useMutation({
    mutationFn: (targetService: string) => api.backups.create(targetService),
    onSuccess: (newBackup) => {
      queryClient.invalidateQueries({ queryKey: ['backups'] });
      toast.success('Backup Started', `Snapshot initiated for service ${newBackup.service_id}`);
      setIsCreateModalOpen(false);
    },
    onError: (err: Error) => toast.error('Backup Failed', err.message),
  });

  const restoreMutation = useMutation({
    mutationFn: ({ id, targetId }: { id: string; targetId?: string }) =>
      api.backups.restore(id, targetId),
    onSuccess: () => {
      toast.success('Restore Complete', 'Database restored successfully');
      setSelectedBackupForRestore(null);
    },
    onError: (err: Error) => toast.error('Restore Failed', err.message),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.backups.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['backups'] });
      toast.success('Backup Deleted', 'Snapshot removed from S3 bucket');
    },
    onError: (err: Error) => toast.error('Delete Failed', err.message),
  });

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-zinc-100 flex items-center gap-2">
            <Archive className="h-5 w-5 text-cyan-400" />
            <span>Backups & S3 Streaming</span>
          </h1>
          <p className="text-xs text-zinc-400 mt-0.5">
            Streaming pg_dump / mysqldump pipelines directly to S3-compatible object storage
          </p>
        </div>

        <Button
          variant="primary"
          size="sm"
          onClick={() => setIsCreateModalOpen(true)}
          leftIcon={<Plus className="h-4 w-4" />}
        >
          Create Snapshot
        </Button>
      </div>

      {/* S3 Storage Destination Banner */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        {destinations && destinations.length > 0 ? (
          destinations.map((d) => (
            <Card key={d.id} className="p-4 flex items-center justify-between">
              <div className="flex items-center gap-3">
                <div className="p-2 bg-zinc-800 rounded-lg text-cyan-400">
                  <HardDrive className="h-4 w-4" />
                </div>
                <div>
                  <div className="text-xs font-semibold text-zinc-200">{d.name}</div>
                  <div className="text-[11px] font-mono text-zinc-500">
                    Bucket: {d.bucket} ({d.region})
                  </div>
                </div>
              </div>
              {d.is_default && <Badge variant="success">Default S3</Badge>}
            </Card>
          ))
        ) : (
          <Card className="p-4 md:col-span-3 flex items-center justify-between">
            <div className="flex items-center gap-3">
              <div className="p-2 bg-zinc-800 rounded-lg text-cyan-400">
                <HardDrive className="h-4 w-4" />
              </div>
              <div>
                <div className="text-xs font-semibold text-zinc-200">Local S3 Bucket (pikpik-backups)</div>
                <div className="text-[11px] font-mono text-zinc-500">
                  Streaming S3-compatible multi-part snapshot target
                </div>
              </div>
            </div>
            <Badge variant="success">Active</Badge>
          </Card>
        )}
      </div>

      {/* Backups Table */}
      <Card className="p-0 overflow-hidden">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Snapshot ID / Key</TableHead>
              <TableHead>Target Service</TableHead>
              <TableHead>Compressed Size</TableHead>
              <TableHead>Execution Time</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Created</TableHead>
              <TableHead className="text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <TableRow>
                <TableCell colSpan={7} className="text-center py-8 text-zinc-500">
                  Loading backup snapshots...
                </TableCell>
              </TableRow>
            ) : (!backups || backups.length === 0) ? (
              <TableRow>
                <TableCell colSpan={7} className="text-center py-10 text-zinc-500">
                  No backup archives found. Click "Create Snapshot" to back up a database.
                </TableCell>
              </TableRow>
            ) : (
              backups.map((b) => (
                <TableRow key={b.id}>
                  <TableCell className="font-semibold text-zinc-100">
                    <div className="flex items-center gap-2">
                      <Archive className="h-4 w-4 text-purple-400 shrink-0" />
                      <div>
                        <div className="font-mono text-xs text-zinc-200 truncate max-w-xs">
                          {b.s3_key || b.id}
                        </div>
                        <div className="text-[10px] font-mono text-zinc-500">{b.id}</div>
                      </div>
                    </div>
                  </TableCell>
                  <TableCell className="font-mono text-xs text-zinc-300">
                    {b.service_id}
                  </TableCell>
                  <TableCell className="font-mono text-xs text-zinc-300">
                    {b.compressed_bytes ? formatBytes(b.compressed_bytes) : '—'}
                  </TableCell>
                  <TableCell className="font-mono text-xs text-zinc-400">
                    {b.duration_ms ? `${b.duration_ms}ms` : '—'}
                  </TableCell>
                  <TableCell>
                    <Badge variant={b.status === 'completed' ? 'success' : 'warning'} dot>
                      {b.status}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-xs text-zinc-500">
                    {formatDate(b.created_at)}
                  </TableCell>
                  <TableCell className="text-right">
                    <div className="flex items-center justify-end gap-1">
                      <Button
                        variant="outline"
                        size="sm"
                        title="Restore Snapshot"
                        onClick={() => setSelectedBackupForRestore(b)}
                        leftIcon={<RotateCcw className="h-3 w-3" />}
                      >
                        Restore
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        title="Delete"
                        onClick={() => {
                          if (confirm(`Delete backup ${b.id}?`)) {
                            deleteMutation.mutate(b.id);
                          }
                        }}
                      >
                        <Trash2 className="h-3.5 w-3.5 text-rose-400" />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </Card>

      {/* Create Backup Modal */}
      <Modal
        isOpen={isCreateModalOpen}
        onClose={() => setIsCreateModalOpen(false)}
        title="Create Database Backup Snapshot"
        description="Stream an immediate compressed backup dump directly into S3"
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            createMutation.mutate(serviceId);
          }}
          className="space-y-4"
        >
          <div className="space-y-1.5">
            <label className="block text-xs font-medium text-zinc-300">Target Database</label>
            <select
              value={serviceId}
              onChange={(e) => setServiceId(e.target.value)}
              className="w-full rounded-lg border border-zinc-800 bg-zinc-950 px-3.5 py-2 text-sm text-zinc-100 focus:border-cyan-500 focus:outline-none"
              required
            >
              <option value="">Select database...</option>
              {databases?.map((db) => (
                <option key={db.id} value={db.name}>
                  {db.name} ({db.engine})
                </option>
              ))}
            </select>
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
              Start Snapshot Stream
            </Button>
          </div>
        </form>
      </Modal>

      {/* Restore Modal */}
      {selectedBackupForRestore && (
        <Modal
          isOpen={Boolean(selectedBackupForRestore)}
          onClose={() => setSelectedBackupForRestore(null)}
          title="Restore Database from Snapshot"
          description={`Restoring ${selectedBackupForRestore.s3_key || selectedBackupForRestore.id}`}
        >
          <div className="space-y-4">
            <div className="p-3 bg-amber-950/30 border border-amber-800/50 rounded-lg text-xs text-amber-300">
              Warning: Restoring will overwrite existing data inside target service{' '}
              <strong>{selectedBackupForRestore.service_id}</strong>.
            </div>

            <div className="flex justify-end gap-2 pt-4 border-t border-zinc-800">
              <Button
                variant="outline"
                size="sm"
                onClick={() => setSelectedBackupForRestore(null)}
              >
                Cancel
              </Button>
              <Button
                variant="destructive"
                size="sm"
                onClick={() =>
                  restoreMutation.mutate({
                    id: selectedBackupForRestore.id,
                    targetId: selectedBackupForRestore.service_id,
                  })
                }
                isLoading={restoreMutation.isPending}
              >
                Confirm Restore
              </Button>
            </div>
          </div>
        </Modal>
      )}
    </div>
  );
}
