import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../lib/api';
import {
  Database,
  CreateDatabaseRequest,
  DatabaseEngine,
  CreateBackupScheduleRequest,
} from '../lib/types';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Badge } from '../components/ui/Badge';
import { Input } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { useToast } from '../components/ui/Toast';
import { QueryErrorAlert } from '../components/ui/QueryErrorAlert';
import { formatDate } from '../lib/utils';
import {
  Database as DbIcon,
  Plus,
  RotateCw,
  Trash2,
  Copy,
  Check,
  Calendar,
  Camera,
  Clock,
} from 'lucide-react';

const ENGINES: { id: DatabaseEngine; name: string; defaultPort: number }[] = [
  { id: 'postgres', name: 'PostgreSQL 16', defaultPort: 5432 },
  { id: 'mysql', name: 'MySQL 8.0', defaultPort: 3306 },
  { id: 'redis', name: 'Redis 7 (In-Memory)', defaultPort: 6379 },
  { id: 'mongodb', name: 'MongoDB 7', defaultPort: 27017 },
  { id: 'mariadb', name: 'MariaDB 11', defaultPort: 3306 },
];

const CRON_PRESETS = [
  { label: 'Daily (Midnight UTC)', expr: '0 0 * * *' },
  { label: 'Every 6 Hours', expr: '0 */6 * * *' },
  { label: 'Weekly (Sunday 00:00)', expr: '0 0 * * 0' },
  { label: 'Custom Cron', expr: 'custom' },
];

export function DatabasesView() {
  const queryClient = useQueryClient();
  const toast = useToast();

  const [isCreateModalOpen, setIsCreateModalOpen] = useState(false);
  const [copiedId, setCopiedId] = useState<string | null>(null);

  // Form states for Create DB
  const [name, setName] = useState('');
  const [engine, setEngine] = useState<DatabaseEngine>('postgres');
  const [dbName, setDbName] = useState('');
  const [username, setUsername] = useState('pikpik');
  const [password, setPassword] = useState('');

  // Schedule Modal State
  const [scheduleModalDb, setScheduleModalDb] = useState<Database | null>(null);
  const [cronPreset, setCronPreset] = useState('0 0 * * *');
  const [customCron, setCustomCron] = useState('0 2 * * *');
  const [retentionDays, setRetentionDays] = useState(14);
  const [scheduleEnabled, setScheduleEnabled] = useState(true);

  const {
    data: databases,
    isLoading,
    isError: isDatabasesError,
    error: databasesError,
    refetch: refetchDatabases,
  } = useQuery({
    queryKey: ['databases'],
    queryFn: api.databases.list,
  });

  const { data: destinations } = useQuery({
    queryKey: ['backupDestinations'],
    queryFn: api.backups.listDestinations,
  });

  const safeDatabases = databases || [];
  const safeDestinations = destinations || [];

  const createMutation = useMutation({
    mutationFn: (req: CreateDatabaseRequest) => api.databases.create(req),
    onSuccess: (newDb) => {
      queryClient.invalidateQueries({ queryKey: ['databases'] });
      toast.success('Database Initialized', `${newDb.name} (${newDb.engine}) is provisioning`);
      setIsCreateModalOpen(false);
      resetForm();
    },
    onError: (err: Error) => toast.error('Creation Failed', err.message),
  });

  const restartMutation = useMutation({
    mutationFn: (id: string) => api.databases.restart(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['databases'] });
      toast.success('Restarted', 'Database container restarting');
    },
    onError: (err: Error) => toast.error('Restart Failed', err.message),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.databases.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['databases'] });
      toast.success('Database Deleted', 'Database container removed');
    },
    onError: (err: Error) => toast.error('Delete Failed', err.message),
  });

  const quickBackupMutation = useMutation({
    mutationFn: (serviceId: string) => api.backups.create(serviceId),
    onSuccess: (b) => {
      queryClient.invalidateQueries({ queryKey: ['backups'] });
      toast.success('Backup Initiated', `Streaming snapshot started for ${b.service_id}`);
    },
    onError: (err: Error) => toast.error('Backup Failed', err.message),
  });

  const createScheduleMutation = useMutation({
    mutationFn: (req: CreateBackupScheduleRequest) => api.schedules.create(req),
    onSuccess: (newSch) => {
      queryClient.invalidateQueries({ queryKey: ['backupSchedules'] });
      toast.success('Schedule Configured', `Automated backup created for ${newSch.service_id}`);
      setScheduleModalDb(null);
    },
    onError: (err: Error) => toast.error('Failed to create schedule', err.message),
  });

  const resetForm = () => {
    setName('');
    setEngine('postgres');
    setDbName('');
    setUsername('pikpik');
    setPassword('');
  };

  const copyConnectionString = (db: Database) => {
    let conn = '';
    if (db.engine === 'postgres') {
      conn = `postgresql://${db.username}:${db.password || '••••••••'}@${db.host}:${db.port}/${db.database_name}`;
    } else if (db.engine === 'mysql' || db.engine === 'mariadb') {
      conn = `mysql://${db.username}:${db.password || '••••••••'}@${db.host}:${db.port}/${db.database_name}`;
    } else if (db.engine === 'redis') {
      conn = `redis://${db.host}:${db.port}`;
    } else if (db.engine === 'mongodb') {
      conn = `mongodb://${db.username}:${db.password || '••••••••'}@${db.host}:${db.port}/${db.database_name}`;
    }

    navigator.clipboard.writeText(conn);
    setCopiedId(db.id);
    toast.success('Copied Connection String', conn);
    setTimeout(() => setCopiedId(null), 2000);
  };

  const handleScheduleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!scheduleModalDb) return;

    const expr = cronPreset === 'custom' ? customCron.trim() : cronPreset;
    const defaultDest = destinations?.find((d) => d.is_default)?.id || destinations?.[0]?.id;

    createScheduleMutation.mutate({
      service_id: scheduleModalDb.name || scheduleModalDb.id,
      database_type: scheduleModalDb.engine,
      engine: scheduleModalDb.engine,
      cron_expression: expr,
      s3_destination_id: defaultDest,
      retention_days: retentionDays,
      is_enabled: scheduleEnabled,
    });
  };

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-zinc-100 flex items-center gap-2">
            <DbIcon className="h-5 w-5 text-cyan-400" />
            <span>Managed Databases</span>
          </h1>
          <p className="text-xs text-zinc-400 mt-0.5">
            Isolated SQL & NoSQL engines (Postgres, MySQL, Redis, Mongo) with automated cron backups
          </p>
        </div>

        <Button
          variant="primary"
          size="sm"
          onClick={() => setIsCreateModalOpen(true)}
          leftIcon={<Plus className="h-4 w-4" />}
        >
          New Database
        </Button>
      </div>

      {/* Grid */}
      {isDatabasesError ? (
        <QueryErrorAlert
          title="Failed to load managed databases"
          error={databasesError}
          onRetry={refetchDatabases}
        />
      ) : isLoading ? (
        <div className="text-center py-12 text-zinc-500 text-xs">Loading databases...</div>
      ) : !databases || databases.length === 0 ? (
        <Card className="text-center py-12">
          <DbIcon className="h-8 w-8 text-zinc-600 mx-auto mb-2" />
          <h3 className="text-sm font-semibold text-zinc-200">No Managed Databases</h3>
          <p className="text-xs text-zinc-500 mt-1 max-w-sm mx-auto">
            Provision Postgres, MySQL, Redis, or Mongo instances with high-performance persistent storage.
          </p>
          <div className="mt-4">
            <Button variant="primary" size="sm" onClick={() => setIsCreateModalOpen(true)}>
              Provision First Database
            </Button>
          </div>
        </Card>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {safeDatabases.map((db) => (
            <Card key={db.id} className="flex flex-col justify-between hover:border-zinc-700">
              <div>
                <div className="flex items-start justify-between">
                  <div className="flex items-center gap-2.5">
                    <div className="p-2.5 rounded-lg bg-zinc-800 text-purple-400">
                      <DbIcon className="h-5 w-5" />
                    </div>
                    <div>
                      <h3 className="text-sm font-bold text-zinc-100">{db.name}</h3>
                      <span className="text-[11px] text-zinc-500 font-mono capitalize">
                        {db.engine} • Port {db.port}
                      </span>
                    </div>
                  </div>
                  <Badge variant={db.status === 'running' ? 'success' : 'warning'} dot>
                    {db.status}
                  </Badge>
                </div>

                <div className="mt-4 space-y-2 text-xs">
                  <div className="flex justify-between py-1 border-b border-zinc-800/60 font-mono">
                    <span className="text-zinc-500">Host:</span>
                    <span className="text-zinc-300">{db.host}</span>
                  </div>
                  <div className="flex justify-between py-1 border-b border-zinc-800/60 font-mono">
                    <span className="text-zinc-500">Database:</span>
                    <span className="text-zinc-300">{db.database_name || 'main'}</span>
                  </div>
                  <div className="flex justify-between py-1 border-b border-zinc-800/60 font-mono">
                    <span className="text-zinc-500">Created:</span>
                    <span className="text-zinc-400">{formatDate(db.created_at)}</span>
                  </div>
                </div>
              </div>

              {/* Action Bar */}
              <div className="space-y-2 pt-4 mt-4 border-t border-zinc-800/60">
                <div className="flex items-center gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => quickBackupMutation.mutate(db.name || db.id)}
                    isLoading={quickBackupMutation.isPending}
                    leftIcon={<Camera className="h-3 w-3 text-cyan-400" />}
                    className="flex-1 text-xs"
                    title="Take snapshot now"
                  >
                    Snapshot
                  </Button>

                  <Button
                    variant="secondary"
                    size="sm"
                    onClick={() => setScheduleModalDb(db)}
                    leftIcon={<Calendar className="h-3 w-3 text-purple-400" />}
                    className="flex-1 text-xs"
                    title="Configure recurring cron schedule"
                  >
                    Schedule
                  </Button>
                </div>

                <div className="flex items-center justify-between gap-2 pt-1">
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => copyConnectionString(db)}
                    className="gap-1.5 text-xs text-zinc-400 hover:text-zinc-200"
                  >
                    {copiedId === db.id ? (
                      <Check className="h-3.5 w-3.5 text-emerald-400" />
                    ) : (
                      <Copy className="h-3.5 w-3.5" />
                    )}
                    <span>Copy URI</span>
                  </Button>

                  <div className="flex items-center gap-1">
                    <Button
                      variant="ghost"
                      size="sm"
                      title="Restart Database"
                      onClick={() => restartMutation.mutate(db.id)}
                    >
                      <RotateCw className="h-3.5 w-3.5" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      title="Delete"
                      onClick={() => {
                        if (confirm(`Delete database ${db.name}?`)) {
                          deleteMutation.mutate(db.id);
                        }
                      }}
                    >
                      <Trash2 className="h-3.5 w-3.5 text-rose-400" />
                    </Button>
                  </div>
                </div>
              </div>
            </Card>
          ))}
        </div>
      )}

      {/* Provision Database Modal */}
      <Modal
        isOpen={isCreateModalOpen}
        onClose={() => setIsCreateModalOpen(false)}
        title="Provision Managed Database"
        description="Deploy an isolated database engine to the cluster"
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            createMutation.mutate({
              name,
              engine,
              database_name: dbName || name,
              username,
              password,
            });
          }}
          className="space-y-4"
        >
          <Input
            label="Service Instance Name"
            placeholder="prod-postgres"
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
          />

          <div className="space-y-1.5">
            <label className="block text-xs font-medium text-zinc-300">Database Engine</label>
            <div className="grid grid-cols-2 sm:grid-cols-3 gap-2">
              {ENGINES.map((eng) => (
                <button
                  type="button"
                  key={eng.id}
                  onClick={() => setEngine(eng.id)}
                  className={`p-3 rounded-lg border text-left text-xs transition-all ${
                    engine === eng.id
                      ? 'border-cyan-500 bg-cyan-950/40 text-cyan-300 font-semibold'
                      : 'border-zinc-800 bg-zinc-950 text-zinc-400 hover:border-zinc-700'
                  }`}
                >
                  <div className="font-medium text-zinc-200">{eng.name}</div>
                  <div className="text-[10px] text-zinc-500 font-mono mt-0.5">Port {eng.defaultPort}</div>
                </button>
              ))}
            </div>
          </div>

          <Input
            label="Initial Database Name"
            placeholder="main_db"
            value={dbName}
            onChange={(e) => setDbName(e.target.value)}
          />

          <div className="grid grid-cols-2 gap-3">
            <Input
              label="Username"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
            />
            <Input
              label="Password"
              type="password"
              placeholder="Auto-generated if blank"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
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
              Provision Instance
            </Button>
          </div>
        </form>
      </Modal>

      {/* Cron Backup Schedule Builder Modal */}
      {scheduleModalDb && (
        <Modal
          isOpen={Boolean(scheduleModalDb)}
          onClose={() => setScheduleModalDb(null)}
          title={`Cron Backup Schedule: ${scheduleModalDb.name}`}
          description={`Automate recurring S3 snapshot dumps for ${scheduleModalDb.engine.toUpperCase()}`}
          size="lg"
        >
          <form onSubmit={handleScheduleSubmit} className="space-y-4">
            <div className="p-3 bg-zinc-900/90 rounded-lg border border-zinc-800 flex items-center justify-between text-xs">
              <div className="flex items-center gap-2">
                <DbIcon className="h-4 w-4 text-cyan-400" />
                <span className="font-semibold text-zinc-200">{scheduleModalDb.name}</span>
                <span className="text-zinc-500 font-mono">({scheduleModalDb.engine})</span>
              </div>
              <Badge variant="info">Multi-DB Cron</Badge>
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
                    onClick={() => setCronPreset(preset.expr)}
                    className={`p-2.5 rounded-lg border text-left text-xs transition-all ${
                      cronPreset === preset.expr
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

            {cronPreset === 'custom' && (
              <Input
                label="Custom 5-Field Cron Expression"
                placeholder="0 */4 * * *"
                value={customCron}
                onChange={(e) => setCustomCron(e.target.value)}
                required
              />
            )}

            {/* Retention and Flags */}
            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1.5">
                <label className="block text-xs font-medium text-zinc-300">Retention Days</label>
                <select
                  value={retentionDays}
                  onChange={(e) => setRetentionDays(Number(e.target.value))}
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
                  className="w-full rounded-lg border border-zinc-800 bg-zinc-950 px-3.5 py-2 text-xs text-zinc-100 focus:border-cyan-500 focus:outline-none"
                >
                  {safeDestinations.length > 0 ? (
                    safeDestinations.map((d) => (
                      <option key={d.id} value={d.id}>
                        {d.name} ({d.bucket})
                      </option>
                    ))
                  ) : (
                    <option value="default">Default S3 (pikpik-backups)</option>
                  )}
                </select>
              </div>
            </div>

            <div className="flex items-center gap-2 pt-1">
              <input
                type="checkbox"
                id="scheduleActive"
                checked={scheduleEnabled}
                onChange={(e) => setScheduleEnabled(e.target.checked)}
                className="h-4 w-4 rounded border-zinc-700 bg-zinc-900 text-cyan-500 focus:ring-cyan-500"
              />
              <label htmlFor="scheduleActive" className="text-xs text-zinc-300 select-none">
                Enable this recurring backup schedule immediately
              </label>
            </div>

            <div className="flex justify-end gap-2 pt-4 border-t border-zinc-800">
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => setScheduleModalDb(null)}
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
                Save Schedule
              </Button>
            </div>
          </form>
        </Modal>
      )}
    </div>
  );
}
