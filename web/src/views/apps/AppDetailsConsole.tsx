import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../../lib/api';
import { App, Project, UpdateAppRequest } from '../../lib/types';
import { Tabs } from '../../components/ui/Tabs';
import { Button } from '../../components/ui/Button';
import { Badge } from '../../components/ui/Badge';
import { useToast } from '../../components/ui/Toast';
import { TopologyContainersTab } from './TopologyContainersTab';
import { TwoColumnConfigEditor } from './TwoColumnConfigEditor';
import { DeploymentsTrafficTab } from './DeploymentsTrafficTab';
import { LiveLogsTab } from './LiveLogsTab';
import { TerminalTab } from './TerminalTab';
import {
  Box,
  ArrowLeft,
  Play,
  RotateCw,
  Square,
  Trash2,
  MoveRight,
  ExternalLink,
  Cpu,
  Sliders,
  FileText,
  Terminal as TerminalIcon,
  Zap,
  Folder,
  Globe,
} from 'lucide-react';

export type AppConsoleTabType = 'topology' | 'config' | 'deployments' | 'logs' | 'terminal';

export interface AppDetailsConsoleProps {
  app: App;
  projects: Project[];
  onBack: () => void;
  onMoveApp: (app: App) => void;
  onDeleteApp: (appId: string) => void;
  initialTab?: AppConsoleTabType;
}

export function AppDetailsConsole({
  app,
  projects,
  onBack,
  onMoveApp,
  onDeleteApp,
  initialTab = 'topology',
}: AppDetailsConsoleProps) {
  const queryClient = useQueryClient();
  const toast = useToast();
  const [activeTab, setActiveTab] = useState<AppConsoleTabType>(initialTab);

  // Deploy Mutation
  const deployMutation = useMutation({
    mutationFn: (id: string) => api.apps.deploy(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['apps'] });
      toast.success('Zero-Downtime Rollout', `Rolling deployment initiated for ${app.name}`);
    },
    onError: (err: Error) => toast.error('Deploy Failed', err.message),
  });

  // Restart Mutation
  const restartMutation = useMutation({
    mutationFn: (id: string) => api.apps.restart(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['apps'] });
      toast.success('Restart Complete', `Restarted service containers for ${app.name}`);
    },
    onError: (err: Error) => toast.error('Restart Failed', err.message),
  });

  // Stop Mutation
  const stopMutation = useMutation({
    mutationFn: (id: string) => api.apps.stop(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['apps'] });
      toast.info('Service Stopped', `${app.name} scaled to 0 replicas`);
    },
    onError: (err: Error) => toast.error('Stop Failed', err.message),
  });

  // Start Mutation
  const startMutation = useMutation({
    mutationFn: (id: string) => api.apps.start(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['apps'] });
      toast.success('Service Started', `${app.name} restored to target replicas`);
    },
    onError: (err: Error) => toast.error('Start Failed', err.message),
  });

  // Update mutation (for TwoColumnConfigEditor)
  const updateMutation = useMutation({
    mutationFn: (req: UpdateAppRequest) => api.apps.update(app.id, req),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['apps'] });
      toast.success('Configuration Saved', 'Application settings updated');
    },
    onError: (err: Error) => toast.error('Save Failed', err.message),
  });

  const handleConfigSaveAndRedeploy = async (req: UpdateAppRequest) => {
    try {
      await api.apps.update(app.id, req);
      await api.apps.deploy(app.id);
      queryClient.invalidateQueries({ queryKey: ['apps'] });
      toast.success('Redeployed', 'Saved changes and initiated rolling rollout');
      setActiveTab('topology');
    } catch (err: any) {
      toast.error('Redeploy Failed', err?.message || 'Failed to save and redeploy');
    }
  };

  const handleConfigSaveChanges = async (req: UpdateAppRequest) => {
    await updateMutation.mutateAsync(req);
  };

  const isRunning = app.status === 'running';
  const project = projects.find((p) => p.id === app.project_id);

  return (
    <div className="space-y-6">
      {/* Top Header & Breadcrumb */}
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 border-b border-zinc-800 pb-5">
        <div className="space-y-2">
          {/* Breadcrumb / Back Button */}
          <div className="flex items-center gap-2 text-xs text-zinc-400">
            <button
              onClick={onBack}
              className="flex items-center gap-1 text-zinc-400 hover:text-cyan-400 font-medium transition-colors cursor-pointer"
            >
              <ArrowLeft className="h-3.5 w-3.5" />
              <span>Applications</span>
            </button>
            <span className="text-zinc-600">/</span>
            <span className="flex items-center gap-1 text-zinc-400">
              <Folder className="h-3 w-3 text-cyan-400" />
              <span>{project?.name || app.project_name || app.project_id}</span>
            </span>
            <span className="text-zinc-600">/</span>
            <span className="font-semibold text-zinc-100 font-mono">{app.name}</span>
          </div>

          {/* App Title & Identity */}
          <div className="flex flex-wrap items-center gap-3">
            <div className="p-2 rounded-xl bg-cyan-950/60 border border-cyan-800/50 text-cyan-400">
              <Box className="h-6 w-6" />
            </div>
            <div>
              <div className="flex flex-wrap items-center gap-2.5">
                <h1 className="text-xl font-bold text-zinc-100 tracking-tight">{app.name}</h1>
                <Badge
                  variant={
                    app.status === 'running'
                      ? 'success'
                      : app.status === 'error'
                      ? 'error'
                      : 'default'
                  }
                  dot
                  pulse={app.status === 'deploying'}
                >
                  {app.status}
                </Badge>
                <span
                  className={`text-[10px] font-mono uppercase px-2 py-0.5 rounded font-semibold ${
                    app.stage_id?.includes('staging')
                      ? 'bg-amber-500/10 text-amber-400 border border-amber-500/20'
                      : 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20'
                  }`}
                >
                  {app.stage_id?.includes('staging') ? 'staging' : 'prod'}
                </span>
                <span className="text-[10px] font-mono text-zinc-400 bg-zinc-800 px-1.5 py-0.5 rounded border border-zinc-700">
                  {app.runtime_mode?.toUpperCase() || 'SWARM'}
                </span>
              </div>

              {/* Meta Links (Domains / Image) */}
              <div className="flex flex-wrap items-center gap-4 text-xs text-zinc-400 mt-1 font-mono">
                {app.domains && app.domains.length > 0 ? (
                  <div className="flex items-center gap-1.5 text-cyan-400 hover:text-cyan-300 transition-colors">
                    <Globe className="h-3.5 w-3.5" />
                    <a
                      href={`https://${app.domains[0]}`}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="underline flex items-center gap-1"
                    >
                      <span>{app.domains[0]}</span>
                      <ExternalLink className="h-3 w-3" />
                    </a>
                  </div>
                ) : (
                  <span className="text-zinc-500">No public domain bound</span>
                )}
                <span className="text-zinc-600">•</span>
                <span className="text-zinc-300 truncate max-w-xs">{app.image}</span>
                <span className="text-zinc-600">•</span>
                <span>{app.replicas} {app.replicas === 1 ? 'replica' : 'replicas'}</span>
              </div>
            </div>
          </div>
        </div>

        {/* Global App Actions Toolbar */}
        <div className="flex items-center gap-2 flex-wrap self-start md:self-center">
          {isRunning ? (
            <>
              <Button
                variant="outline"
                size="sm"
                onClick={() => restartMutation.mutate(app.id)}
                isLoading={restartMutation.isPending}
                leftIcon={<RotateCw className="h-3.5 w-3.5 text-zinc-300" />}
              >
                Restart
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={() => stopMutation.mutate(app.id)}
                isLoading={stopMutation.isPending}
                leftIcon={<Square className="h-3.5 w-3.5 text-amber-400" />}
              >
                Stop
              </Button>
            </>
          ) : (
            <Button
              variant="outline"
              size="sm"
              onClick={() => startMutation.mutate(app.id)}
              isLoading={startMutation.isPending}
              leftIcon={<Play className="h-3.5 w-3.5 text-emerald-400" />}
            >
              Start
            </Button>
          )}

          <Button
            variant="primary"
            size="sm"
            onClick={() => deployMutation.mutate(app.id)}
            isLoading={deployMutation.isPending}
            leftIcon={<Zap className="h-3.5 w-3.5 text-zinc-950" />}
          >
            Redeploy
          </Button>

          <Button
            variant="ghost"
            size="sm"
            onClick={() => onMoveApp(app)}
            title="Move to another project"
          >
            <MoveRight className="h-4 w-4 text-cyan-400" />
          </Button>

          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              if (confirm(`Are you sure you want to delete ${app.name}?`)) {
                onDeleteApp(app.id);
              }
            }}
            title="Delete application"
          >
            <Trash2 className="h-4 w-4 text-rose-400" />
          </Button>
        </div>
      </div>

      {/* Tabs Navigation */}
      <Tabs
        tabs={[
          {
            id: 'topology',
            label: 'Topology & Containers',
            icon: <Cpu className="h-4 w-4" />,
          },
          {
            id: 'config',
            label: 'Configuration Editor',
            icon: <Sliders className="h-4 w-4" />,
          },
          {
            id: 'deployments',
            label: 'Deployments & Traffic',
            icon: <Zap className="h-4 w-4" />,
          },
          {
            id: 'logs',
            label: 'Live Logs',
            icon: <FileText className="h-4 w-4" />,
          },
          {
            id: 'terminal',
            label: 'Web Terminal',
            icon: <TerminalIcon className="h-4 w-4" />,
          },
        ]}
        activeTab={activeTab}
        onChange={(tab) => setActiveTab(tab as AppConsoleTabType)}
      />

      {/* Tab Panels */}
      <div>
        {activeTab === 'topology' && (
          <TopologyContainersTab
            app={app}
            onSelectLogs={() => setActiveTab('logs')}
          />
        )}

        {activeTab === 'config' && (
          <TwoColumnConfigEditor
            mode="edit"
            initialApp={app}
            projects={projects}
            onSaveAndRedeploy={handleConfigSaveAndRedeploy}
            onSaveChanges={handleConfigSaveChanges}
            isLoading={updateMutation.isPending || deployMutation.isPending}
          />
        )}

        {activeTab === 'deployments' && <DeploymentsTrafficTab app={app} />}

        {activeTab === 'logs' && <LiveLogsTab appId={app.id} />}

        {activeTab === 'terminal' && (
          <TerminalTab appId={app.id} appName={app.name} />
        )}
      </div>
    </div>
  );
}
