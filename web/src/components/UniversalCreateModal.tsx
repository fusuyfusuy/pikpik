import React, { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../lib/api';
import { Template } from '../lib/types';
import { Modal } from './ui/Modal';
import { Button } from './ui/Button';
import { Input } from './ui/Input';
import { Badge } from './ui/Badge';
import { useToast } from './ui/Toast';
import {
  GitBranch,
  Box,
  Database as DbIcon,
  Layers,
  Store,
  Sparkles,
  Search,
  Server,
  Key,
} from 'lucide-react';

export interface UniversalCreateModalProps {
  isOpen: boolean;
  onClose: () => void;
  onNavigate?: (view: string) => void;
  defaultTab?: 'git' | 'image' | 'database' | 'stack' | 'template';
  defaultProjectId?: string;
}

type CreateTab = 'git' | 'image' | 'database' | 'stack' | 'template';

const DATABASE_PRESETS = [
  { id: 'postgres', name: 'PostgreSQL', version: '16-alpine', port: 5432, icon: '🐘', color: 'from-blue-500/20 to-blue-600/10' },
  { id: 'mysql', name: 'MySQL', version: '8.0-debian', port: 3306, icon: '🐬', color: 'from-amber-500/20 to-amber-600/10' },
  { id: 'mariadb', name: 'MariaDB', version: '11.2', port: 3306, icon: '🦭', color: 'from-orange-500/20 to-orange-600/10' },
  { id: 'redis', name: 'Redis', version: '7.2-alpine', port: 6379, icon: '⚡', color: 'from-rose-500/20 to-rose-600/10' },
  { id: 'mongodb', name: 'MongoDB', version: '7.0', port: 27017, icon: '🍃', color: 'from-emerald-500/20 to-emerald-600/10' },
  { id: 'clickhouse', name: 'ClickHouse', version: '24.1', port: 8123, icon: '📊', color: 'from-yellow-500/20 to-yellow-600/10' },
  { id: 'pocketbase', name: 'PocketBase', version: '0.22', port: 8090, icon: '📦', color: 'from-cyan-500/20 to-cyan-600/10' },
];

export function UniversalCreateModal({
  isOpen,
  onClose,
  onNavigate,
  defaultTab = 'git',
  defaultProjectId = 'prj_default',
}: UniversalCreateModalProps) {
  const queryClient = useQueryClient();
  const toast = useToast();

  const [activeTab, setActiveTab] = useState<CreateTab>(defaultTab);
  const [projectId, setProjectId] = useState(defaultProjectId);

  // 1. Git App State
  const [gitName, setGitName] = useState('');
  const [gitRepoUrl, setGitRepoUrl] = useState('');
  const [gitBranch, setGitBranch] = useState('main');
  const [gitPort, setGitPort] = useState('3000');
  const [gitDomain, setGitDomain] = useState('');
  const [gitStrategy, setGitStrategy] = useState<'dockerfile' | 'nixpacks' | 'compose'>('dockerfile');
  const [gitEnv, setGitEnv] = useState('NODE_ENV=production\nPORT=3000');

  // 2. Docker Image State
  const [imageName, setImageName] = useState('');
  const [imageTag, setImageTag] = useState('');
  const [imagePort, setImagePort] = useState('80');
  const [imageReplicas, setImageReplicas] = useState(1);
  const [imageDomain, setImageDomain] = useState('');

  // 3. Database State
  const [selectedDbType, setSelectedDbType] = useState('postgres');
  const [dbName, setDbName] = useState('production-db');
  const [dbUser, setDbUser] = useState('pikpik');
  const [dbPassword, setDbPassword] = useState(() => Math.random().toString(36).slice(-10) + '!');

  // 4. Compose Stack State
  const [stackName, setStackName] = useState('');
  const [stackComposeYaml, setStackComposeYaml] = useState(`services:
  web:
    image: nginx:alpine
    ports:
      - "80:80"
    restart: always`);

  // 5. Template State
  const [templateSearch, setTemplateSearch] = useState('');
  const [selectedTemplate, setSelectedTemplate] = useState<Template | null>(null);

  // Queries
  const { data: projects = [] } = useQuery({
    queryKey: ['projects'],
    queryFn: () => api.projects.list(),
    enabled: isOpen,
  });

  const { data: templates = [] } = useQuery({
    queryKey: ['templates', templateSearch],
    queryFn: () => api.templates.list(undefined, templateSearch),
    enabled: isOpen && activeTab === 'template',
  });

  // Mutations
  const createAppMutation = useMutation({
    mutationFn: (req: any) => api.apps.create(req),
    onSuccess: (newApp) => {
      queryClient.invalidateQueries({ queryKey: ['apps'] });
      queryClient.invalidateQueries({ queryKey: ['projects'] });
      toast.success('Workload Created', `Application '${newApp.name}' is ready`);
      onClose();
      if (onNavigate) onNavigate('apps');
    },
    onError: (err: Error) => toast.error('Creation Failed', err.message),
  });

  const createDbMutation = useMutation({
    mutationFn: (req: any) => api.databases.create(req),
    onSuccess: (newDb) => {
      queryClient.invalidateQueries({ queryKey: ['databases'] });
      queryClient.invalidateQueries({ queryKey: ['projects'] });
      toast.success('Database Provisioned', `Managed database '${newDb.name}' is online`);
      onClose();
      if (onNavigate) onNavigate('databases');
    },
    onError: (err: Error) => toast.error('Creation Failed', err.message),
  });

  const createStackMutation = useMutation({
    mutationFn: (req: any) => api.stacks.create(req),
    onSuccess: (newStack) => {
      queryClient.invalidateQueries({ queryKey: ['stacks'] });
      queryClient.invalidateQueries({ queryKey: ['projects'] });
      toast.success('Stack Created', `Compose stack '${newStack.name}' instantiated`);
      onClose();
      if (onNavigate) onNavigate('stacks');
    },
    onError: (err: Error) => toast.error('Creation Failed', err.message),
  });

  const deployTemplateMutation = useMutation({
    mutationFn: ({ id, req }: { id: string; req: any }) => api.templates.deploy(id, req),
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ['apps'] });
      queryClient.invalidateQueries({ queryKey: ['projects'] });
      toast.success('Template Deployed', `Recipe deployed as '${res.name}'`);
      onClose();
      if (onNavigate) onNavigate('apps');
    },
    onError: (err: Error) => toast.error('Deployment Failed', err.message),
  });

  const parseEnvLines = (raw: string): Record<string, string> => {
    const env: Record<string, string> = {};
    raw.split('\n').forEach((line) => {
      const trimmed = line.trim();
      if (trimmed && !trimmed.startsWith('#')) {
        const idx = trimmed.indexOf('=');
        if (idx !== -1) {
          const k = trimmed.slice(0, idx).trim();
          const v = trimmed.slice(idx + 1).trim();
          if (k) env[k] = v;
        }
      }
    });
    return env;
  };

  const handleGitSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    createAppMutation.mutate({
      project_id: projectId,
      name: gitName || 'git-app',
      image: '',
      git_repo_url: gitRepoUrl,
      git_branch: gitBranch,
      build_strategy: gitStrategy,
      container_port: parseInt(gitPort, 10) || 3000,
      domains: gitDomain ? [gitDomain] : [],
      env: parseEnvLines(gitEnv),
    });
  };

  const handleImageSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    createAppMutation.mutate({
      project_id: projectId,
      name: imageName || 'image-app',
      image: imageTag,
      container_port: parseInt(imagePort, 10) || 80,
      replicas: imageReplicas,
      domains: imageDomain ? [imageDomain] : [],
    });
  };

  const handleDbSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const preset = DATABASE_PRESETS.find((p) => p.id === selectedDbType);
    createDbMutation.mutate({
      project_id: projectId,
      name: dbName,
      type: selectedDbType,
      version: preset?.version || 'latest',
      port: preset?.port || 5432,
      user: dbUser,
      password: dbPassword,
    });
  };

  const handleStackSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    createStackMutation.mutate({
      project_id: projectId,
      name: stackName || 'compose-stack',
      compose_yaml: stackComposeYaml,
    });
  };

  const handleTemplateSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedTemplate) return;
    deployTemplateMutation.mutate({
      id: selectedTemplate.id,
      req: {
        project_id: projectId,
        name: selectedTemplate.id + '-' + Math.random().toString(36).slice(-4),
        auto_generate_missing: true,
      },
    });
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title="Universal Resource Provisioner"
      description="Deploy applications, managed databases, Compose stacks, or 1-click recipes"
      className="max-w-2xl"
    >
      <div className="space-y-4">
        {/* Navigation Tabs */}
        <div className="grid grid-cols-5 gap-1.5 p-1 bg-zinc-950 border border-zinc-800 rounded-xl">
          <button
            type="button"
            onClick={() => setActiveTab('git')}
            className={`flex flex-col items-center gap-1 py-2 px-1 rounded-lg text-xs font-medium transition-all ${
              activeTab === 'git'
                ? 'bg-zinc-800 text-cyan-400 shadow-sm'
                : 'text-zinc-400 hover:text-zinc-200 hover:bg-zinc-900'
            }`}
          >
            <GitBranch className="h-4 w-4" />
            <span>Git Auto-Deploy</span>
          </button>
          <button
            type="button"
            onClick={() => setActiveTab('image')}
            className={`flex flex-col items-center gap-1 py-2 px-1 rounded-lg text-xs font-medium transition-all ${
              activeTab === 'image'
                ? 'bg-zinc-800 text-cyan-400 shadow-sm'
                : 'text-zinc-400 hover:text-zinc-200 hover:bg-zinc-900'
            }`}
          >
            <Box className="h-4 w-4" />
            <span>Docker Image</span>
          </button>
          <button
            type="button"
            onClick={() => setActiveTab('database')}
            className={`flex flex-col items-center gap-1 py-2 px-1 rounded-lg text-xs font-medium transition-all ${
              activeTab === 'database'
                ? 'bg-zinc-800 text-cyan-400 shadow-sm'
                : 'text-zinc-400 hover:text-zinc-200 hover:bg-zinc-900'
            }`}
          >
            <DbIcon className="h-4 w-4" />
            <span>1-Click DB</span>
          </button>
          <button
            type="button"
            onClick={() => setActiveTab('stack')}
            className={`flex flex-col items-center gap-1 py-2 px-1 rounded-lg text-xs font-medium transition-all ${
              activeTab === 'stack'
                ? 'bg-zinc-800 text-cyan-400 shadow-sm'
                : 'text-zinc-400 hover:text-zinc-200 hover:bg-zinc-900'
            }`}
          >
            <Layers className="h-4 w-4" />
            <span>Compose Stack</span>
          </button>
          <button
            type="button"
            onClick={() => setActiveTab('template')}
            className={`flex flex-col items-center gap-1 py-2 px-1 rounded-lg text-xs font-medium transition-all ${
              activeTab === 'template'
                ? 'bg-zinc-800 text-cyan-400 shadow-sm'
                : 'text-zinc-400 hover:text-zinc-200 hover:bg-zinc-900'
            }`}
          >
            <Store className="h-4 w-4" />
            <span>Marketplace</span>
          </button>
        </div>

        {/* Project Target Selector */}
        <div className="flex items-center justify-between p-2.5 bg-zinc-950 border border-zinc-800 rounded-lg text-xs">
          <span className="text-zinc-400 flex items-center gap-1.5 font-medium">
            <Server className="h-3.5 w-3.5 text-cyan-400" />
            <span>Target Workspace / Project:</span>
          </span>
          <select
            value={projectId}
            onChange={(e) => setProjectId(e.target.value)}
            className="bg-zinc-900 border border-zinc-700 text-zinc-200 rounded px-2 py-1 text-xs focus:outline-none focus:border-cyan-500"
          >
            {projects.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name} ({p.id})
              </option>
            ))}
          </select>
        </div>

        {/* Tab 1: Git Auto-Deploy Form */}
        {activeTab === 'git' && (
          <form onSubmit={handleGitSubmit} className="space-y-3.5">
            <div className="grid grid-cols-2 gap-3">
              <Input
                label="Application Name"
                placeholder="e.g. web-frontend"
                value={gitName}
                onChange={(e) => setGitName(e.target.value)}
                required
              />
              <Input
                label="Target Container Port"
                placeholder="3000"
                value={gitPort}
                onChange={(e) => setGitPort(e.target.value)}
                required
              />
            </div>

            <div className="grid grid-cols-3 gap-3">
              <div className="col-span-2">
                <Input
                  label="Git Repository URL"
                  placeholder="https://github.com/org/repo.git"
                  value={gitRepoUrl}
                  onChange={(e) => setGitRepoUrl(e.target.value)}
                  required
                />
              </div>
              <Input
                label="Branch Ref"
                placeholder="main"
                value={gitBranch}
                onChange={(e) => setGitBranch(e.target.value)}
                required
              />
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="block text-xs font-medium text-zinc-300 mb-1.5">Build Engine</label>
                <select
                  value={gitStrategy}
                  onChange={(e) => setGitStrategy(e.target.value as any)}
                  className="w-full bg-zinc-950 border border-zinc-800 text-zinc-200 rounded-lg px-3 py-2 text-xs focus:border-cyan-500 focus:outline-none"
                >
                  <option value="dockerfile">Standard Dockerfile</option>
                  <option value="nixpacks">Nixpacks (Auto-detect Language)</option>
                  <option value="compose">Docker Compose Builder</option>
                </select>
              </div>
              <Input
                label="Custom Domain (Auto-TLS)"
                placeholder="app.example.com"
                value={gitDomain}
                onChange={(e) => setGitDomain(e.target.value)}
              />
            </div>

            <div>
              <label className="block text-xs font-medium text-zinc-300 mb-1.5">Environment Variables (.env format)</label>
              <textarea
                value={gitEnv}
                onChange={(e) => setGitEnv(e.target.value)}
                rows={3}
                className="w-full bg-zinc-950 border border-zinc-800 text-cyan-300 font-mono text-xs rounded-lg p-2.5 focus:border-cyan-500 focus:outline-none"
                placeholder="KEY=VALUE&#10;DATABASE_URL=postgres://..."
              />
            </div>

            <div className="flex justify-end gap-2 pt-3 border-t border-zinc-800">
              <Button type="button" variant="outline" size="sm" onClick={onClose}>
                Cancel
              </Button>
              <Button type="submit" variant="primary" size="sm" isLoading={createAppMutation.isPending}>
                <Sparkles className="h-3.5 w-3.5 mr-1" />
                Deploy from Git
              </Button>
            </div>
          </form>
        )}

        {/* Tab 2: Docker Image Form */}
        {activeTab === 'image' && (
          <form onSubmit={handleImageSubmit} className="space-y-3.5">
            <div className="grid grid-cols-2 gap-3">
              <Input
                label="Application Name"
                placeholder="e.g. api-service"
                value={imageName}
                onChange={(e) => setImageName(e.target.value)}
                required
              />
              <Input
                label="Docker Image Tag"
                placeholder="e.g. ghcr.io/org/api:v1.2.0"
                value={imageTag}
                onChange={(e) => setImageTag(e.target.value)}
                required
              />
            </div>

            <div className="grid grid-cols-3 gap-3">
              <Input
                label="Container Port"
                placeholder="8080"
                value={imagePort}
                onChange={(e) => setImagePort(e.target.value)}
                required
              />
              <div>
                <label className="block text-xs font-medium text-zinc-300 mb-1.5">Replicas</label>
                <input
                  type="number"
                  min="1"
                  max="20"
                  value={imageReplicas}
                  onChange={(e) => setImageReplicas(parseInt(e.target.value, 10) || 1)}
                  className="w-full bg-zinc-950 border border-zinc-800 text-zinc-200 rounded-lg px-3 py-2 text-xs focus:border-cyan-500 focus:outline-none"
                />
              </div>
              <Input
                label="Domain (Optional)"
                placeholder="api.example.com"
                value={imageDomain}
                onChange={(e) => setImageDomain(e.target.value)}
              />
            </div>

            <div className="flex justify-end gap-2 pt-3 border-t border-zinc-800">
              <Button type="button" variant="outline" size="sm" onClick={onClose}>
                Cancel
              </Button>
              <Button type="submit" variant="primary" size="sm" isLoading={createAppMutation.isPending}>
                <Box className="h-3.5 w-3.5 mr-1" />
                Launch Image
              </Button>
            </div>
          </form>
        )}

        {/* Tab 3: Managed Database Preset Selector */}
        {activeTab === 'database' && (
          <form onSubmit={handleDbSubmit} className="space-y-3.5">
            <div>
              <label className="block text-xs font-medium text-zinc-300 mb-2">Select Database Engine</label>
              <div className="grid grid-cols-4 gap-2">
                {DATABASE_PRESETS.map((p) => {
                  const isSelected = selectedDbType === p.id;
                  return (
                    <button
                      key={p.id}
                      type="button"
                      onClick={() => setSelectedDbType(p.id)}
                      className={`p-2.5 rounded-xl border text-left flex flex-col items-center gap-1 transition-all ${
                        isSelected
                          ? 'border-cyan-500 bg-cyan-950/30 text-zinc-100 ring-1 ring-cyan-500/50'
                          : 'border-zinc-800 bg-zinc-950 text-zinc-400 hover:border-zinc-700'
                      }`}
                    >
                      <span className="text-xl">{p.icon}</span>
                      <span className="text-xs font-semibold">{p.name}</span>
                      <span className="text-[10px] text-zinc-500">{p.version}</span>
                    </button>
                  );
                })}
              </div>
            </div>

            <div className="grid grid-cols-3 gap-3">
              <Input
                label="Database Service Name"
                placeholder="production-db"
                value={dbName}
                onChange={(e) => setDbName(e.target.value)}
                required
              />
              <Input
                label="Root Username"
                placeholder="pikpik"
                value={dbUser}
                onChange={(e) => setDbUser(e.target.value)}
                required
              />
              <Input
                label="Auto-Generated Password"
                placeholder="Secure password"
                value={dbPassword}
                onChange={(e) => setDbPassword(e.target.value)}
                required
              />
            </div>

            <div className="p-3 bg-zinc-950 border border-zinc-800 rounded-lg text-[11px] text-zinc-400 space-y-1">
              <div className="flex items-center gap-1.5 text-zinc-300 font-semibold">
                <Key className="h-3.5 w-3.5 text-amber-400" />
                <span>Zero-Configuration Credentials:</span>
              </div>
              <p>Persistent storage volume will be auto-mounted under <code>/var/lib/data</code> with automated hourly snapshot readiness.</p>
            </div>

            <div className="flex justify-end gap-2 pt-3 border-t border-zinc-800">
              <Button type="button" variant="outline" size="sm" onClick={onClose}>
                Cancel
              </Button>
              <Button type="submit" variant="primary" size="sm" isLoading={createDbMutation.isPending}>
                <DbIcon className="h-3.5 w-3.5 mr-1" />
                Provision Database
              </Button>
            </div>
          </form>
        )}

        {/* Tab 4: Compose Stack Form */}
        {activeTab === 'stack' && (
          <form onSubmit={handleStackSubmit} className="space-y-3.5">
            <Input
              label="Stack Name"
              placeholder="e.g. backend-stack"
              value={stackName}
              onChange={(e) => setStackName(e.target.value)}
              required
            />

            <div>
              <label className="block text-xs font-medium text-zinc-300 mb-1.5">Compose Specification (YAML)</label>
              <textarea
                value={stackComposeYaml}
                onChange={(e) => setStackComposeYaml(e.target.value)}
                rows={6}
                className="w-full bg-zinc-950 border border-zinc-800 text-emerald-400 font-mono text-xs rounded-lg p-2.5 focus:border-cyan-500 focus:outline-none"
                required
              />
            </div>

            <div className="flex justify-end gap-2 pt-3 border-t border-zinc-800">
              <Button type="button" variant="outline" size="sm" onClick={onClose}>
                Cancel
              </Button>
              <Button type="submit" variant="primary" size="sm" isLoading={createStackMutation.isPending}>
                <Layers className="h-3.5 w-3.5 mr-1" />
                Deploy Stack
              </Button>
            </div>
          </form>
        )}

        {/* Tab 5: Marketplace Recipes */}
        {activeTab === 'template' && (
          <form onSubmit={handleTemplateSubmit} className="space-y-3.5">
            <div className="relative">
              <Search className="absolute left-3 top-2.5 h-3.5 w-3.5 text-zinc-500" />
              <input
                type="text"
                placeholder="Search 22 curated recipes (PocketBase, MinIO, Vaultwarden, Ghost...)"
                value={templateSearch}
                onChange={(e) => setTemplateSearch(e.target.value)}
                className="w-full pl-9 pr-3 py-2 bg-zinc-950 border border-zinc-800 rounded-lg text-xs text-zinc-200 focus:outline-none focus:border-cyan-500"
              />
            </div>

            <div className="grid grid-cols-2 gap-2 max-h-56 overflow-y-auto pr-1">
              {templates.map((tpl) => {
                const isSelected = selectedTemplate?.id === tpl.id;
                return (
                  <div
                    key={tpl.id}
                    onClick={() => setSelectedTemplate(tpl)}
                    className={`p-2.5 rounded-xl border cursor-pointer transition-all flex items-start gap-2.5 ${
                      isSelected
                        ? 'border-cyan-500 bg-cyan-950/30 ring-1 ring-cyan-500/50'
                        : 'border-zinc-800 bg-zinc-950 hover:border-zinc-700'
                    }`}
                  >
                    <span className="text-xl shrink-0 mt-0.5">{tpl.icon || '📦'}</span>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center justify-between">
                        <span className="text-xs font-semibold text-zinc-100 truncate">{tpl.name}</span>
                        <Badge variant="outline" className="text-[10px] capitalize">
                          {tpl.category}
                        </Badge>
                      </div>
                      <p className="text-[11px] text-zinc-400 line-clamp-1 mt-0.5">{tpl.description}</p>
                    </div>
                  </div>
                );
              })}
            </div>

            <div className="flex justify-end gap-2 pt-3 border-t border-zinc-800">
              <Button type="button" variant="outline" size="sm" onClick={onClose}>
                Cancel
              </Button>
              <Button
                type="submit"
                variant="primary"
                size="sm"
                disabled={!selectedTemplate}
                isLoading={deployTemplateMutation.isPending}
              >
                <Store className="h-3.5 w-3.5 mr-1" />
                Deploy {selectedTemplate?.name || 'Recipe'}
              </Button>
            </div>
          </form>
        )}
      </div>
    </Modal>
  );
}
