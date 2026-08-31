import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../lib/api';
import { CreateIntegrationRequest } from '../lib/types';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Badge } from '../components/ui/Badge';
import { Input } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { Tabs } from '../components/ui/Tabs';
import { useToast } from '../components/ui/Toast';
import { QueryErrorAlert } from '../components/ui/QueryErrorAlert';
import { formatDate } from '../lib/utils';
import {
  Link2,
  Plus,
  Trash2,
  GitBranch,
  Box,
  HardDrive,
  CheckCircle,
  AlertTriangle,
  Eye,
  EyeOff,
  Zap,
} from 'lucide-react';

const INTEGRATION_TYPES = [
  { id: 'git_github', name: 'GitHub App / PAT', category: 'git', icon: GitBranch, description: 'Auto-deploy webhooks, commit metadata, and branch synchronization' },
  { id: 'git_gitlab', name: 'GitLab CI / PAT', category: 'git', icon: GitBranch, description: 'GitLab repository webhook triggers and release pipeline automation' },
  { id: 'git_gitea', name: 'Gitea / Forgejo', category: 'git', icon: GitBranch, description: 'Self-hosted Git instance push events and lightweight runner integration' },
  { id: 'registry_dockerhub', name: 'Docker Hub', category: 'registry', icon: Box, description: 'Public and private image repository pulling with robot credential injection' },
  { id: 'registry_ghcr', name: 'GitHub Container Registry (GHCR)', category: 'registry', icon: Box, description: 'Private container package ingestion and swarm cluster image pulls' },
  { id: 'registry_ecr', name: 'AWS Elastic Container Registry (ECR)', category: 'registry', icon: Box, description: 'Amazon AWS ECR authorization tokens and automated secret renewal' },
  { id: 'storage_s3', name: 'AWS S3 / Compatible S3', category: 'storage', icon: HardDrive, description: 'Streaming database snapshot backups, multi-part uploads, and offsite disaster recovery' },
  { id: 'storage_r2', name: 'Cloudflare R2', category: 'storage', icon: HardDrive, description: 'Zero-egress database backup target with S3-compatible API semantics' },
  { id: 'storage_minio', name: 'MinIO Self-Hosted S3', category: 'storage', icon: HardDrive, description: 'On-premise high-performance object storage server for cluster backups' },
];

export function IntegrationsView() {
  const queryClient = useQueryClient();
  const toast = useToast();

  const [activeCategory, setActiveCategory] = useState('all');
  const [isAddModalOpen, setIsAddModalOpen] = useState(false);
  const [selectedType, setSelectedType] = useState('git_github');
  const [intName, setIntName] = useState('');
  const [credentials, setCredentials] = useState('');
  const [configJSON, setConfigJSON] = useState('{}');
  const [showCredentials, setShowCredentials] = useState(false);

  const [testingId, setTestingId] = useState<string | null>(null);
  const [testResults, setTestResults] = useState<Record<string, { success: boolean; message: string; latency_ms: number }>>({});

  const {
    data: integrations = [],
    isLoading,
    isError,
    error,
    refetch,
  } = useQuery({
    queryKey: ['integrations'],
    queryFn: () => api.integrations.list(),
  });

  const createMutation = useMutation({
    mutationFn: (req: CreateIntegrationRequest) => api.integrations.create(req),
    onSuccess: (newIt) => {
      queryClient.invalidateQueries({ queryKey: ['integrations'] });
      toast.success('Integration Added', `Registered '${newIt.name}' successfully`);
      setIsAddModalOpen(false);
      setIntName('');
      setCredentials('');
      setConfigJSON('{}');
    },
    onError: (err: Error) => toast.error('Creation Failed', err.message),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.integrations.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['integrations'] });
      toast.success('Integration Deleted', 'Provider integration removed');
    },
    onError: (err: Error) => toast.error('Deletion Failed', err.message),
  });

  const handleTestIntegration = async (id: string) => {
    setTestingId(id);
    try {
      const res = await api.integrations.test(id);
      setTestResults((prev) => ({ ...prev, [id]: res }));
      if (res.success) {
        toast.success('Connection Verified', `${res.message} (${res.latency_ms}ms)`);
      } else {
        toast.error('Connection Failed', res.message);
      }
      queryClient.invalidateQueries({ queryKey: ['integrations'] });
    } catch (err: any) {
      toast.error('Ping Failed', err?.message || 'Failed to test integration');
    } finally {
      setTestingId(null);
    }
  };

  const safeIntegrations = integrations || [];

  const filteredIntegrations = safeIntegrations.filter((it) => {
    if (activeCategory === 'git') return it.type.startsWith('git_');
    if (activeCategory === 'registry') return it.type.startsWith('registry_');
    if (activeCategory === 'storage') return it.type.startsWith('storage_');
    return true;
  });

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-zinc-100 flex items-center gap-2">
            <Link2 className="h-5 w-5 text-cyan-400" />
            <span>Integrations & External Providers</span>
          </h1>
          <p className="text-xs text-zinc-400 mt-0.5">
            Connect private registries, S3 storage buckets, and Git repository webhook endpoints
          </p>
        </div>

        <Button
          variant="primary"
          size="sm"
          onClick={() => setIsAddModalOpen(true)}
          leftIcon={<Plus className="h-4 w-4" />}
        >
          Add Integration
        </Button>
      </div>

      {/* Category Tabs */}
      <Tabs
        variant="pills"
        activeTab={activeCategory}
        onChange={setActiveCategory}
        tabs={[
          { id: 'all', label: 'All Integrations', count: safeIntegrations.length },
          { id: 'git', label: 'Git Providers', count: safeIntegrations.filter((i) => i.type.startsWith('git_')).length },
          { id: 'registry', label: 'Container Registries', count: safeIntegrations.filter((i) => i.type.startsWith('registry_')).length },
          { id: 'storage', label: 'Storage & S3', count: safeIntegrations.filter((i) => i.type.startsWith('storage_')).length },
        ]}
      />

      {isError && (
        <QueryErrorAlert
          error={error}
          title="Failed to load integrations"
          onRetry={() => refetch()}
        />
      )}

      {/* Integrations Grid */}
      {isLoading ? (
        <Card className="p-8 text-center text-zinc-500 text-xs">
          Loading configured integrations...
        </Card>
      ) : filteredIntegrations.length === 0 ? (
        <Card className="p-12 text-center text-zinc-500 text-xs space-y-3">
          <Link2 className="h-8 w-8 text-zinc-600 mx-auto" />
          <p className="font-semibold text-zinc-400">No integrations configured in this category</p>
          <p className="text-zinc-500 max-w-sm mx-auto">
            Connect your GitHub/GitLab repositories, private container registries, or S3 buckets to automate deployment pipelines.
          </p>
          <Button
            variant="outline"
            size="sm"
            onClick={() => setIsAddModalOpen(true)}
            className="mt-2"
          >
            <Plus className="h-4 w-4 mr-1 text-cyan-400" />
            Add First Integration
          </Button>
        </Card>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {filteredIntegrations.map((it) => {
            const meta = INTEGRATION_TYPES.find((t) => t.id === it.type) || {
              name: it.type,
              icon: Link2,
              description: 'External provider integration',
            };
            const Icon = meta.icon;
            const testResult = testResults[it.id];
            const isTesting = testingId === it.id;

            return (
              <Card key={it.id} className="p-5 flex flex-col justify-between space-y-4 border-zinc-800 bg-zinc-950/60">
                <div className="space-y-3">
                  <div className="flex items-start justify-between gap-3">
                    <div className="flex items-center gap-3">
                      <div className="p-2.5 rounded-xl bg-zinc-900 border border-zinc-800 text-cyan-400">
                        <Icon className="h-5 w-5" />
                      </div>
                      <div>
                        <h3 className="font-semibold text-sm text-zinc-100">{it.name}</h3>
                        <span className="font-mono text-[11px] text-zinc-500">{meta.name}</span>
                      </div>
                    </div>

                    <Badge
                      variant={it.status === 'active' ? 'success' : 'error'}
                      className="capitalize text-[10px]"
                    >
                      {it.status}
                    </Badge>
                  </div>

                  <p className="text-xs text-zinc-400 line-clamp-2">{meta.description}</p>

                  {testResult && (
                    <div
                      className={`p-2.5 rounded-lg border text-[11px] flex items-center gap-2 ${
                        testResult.success
                          ? 'bg-emerald-950/40 border-emerald-800/50 text-emerald-300'
                          : 'bg-rose-950/40 border-rose-800/50 text-rose-300'
                      }`}
                    >
                      {testResult.success ? (
                        <CheckCircle className="h-3.5 w-3.5 shrink-0 text-emerald-400" />
                      ) : (
                        <AlertTriangle className="h-3.5 w-3.5 shrink-0 text-rose-400" />
                      )}
                      <span className="truncate">{testResult.message}</span>
                      <span className="ml-auto font-mono text-[10px] opacity-75">{testResult.latency_ms}ms</span>
                    </div>
                  )}
                </div>

                <div className="pt-3 border-t border-zinc-800/60 flex items-center justify-between text-xs">
                  <span className="text-[11px] text-zinc-500 font-mono">
                    {formatDate(it.created_at)}
                  </span>

                  <div className="flex items-center gap-1.5">
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => handleTestIntegration(it.id)}
                      isLoading={isTesting}
                      leftIcon={<Zap className="h-3.5 w-3.5 text-cyan-400" />}
                      className="text-xs py-1 px-2.5"
                    >
                      Test
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => {
                        if (confirm(`Remove integration '${it.name}'?`)) {
                          deleteMutation.mutate(it.id);
                        }
                      }}
                      className="text-xs p-1.5 text-zinc-400 hover:text-rose-400"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                </div>
              </Card>
            );
          })}
        </div>
      )}

      {/* Add Integration Modal */}
      <Modal
        isOpen={isAddModalOpen}
        onClose={() => setIsAddModalOpen(false)}
        title="Add External Integration"
        description="Credentials are AES-256-GCM encrypted and strictly isolated in the hardware vault"
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            createMutation.mutate({
              name: intName,
              type: selectedType,
              credentials: credentials,
              config_json: configJSON || '{}',
            });
          }}
          className="space-y-4"
        >
          <div>
            <label className="block text-xs font-semibold text-zinc-300 mb-1.5">
              Select Provider Type
            </label>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-2 max-h-48 overflow-y-auto p-1 border border-zinc-800 rounded-lg bg-zinc-950">
              {INTEGRATION_TYPES.map((t) => {
                const Icon = t.icon;
                const isSelected = selectedType === t.id;
                return (
                  <div
                    key={t.id}
                    onClick={() => {
                      setSelectedType(t.id);
                      if (!intName) setIntName(t.name);
                    }}
                    className={`p-2.5 rounded-lg border text-left cursor-pointer transition-all flex items-center gap-2.5 ${
                      isSelected
                        ? 'border-cyan-500 bg-cyan-950/30 text-cyan-200'
                        : 'border-zinc-800 hover:border-zinc-700 bg-zinc-900/40 text-zinc-300'
                    }`}
                  >
                    <Icon className="h-4 w-4 shrink-0 text-cyan-400" />
                    <div className="truncate">
                      <div className="text-xs font-medium truncate">{t.name}</div>
                      <div className="text-[10px] text-zinc-500 capitalize">{t.category}</div>
                    </div>
                  </div>
                );
              })}
            </div>
          </div>

          <Input
            label="Integration Display Name"
            placeholder="e.g. Production GitHub PAT"
            value={intName}
            onChange={(e) => setIntName(e.target.value)}
            required
          />

          <div>
            <label className="block text-xs font-semibold text-zinc-300 mb-1.5">
              Secret Token / API Key / Password
            </label>
            <div className="relative">
              <input
                type={showCredentials ? 'text' : 'password'}
                placeholder="ghp_xxxx or Docker token or S3 secret key"
                value={credentials}
                onChange={(e) => setCredentials(e.target.value)}
                required
                className="w-full px-3 py-2 pr-10 bg-zinc-950 border border-zinc-800 rounded-lg text-xs font-mono text-zinc-200 focus:outline-none focus:border-cyan-500 transition-colors"
              />
              <button
                type="button"
                onClick={() => setShowCredentials(!showCredentials)}
                className="absolute right-3 top-2.5 text-zinc-500 hover:text-zinc-300"
              >
                {showCredentials ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
              </button>
            </div>
            <p className="text-[10px] text-zinc-500 mt-1">
              Encrypted at rest with AES-256-GCM. Never exposed in unauthenticated API responses.
            </p>
          </div>

          <div>
            <label className="block text-xs font-semibold text-zinc-300 mb-1.5">
              Configuration JSON (Optional)
            </label>
            <textarea
              rows={2}
              value={configJSON}
              onChange={(e) => setConfigJSON(e.target.value)}
              placeholder='{"username": "devops", "region": "us-east-1"}'
              className="w-full px-3 py-2 bg-zinc-950 border border-zinc-800 rounded-lg text-xs font-mono text-zinc-300 focus:outline-none focus:border-cyan-500 transition-colors resize-none"
            />
          </div>

          <div className="flex justify-end gap-2 pt-4 border-t border-zinc-800">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setIsAddModalOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              variant="primary"
              size="sm"
              isLoading={createMutation.isPending}
            >
              Save Integration
            </Button>
          </div>
        </form>
      </Modal>
    </div>
  );
}
