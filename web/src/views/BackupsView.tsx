import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../lib/api';
import {
  Backup,
  CreateBackupScheduleRequest,
  DatabaseEngine,
} from '../lib/types';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Badge } from '../components/ui/Badge';
import { Input } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '../components/ui/Table';
import { useToast } from '../components/ui/Toast';
import { QueryErrorAlert } from '../components/ui/QueryErrorAlert';
import { formatBytes, formatDate } from '../lib/utils';
import {
  Archive,
  Plus,
  RotateCcw,
  Trash2,
  HardDrive,
  Calendar,
  Clock,
} from 'lucide-react';

const CRON_PRESETS = [
  { label: 'Daily (Midnight UTC)', expr: '0 0 * * *' },
  { label: 'Every 6 Hours', expr: '0 */6 * * *' },
  { label: 'Weekly (Sunday 00:00)', expr: '0 0 * * 0' },
  { label: 'Custom Cron', expr: 'custom' },
];

const DB_ENGINES: { id: DatabaseEngine; name: string }[] = [
  { id: 'postgres', name: 'PostgreSQL' },
  { id: 'mysql', name: 'MySQL' },
  { id: 'redis', name: 'Redis (RDB snapshot)' },
  { id: 'mongodb', name: 'MongoDB (mongodump)' },
  { id: 'mariadb', name: 'MariaDB' },
];

export function BackupsView() {
  const queryClient = useQueryClient();
  const toast = useToast();

  const [activeTab, setActiveTab] = useState<'snapshots' | 'schedules' | 'destinations'>('snapshots');

  // Snapshot modal states
  const [isCreateSnapshotModalOpen, setIsCreateSnapshotModalOpen] = useState(false);
  const [selectedBackupForRestore, setSelectedBackupForRestore] = useState<Backup | null>(null);
  const [snapshotServiceId, setSnapshotServiceId] = useState('');
  const [restoreTargetServiceId, setRestoreTargetServiceId] = useState('');

  // Schedule modal states
  const [isScheduleModalOpen, setIsScheduleModalOpen] = useState(false);
  const [scheduleServiceId, setScheduleServiceId] = useState('');
  const [scheduleDbEngine, setScheduleDbEngine] = useState<DatabaseEngine>('postgres');
  const [schedulePreset, setSchedulePreset] = useState('0 0 * * *');
  const [scheduleCustomCron, setScheduleCustomCron] = useState('0 3 * * *');
  const [scheduleRetentionDays, setScheduleRetentionDays] = useState(14);
  const [scheduleDestinationId, setScheduleDestinationId] = useState('');
  const [scheduleIsEnabled, setScheduleIsEnabled] = useState(true);

  // Queries
  const {
    data: backups,
    isLoading: isBackupsLoading,
    isError: isBackupsError,
    error: backupsError,
    refetch: refetchBackups,
  } = useQuery({
    queryKey: ['backups'],
    queryFn: api.backups.list,
  });

  const {
    data: schedules,
    isLoading: isSchedulesLoading,
    isError: isSchedulesError,
    error: schedulesError,
    refetch: refetchSchedules,
  } = useQuery({
    queryKey: ['backupSchedules'],
    queryFn: () => api.schedules.list(),
  });

  const { data: databases } = useQuery({
    queryKey: ['databases'],
    queryFn: api.databases.list,
  });

  const {
    data: destinations,
    isError: isDestinationsError,
    error: destinationsError,
    refetch: refetchDestinations,
  } = useQuery({
    queryKey: ['backupDestinations'],
    queryFn: api.backups.listDestinations,
  });

  const safeBackups = backups || [];
  const safeSchedules = schedules || [];
  const safeDatabases = databases || [];
  const safeDestinations = destinations || [];

  // Snapshot Mutations
  const createSnapshotMutation = useMutation({
    mutationFn: (targetService: string) => api.backups.create(targetService),
    onSuccess: (newBackup) => {
      queryClient.invalidateQueries({ queryKey: ['backups'] });
      toast.success('Backup Started', `Streaming snapshot initiated for service ${newBackup.service_id}`);
      setIsCreateSnapshotModalOpen(false);
      setSnapshotServiceId('');
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

  const deleteBackupMutation = useMutation({
    mutationFn: (id: string) => api.backups.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['backups'] });
      toast.success('Backup Deleted', 'Snapshot removed from S3 bucket');
    },
    onError: (err: Error) => toast.error('Delete Failed', err.message),
  });

  // Schedule Mutations
  const createScheduleMutation = useMutation({
    mutationFn: (req: CreateBackupScheduleRequest) => api.schedules.create(req),
    onSuccess: (sch) => {
      queryClient.invalidateQueries({ queryKey: ['backupSchedules'] });
      toast.success(
        'Schedule Created',
        `Cron backup (${sch.cron_expression || sch.cron_expr}) activated for ${sch.service_id}`
      );
      setIsScheduleModalOpen(false);
      resetScheduleForm();
    },
    onError: (err: Error) => toast.error('Creation Failed', err.message),
  });

  const updateScheduleMutation = useMutation({
    mutationFn: ({ id, is_enabled }: { id: string; is_enabled: boolean }) =>
      api.schedules.update(id, { is_enabled }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['backupSchedules'] });
      toast.success('Schedule Updated', 'Recurring backup schedule updated');
    },
    onError: (err: Error) => toast.error('Update Failed', err.message),
  });

  const deleteScheduleMutation = useMutation({
    mutationFn: (id: string) => api.schedules.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['backupSchedules'] });
      toast.success('Schedule Removed', 'Recurring cron backup deleted');
    },
    onError: (err: Error) => toast.error('Delete Failed', err.message),
  });

  const resetScheduleForm = () => {
    setScheduleServiceId('');
    setScheduleDbEngine('postgres');
    setSchedulePreset('0 0 * * *');
    setScheduleCustomCron('0 3 * * *');
    setScheduleRetentionDays(14);
    setScheduleDestinationId('');
    setScheduleIsEnabled(true);
  };

  const handleCreateSchedule = (e: React.FormEvent) => {
    e.preventDefault();
    const expr = schedulePreset === 'custom' ? scheduleCustomCron.trim() : schedulePreset;
    const dest =
      scheduleDestinationId ||
      destinations?.find((d) => d.is_default)?.id ||
      destinations?.[0]?.id;

    createScheduleMutation.mutate({
      service_id: scheduleServiceId.trim(),
      database_type: scheduleDbEngine,
      engine: scheduleDbEngine,
      cron_expression: expr,
      s3_destination_id: dest,
      retention_days: scheduleRetentionDays,
      is_enabled: scheduleIsEnabled,
    });
  };

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-zinc-100 flex items-center gap-2">
            <Archive className="h-5 w-5 text-cyan-400" />
            <span>Backups, Cron & Multi-DB S3 Streaming</span>
          </h1>
          <p className="text-xs text-zinc-400 mt-0.5">
            Automated recurring multi-database backups (PostgreSQL, MySQL, Redis, MongoDB) with S3 retention
          </p>
        </div>

        <div className="flex items-center gap-2">
          <Button
            variant="secondary"
            size="sm"
            onClick={() => setIsScheduleModalOpen(true)}
            leftIcon={<Calendar className="h-4 w-4 text-purple-400" />}
          >
            New Cron Schedule
          </Button>
          <Button
            variant="primary"
            size="sm"
            onClick={() => setIsCreateSnapshotModalOpen(true)}
            leftIcon={<Plus className="h-4 w-4" />}
          >
            Create Snapshot
          </Button>
        </div>
      </div>

      {/* Tabs */}
      <div className="flex items-center gap-1 border-b border-zinc-800 pb-2">
        <button
          onClick={() => setActiveTab('snapshots')}
          className={`flex items-center gap-2 px-3 py-1.5 rounded-lg text-xs font-medium transition-all ${
            activeTab === 'snapshots'
              ? 'bg-zinc-800 text-cyan-300 font-semibold'
              : 'text-zinc-400 hover:text-zinc-200'
          }`}
        >
          <Archive className="h-3.5 w-3.5" />
          <span>Snapshot Archives</span>
          <span className="text-[10px] bg-zinc-900 px-1.5 py-0.5 rounded text-zinc-400">
            {backups?.length || 0}
          </span>
        </button>

        <button
          onClick={() => setActiveTab('schedules')}
          className={`flex items-center gap-2 px-3 py-1.5 rounded-lg text-xs font-medium transition-all ${
            activeTab === 'schedules'
              ? 'bg-zinc-800 text-cyan-300 font-semibold'
              : 'text-zinc-400 hover:text-zinc-200'
          }`}
        >
          <Calendar className="h-3.5 w-3.5" />
          <span>Cron Schedules</span>
          <span className="text-[10px] bg-zinc-900 px-1.5 py-0.5 rounded text-zinc-400">
            {schedules?.length || 0}
          </span>
        </button>

        <button
          onClick={() => setActiveTab('destinations')}
          className={`flex items-center gap-2 px-3 py-1.5 rounded-lg text-xs font-medium transition-all ${
            activeTab === 'destinations'
              ? 'bg-zinc-800 text-cyan-300 font-semibold'
              : 'text-zinc-400 hover:text-zinc-200'
          }`}
        >
          <HardDrive className="h-3.5 w-3.5" />
          <span>S3 Storage Targets</span>
        </button>
      </div>

      {/* TAB 1: Snapshots */}
      {activeTab === 'snapshots' && (
        <Card className="p-0 overflow-hidden">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Snapshot ID / S3 Key</TableHead>
                <TableHead>Target Service</TableHead>
                <TableHead>Compressed Size</TableHead>
                <TableHead>Execution Time</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Created</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isBackupsError ? (
                <TableRow>
                  <TableCell colSpan={7} className="p-4">
                    <QueryErrorAlert
                      title="Failed to load backups"
                      error={backupsError}
                      onRetry={refetchBackups}
                    />
                  </TableCell>
                </TableRow>
              ) : isBackupsLoading ? (
                <TableRow>
                  <TableCell colSpan={7} className="text-center py-8 text-zinc-500">
                    Loading backup snapshots...
                  </TableCell>
                </TableRow>
              ) : safeBackups.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={7} className="text-center py-10 text-zinc-500">
                    No backup archives found. Click "Create Snapshot" or create a Cron schedule.
                  </TableCell>
                </TableRow>
              ) : (
                safeBackups.map((b) => (
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
                          onClick={() => {
                            setSelectedBackupForRestore(b);
                            setRestoreTargetServiceId(b.service_id);
                          }}
                          leftIcon={<RotateCcw className="h-3 w-3" />}
                        >
                          Restore
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          title="Delete"
                          onClick={() => {
                            if (confirm(`Delete backup snapshot ${b.id}?`)) {
                              deleteBackupMutation.mutate(b.id);
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
      )}

      {/* TAB 2: Schedules */}
      {activeTab === 'schedules' && (
        <Card className="p-0 overflow-hidden">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Target Database / Service</TableHead>
                <TableHead>Engine</TableHead>
                <TableHead>Cron Expression</TableHead>
                <TableHead>Retention</TableHead>
                <TableHead>Enabled</TableHead>
                <TableHead>Last / Next Run</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isSchedulesError ? (
                <TableRow>
                  <TableCell colSpan={7} className="p-4">
                    <QueryErrorAlert
                      title="Failed to load backup schedules"
                      error={schedulesError}
                      onRetry={refetchSchedules}
                    />
                  </TableCell>
                </TableRow>
              ) : isSchedulesLoading ? (
                <TableRow>
                  <TableCell colSpan={7} className="text-center py-8 text-zinc-500">
                    Loading backup schedules...
                  </TableCell>
                </TableRow>
              ) : safeSchedules.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={7} className="text-center py-10 text-zinc-500">
                    No recurring backup schedules found. Click "New Cron Schedule" to configure automated dumps.
                  </TableCell>
                </TableRow>
              ) : (
                safeSchedules.map((sch) => (
                  <TableRow key={sch.id}>
                    <TableCell className="font-semibold text-zinc-100">
                      <div className="flex items-center gap-2">
                        <Calendar className="h-4 w-4 text-purple-400 shrink-0" />
                        <span className="font-mono text-xs text-zinc-200">{sch.service_id}</span>
                      </div>
                    </TableCell>
                    <TableCell className="font-mono text-xs text-zinc-400 capitalize">
                      {sch.database_type || sch.engine || 'postgres'}
                    </TableCell>
                    <TableCell>
                      <Badge variant="outline" className="font-mono text-xs border-zinc-700 text-cyan-300">
                        {sch.cron_expression || sch.cron_expr}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-xs text-zinc-300">
                      {sch.retention_days ? `${sch.retention_days} days` : '14 days'}
                    </TableCell>
                    <TableCell>
                      <button
                        onClick={() =>
                          updateScheduleMutation.mutate({
                            id: sch.id,
                            is_enabled: !sch.is_enabled,
                          })
                        }
                        className="cursor-pointer"
                        title={sch.is_enabled ? 'Click to disable' : 'Click to enable'}
                      >
                        <Badge variant={sch.is_enabled ? 'success' : 'default'} dot>
                          {sch.is_enabled ? 'Active' : 'Paused'}
                        </Badge>
                      </button>
                    </TableCell>
                    <TableCell className="text-xs text-zinc-400">
                      <div>
                        Last:{' '}
                        {sch.last_run_at ? formatDate(sch.last_run_at) : 'Never'}
                      </div>
                      <div className="text-zinc-500 text-[10px]">
                        Next:{' '}
                        {sch.next_run_at ? formatDate(sch.next_run_at) : 'Calculated by cron'}
                      </div>
                    </TableCell>
                    <TableCell className="text-right">
                      <Button
                        variant="ghost"
                        size="sm"
                        title="Delete Schedule"
                        onClick={() => {
                          if (confirm(`Remove backup schedule for ${sch.service_id}?`)) {
                            deleteScheduleMutation.mutate(sch.id);
                          }
                        }}
                      >
                        <Trash2 className="h-3.5 w-3.5 text-rose-400" />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </Card>
      )}

      {/* TAB 3: Destinations */}
      {activeTab === 'destinations' && (
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          {isDestinationsError ? (
            <div className="md:col-span-3">
              <QueryErrorAlert
                title="Failed to load backup destinations"
                error={destinationsError}
                onRetry={refetchDestinations}
              />
            </div>
          ) : safeDestinations.length > 0 ? (
            safeDestinations.map((d) => (
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
                  <div className="text-xs font-semibold text-zinc-200">
                    Default S3 Bucket (pikpik-backups)
                  </div>
                  <div className="text-[11px] font-mono text-zinc-500">
                    Streaming S3-compatible multi-part snapshot target
                  </div>
                </div>
              </div>
              <Badge variant="success">Active</Badge>
            </Card>
          )}
        </div>
      )}

      {/* Create Backup Snapshot Modal */}
      <Modal
        isOpen={isCreateSnapshotModalOpen}
        onClose={() => setIsCreateSnapshotModalOpen(false)}
        title="Create Immediate Database Snapshot"
        description="Stream an immediate compressed backup dump directly into S3"
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            createSnapshotMutation.mutate(snapshotServiceId);
          }}
          className="space-y-4"
        >
          <div className="space-y-1.5">
            <label className="block text-xs font-medium text-zinc-300">Target Database</label>
            <select
              value={snapshotServiceId}
              onChange={(e) => setSnapshotServiceId(e.target.value)}
              className="w-full rounded-lg border border-zinc-800 bg-zinc-950 px-3.5 py-2 text-sm text-zinc-100 focus:border-cyan-500 focus:outline-none"
              required
            >
              <option value="">Select database...</option>
              {safeDatabases.map((db) => (
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
              onClick={() => setIsCreateSnapshotModalOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              variant="primary"
              size="sm"
              isLoading={createSnapshotMutation.isPending}
            >
              Start Snapshot Stream
            </Button>
          </div>
        </form>
      </Modal>

      {/* Create Cron Schedule Modal */}
      <Modal
        isOpen={isScheduleModalOpen}
        onClose={() => setIsScheduleModalOpen(false)}
        title="Cron Backup Schedule Builder"
        description="Configure automated recurring database backups with S3 retention policies"
        size="lg"
      >
        <form onSubmit={handleCreateSchedule} className="space-y-4">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <label className="block text-xs font-medium text-zinc-300">Target Service / DB</label>
              {safeDatabases.length > 0 ? (
                <select
                  value={scheduleServiceId}
                  onChange={(e) => {
                    setScheduleServiceId(e.target.value);
                    const selected = safeDatabases.find((d) => d.name === e.target.value);
                    if (selected) {
                      setScheduleDbEngine(selected.engine);
                    }
                  }}
                  className="w-full rounded-lg border border-zinc-800 bg-zinc-950 px-3.5 py-2 text-sm text-zinc-100 focus:border-cyan-500 focus:outline-none"
                  required
                >
                  <option value="">Select database...</option>
                  {safeDatabases.map((db) => (
                    <option key={db.id} value={db.name}>
                      {db.name} ({db.engine})
                    </option>
                  ))}
                </select>
              ) : (
                <Input
                  placeholder="e.g. postgres-prod"
                  value={scheduleServiceId}
                  onChange={(e) => setScheduleServiceId(e.target.value)}
                  required
                />
              )}
            </div>

            <div className="space-y-1.5">
              <label className="block text-xs font-medium text-zinc-300">Database Engine</label>
              <select
                value={scheduleDbEngine}
                onChange={(e) => setScheduleDbEngine(e.target.value as DatabaseEngine)}
                className="w-full rounded-lg border border-zinc-800 bg-zinc-950 px-3.5 py-2 text-sm text-zinc-100 focus:border-cyan-500 focus:outline-none"
              >
                {DB_ENGINES.map((eng) => (
                  <option key={eng.id} value={eng.id}>
                    {eng.name}
                  </option>
                ))}
              </select>
            </div>
          </div>

          {/* Schedule Presets */}
          <div className="space-y-2">
            <label className="block text-xs font-medium text-zinc-300 flex items-center gap-1.5">
              <Clock className="h-3.5 w-3.5 text-cyan-400" />
              Schedule Frequency
            </label>
            <div className="grid grid-cols-2 gap-2">
              {CRON_PRESETS.map((preset) => (
                <button
                  type="button"
                  key={preset.expr}
                  onClick={() => setSchedulePreset(preset.expr)}
                  className={`p-2.5 rounded-lg border text-left text-xs transition-all ${
                    schedulePreset === preset.expr
                      ? 'border-cyan-500 bg-cyan-950/40 text-cyan-300 font-medium'
                      : 'border-zinc-800 bg-zinc-950 text-zinc-400 hover:border-zinc-700'
                  }`}
                >
                  <div className="text-zinc-200">{preset.label}</div>
                  {preset.expr !== 'custom' && (
                    <div className="text-[10px] text-zinc-500 font-mono mt-0.5">
                      {preset.expr}
                    </div>
                  )}
                </button>
              ))}
            </div>
          </div>

          {schedulePreset === 'custom' && (
            <Input
              label="Custom 5-Field Cron Expression"
              placeholder="0 */4 * * *"
              value={scheduleCustomCron}
              onChange={(e) => setScheduleCustomCron(e.target.value)}
              required
            />
          )}

          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <label className="block text-xs font-medium text-zinc-300">Retention Days</label>
              <select
                value={scheduleRetentionDays}
                onChange={(e) => setScheduleRetentionDays(Number(e.target.value))}
                className="w-full rounded-lg border border-zinc-800 bg-zinc-950 px-3.5 py-2 text-xs text-zinc-100 focus:border-cyan-500 focus:outline-none"
              >
                <option value={7}>7 Days</option>
                <option value={14}>14 Days (Recommended)</option>
                <option value={30}>30 Days (1 Month)</option>
                <option value={90}>90 Days (Quarterly)</option>
                <option value={365}>365 Days (1 Year)</option>
              </select>
            </div>

            <div className="space-y-1.5">
              <label className="block text-xs font-medium text-zinc-300">S3 Destination</label>
              <select
                value={scheduleDestinationId}
                onChange={(e) => setScheduleDestinationId(e.target.value)}
                className="w-full rounded-lg border border-zinc-800 bg-zinc-950 px-3.5 py-2 text-xs text-zinc-100 focus:border-cyan-500 focus:outline-none"
              >
                {destinations && destinations.length > 0 ? (
                  destinations.map((d) => (
                    <option key={d.id} value={d.id}>
                      {d.name} ({d.bucket})
                    </option>
                  ))
                ) : (
                  <option value="">Default S3 (pikpik-backups)</option>
                )}
              </select>
            </div>
          </div>

          <div className="flex items-center gap-2 pt-1">
            <input
              type="checkbox"
              id="scheduleActiveModal"
              checked={scheduleIsEnabled}
              onChange={(e) => setScheduleIsEnabled(e.target.checked)}
              className="h-4 w-4 rounded border-zinc-700 bg-zinc-900 text-cyan-500 focus:ring-cyan-500"
            />
            <label htmlFor="scheduleActiveModal" className="text-xs text-zinc-300 select-none">
              Activate schedule immediately
            </label>
          </div>

          <div className="flex justify-end gap-2 pt-4 border-t border-zinc-800">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setIsScheduleModalOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              variant="primary"
              size="sm"
              isLoading={createScheduleMutation.isPending}
              leftIcon={<Calendar className="h-3.5 w-3.5" />}
            >
              Create Cron Schedule
            </Button>
          </div>
        </form>
      </Modal>

      {/* Restore Modal */}
      {selectedBackupForRestore && (
        <Modal
          isOpen={Boolean(selectedBackupForRestore)}
          onClose={() => setSelectedBackupForRestore(null)}
          title="Restore Multi-Database Snapshot"
          description={`Restoring ${selectedBackupForRestore.s3_key || selectedBackupForRestore.id}`}
        >
          <div className="space-y-4">
            <div className="space-y-1.5">
              <label className="block text-xs font-medium text-zinc-300">
                Target Restore Service
              </label>
              <select
                value={restoreTargetServiceId}
                onChange={(e) => setRestoreTargetServiceId(e.target.value)}
                className="w-full rounded-lg border border-zinc-800 bg-zinc-950 px-3.5 py-2 text-sm text-zinc-100 focus:border-cyan-500 focus:outline-none"
              >
                <option value={selectedBackupForRestore.service_id}>
                  {selectedBackupForRestore.service_id} (Original Source)
                </option>
                {databases
                  ?.filter((d) => d.name !== selectedBackupForRestore.service_id)
                  .map((d) => (
                    <option key={d.id} value={d.name}>
                      {d.name} ({d.engine})
                    </option>
                  ))}
              </select>
            </div>

            <div className="p-3 bg-amber-950/30 border border-amber-800/50 rounded-lg text-xs text-amber-300">
              Warning: Restoring will overwrite existing data inside target service{' '}
              <strong>{restoreTargetServiceId || selectedBackupForRestore.service_id}</strong>.
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
                    targetId: restoreTargetServiceId || selectedBackupForRestore.service_id,
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
