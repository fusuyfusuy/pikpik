import { useState, useMemo } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../lib/api';
import {
  App,
  Project,
  CreateProjectRequest,
  CreateAppRequest,
} from '../lib/types';
import { FastCreateModal, SourceStrategyType } from './apps/FastCreateModal';
import { TwoColumnConfigEditor } from './apps/TwoColumnConfigEditor';
import { AppDetailsConsole } from './apps/AppDetailsConsole';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Badge } from '../components/ui/Badge';
import { Input } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '../components/ui/Table';
import { useToast } from '../components/ui/Toast';
import { QueryErrorAlert } from '../components/ui/QueryErrorAlert';
import {
  Layers,
  Plus,
  Search,
  Filter,
  Folder,
  FolderPlus,
  Box,
  Globe,
  SlidersHorizontal,
  ChevronRight,
  ChevronDown,
  Play,
  RotateCw,
  Square,
  Trash2,
  MoveRight,
  Loader2,
  X,
} from 'lucide-react';

export function AppsView() {
  const queryClient = useQueryClient();
  const toast = useToast();

  // Navigation State: selectedApp for Dedicated Console Mode
  const [selectedAppId, setSelectedAppId] = useState<string | null>(null);

  // Modals state
  const [isFastCreateOpen, setIsFastCreateOpen] = useState(false);
  const [isAdvancedCreateOpen, setIsAdvancedCreateOpen] = useState(false);
  const [advancedCreateInitialData, setAdvancedCreateInitialData] = useState<{
    name: string;
    projectId: string;
    stageId: string;
    strategy: SourceStrategyType;
    image?: string;
    gitRepoUrl?: string;
    gitBranch?: string;
    dockerfilePath?: string;
  } | null>(null);

  const [isNewProjectModalOpen, setIsNewProjectModalOpen] = useState(false);
  const [newProjectName, setNewProjectName] = useState('');
  const [newProjectDesc, setNewProjectDesc] = useState('');
  const [newProjectTags, setNewProjectTags] = useState('');

  const [isMoveModalOpen, setIsMoveModalOpen] = useState(false);
  const [appToMove, setAppToMove] = useState<App | null>(null);
  const [targetProjectId, setTargetProjectId] = useState('prj_default');

  // Filter & Search states
  const [selectedProject, setSelectedProject] = useState<string>('');
  const [selectedTag, setSelectedTag] = useState<string>('');
  const [searchQuery, setSearchQuery] = useState<string>('');
  const [collapsedProjects, setCollapsedProjects] = useState<Record<string, boolean>>({});

  // Queries
  const {
    data: projects = [],
    isLoading: isProjectsLoading,
    isError: isProjectsError,
    error: projectsError,
    refetch: refetchProjects,
  } = useQuery({
    queryKey: ['projects'],
    queryFn: () => api.projects.list(),
  });

  const { data: tagsList = [] } = useQuery({
    queryKey: ['tags'],
    queryFn: api.tags.list,
  });

  const {
    data: apps = [],
    isLoading: isAppsLoading,
    isError: isAppsError,
    error: appsError,
    refetch: refetchApps,
  } = useQuery({
    queryKey: ['apps', selectedProject, selectedTag, searchQuery],
    queryFn: () =>
      api.apps.list({
        project_id: selectedProject || undefined,
        tag: selectedTag || undefined,
        search: searchQuery || undefined,
      }),
  });

  const safeProjects = projects || [];
  const safeApps = apps || [];
  const safeTagsList = tagsList || [];

  // Currently selected app object
  const selectedApp = useMemo(() => {
    if (!selectedAppId) return null;
    return safeApps.find((a) => a.id === selectedAppId) || null;
  }, [safeApps, selectedAppId]);

  // Project Grouping Memo
  const groupedProjects = useMemo(() => {
    const defaultProj: Project = {
      id: 'prj_default',
      org_id: 'org_default',
      name: 'Default Project',
      slug: 'default',
      description: 'Default workspace for standalone applications and services',
      tags: [],
      app_count: 0,
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    };

    const projectList = safeProjects.length > 0 ? safeProjects : [defaultProj];
    const map = new Map<string, Project & { apps: App[] }>();
    for (const p of projectList) {
      map.set(p.id, { ...p, apps: [] });
    }

    if (!map.has('prj_default')) {
      map.set('prj_default', { ...defaultProj, apps: [] });
    }

    for (const app of safeApps) {
      const pid = app.project_id || 'prj_default';
      if (map.has(pid)) {
        map.get(pid)!.apps.push(app);
      } else {
        if (!map.has('prj_default')) {
          map.set('prj_default', { ...defaultProj, apps: [] });
        }
        map.get('prj_default')!.apps.push(app);
      }
    }

    let list = Array.from(map.values());
    if (selectedProject) {
      list = list.filter((p) => p.id === selectedProject || p.slug === selectedProject);
    }
    return list;
  }, [safeProjects, safeApps, selectedProject]);

  // Mutations
  const createProjectMutation = useMutation({
    mutationFn: (req: CreateProjectRequest) => api.projects.create(req),
    onSuccess: (newPrj) => {
      queryClient.invalidateQueries({ queryKey: ['projects'] });
      queryClient.invalidateQueries({ queryKey: ['tags'] });
      toast.success('Project Created', `Project "${newPrj.name}" initialized`);
      setIsNewProjectModalOpen(false);
      setNewProjectName('');
      setNewProjectDesc('');
      setNewProjectTags('');
    },
    onError: (err: Error) => toast.error('Project Creation Failed', err.message),
  });

  const moveAppMutation = useMutation({
    mutationFn: ({ appId, projectId }: { appId: string; projectId: string }) =>
      api.apps.update(appId, { project_id: projectId }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['apps'] });
      queryClient.invalidateQueries({ queryKey: ['projects'] });
      toast.success('App Moved', 'Application reassigned to project');
      setIsMoveModalOpen(false);
      setAppToMove(null);
    },
    onError: (err: Error) => toast.error('Move Failed', err.message),
  });

  const createMutation = useMutation({
    mutationFn: (req: CreateAppRequest) => api.apps.create(req),
    onSuccess: (newApp) => {
      queryClient.invalidateQueries({ queryKey: ['apps'] });
      queryClient.invalidateQueries({ queryKey: ['projects'] });
      queryClient.invalidateQueries({ queryKey: ['tags'] });
      toast.success('App Created', `Application ${newApp.name} initialized`);
      setIsFastCreateOpen(false);
      setIsAdvancedCreateOpen(false);
      // Auto-focus the newly created app in Dedicated Console Mode
      setSelectedAppId(newApp.id);
    },
    onError: (err: Error) => toast.error('Creation Failed', err.message),
  });

  const startMutation = useMutation({
    mutationFn: (id: string) => api.apps.start(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['apps'] });
      toast.success('Started', 'Application started');
    },
    onError: (err: Error) => toast.error('Start Failed', err.message),
  });

  const stopMutation = useMutation({
    mutationFn: (id: string) => api.apps.stop(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['apps'] });
      toast.info('Stopped', 'Application scaled to 0 replicas');
    },
    onError: (err: Error) => toast.error('Stop Failed', err.message),
  });

  const restartMutation = useMutation({
    mutationFn: (id: string) => api.apps.restart(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['apps'] });
      toast.success('Restarted', 'Application containers restarted');
    },
    onError: (err: Error) => toast.error('Restart Failed', err.message),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.apps.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['apps'] });
      queryClient.invalidateQueries({ queryKey: ['projects'] });
      queryClient.invalidateQueries({ queryKey: ['tags'] });
      toast.success('Deleted', 'Application service removed');
      if (selectedAppId) setSelectedAppId(null);
    },
    onError: (err: Error) => toast.error('Delete Failed', err.message),
  });

  const toggleCollapse = (projId: string) => {
    setCollapsedProjects((prev) => ({
      ...prev,
      [projId]: !prev[projId],
    }));
  };

  const handleQuickDeploy = (data: {
    name: string;
    projectId: string;
    stageId: string;
    strategy: SourceStrategyType;
    image?: string;
    gitRepoUrl?: string;
    gitBranch?: string;
    dockerfilePath?: string;
  }) => {
    if (data.strategy === 'git') {
      createMutation.mutate({
        name: data.name,
        project_id: data.projectId,
        stage_id: data.stageId,
        image: `${data.name}:latest`,
        replicas: 1,
        git_repo_url: data.gitRepoUrl,
        git_branch: data.gitBranch || 'main',
        build_strategy: 'dockerfile',
        dockerfile_path: data.dockerfilePath || 'Dockerfile',
      });
    } else if (data.strategy === 'image') {
      createMutation.mutate({
        name: data.name,
        project_id: data.projectId,
        stage_id: data.stageId,
        image: data.image || 'nginx:alpine',
        replicas: 1,
      });
    } else {
      createMutation.mutate({
        name: data.name,
        project_id: data.projectId,
        stage_id: data.stageId,
        image: 'nginx:alpine',
        replicas: 1,
        runtime_mode: 'swarm',
        compose_yaml: `version: '3.8'\nservices:\n  ${data.name}:\n    image: nginx:alpine\n    ports:\n      - "80:80"\n    deploy:\n      replicas: 1\n`,
      });
    }
  };

  const handleOpenAdvanced = (initialState: {
    name: string;
    projectId: string;
    stageId: string;
    strategy: SourceStrategyType;
    image?: string;
    gitRepoUrl?: string;
    gitBranch?: string;
    dockerfilePath?: string;
  }) => {
    setAdvancedCreateInitialData(initialState);
    setIsFastCreateOpen(false);
    setIsAdvancedCreateOpen(true);
  };

  const handleNewProjectSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!newProjectName.trim()) return;
    const tagList = newProjectTags
      .split(',')
      .map((t) => t.trim())
      .filter(Boolean);
    createProjectMutation.mutate({
      name: newProjectName.trim(),
      description: newProjectDesc.trim() || undefined,
      tags: tagList,
    });
  };

  // =========================================================================
  // VIEW 1: Dedicated App Details Console Mode (When an App is Selected)
  // =========================================================================
  if (selectedApp) {
    return (
      <AppDetailsConsole
        app={selectedApp}
        projects={projects}
        onBack={() => setSelectedAppId(null)}
        onMoveApp={(app) => {
          setAppToMove(app);
          setTargetProjectId(app.project_id || 'prj_default');
          setIsMoveModalOpen(true);
        }}
        onDeleteApp={(appId) => deleteMutation.mutate(appId)}
      />
    );
  }

  // =========================================================================
  // VIEW 2: Advanced Two-Column Creation Mode
  // =========================================================================
  if (isAdvancedCreateOpen) {
    return (
      <div className="space-y-4">
        <TwoColumnConfigEditor
          mode="create"
          projects={projects}
          defaultProjectId={advancedCreateInitialData?.projectId || 'prj_default'}
          defaultStageId={advancedCreateInitialData?.stageId || 'stg_default_prod'}
          defaultStrategy={advancedCreateInitialData?.strategy || 'compose'}
          defaultName={advancedCreateInitialData?.name || ''}
          defaultImage={advancedCreateInitialData?.image || ''}
          defaultGitRepo={advancedCreateInitialData?.gitRepoUrl || ''}
          defaultGitBranch={advancedCreateInitialData?.gitBranch || 'main'}
          defaultDockerfile={advancedCreateInitialData?.dockerfilePath || 'Dockerfile'}
          onSaveAndRedeploy={(req) => createMutation.mutate(req as CreateAppRequest)}
          onCancel={() => setIsAdvancedCreateOpen(false)}
          isLoading={createMutation.isPending}
        />
      </div>
    );
  }

  // =========================================================================
  // VIEW 3: Workspace Projects & Applications Explorer
  // =========================================================================
  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-zinc-100 flex items-center gap-2">
            <Layers className="h-5 w-5 text-cyan-400" />
            <span>Applications & Projects</span>
          </h1>
          <p className="text-xs text-zinc-400 mt-0.5">
            Docker Swarm services organized by project workspaces with multi-tag taxonomy and zero-downtime rollouts
          </p>
        </div>

        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => setIsNewProjectModalOpen(true)}
            leftIcon={<FolderPlus className="h-4 w-4 text-zinc-300" />}
          >
            New Project
          </Button>

          <Button
            variant="secondary"
            size="sm"
            onClick={() => {
              setAdvancedCreateInitialData(null);
              setIsAdvancedCreateOpen(true);
            }}
            leftIcon={<SlidersHorizontal className="h-4 w-4 text-cyan-400" />}
          >
            Power Editor
          </Button>

          <Button
            variant="primary"
            size="sm"
            onClick={() => setIsFastCreateOpen(true)}
            leftIcon={<Plus className="h-4 w-4" />}
          >
            Create App
          </Button>
        </div>
      </div>

      {/* Filter Bar */}
      <Card className="p-3.5 space-y-3 bg-zinc-900/60 border-zinc-800">
        <div className="flex flex-col md:flex-row items-stretch md:items-center justify-between gap-3">
          {/* Search Box */}
          <div className="relative flex-1 max-w-md">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-zinc-400" />
            <input
              type="text"
              placeholder="Search apps by name, image, domain, git..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="w-full bg-zinc-950/80 border border-zinc-800 rounded-lg pl-9 pr-8 py-1.5 text-xs text-zinc-100 placeholder-zinc-500 focus:outline-none focus:border-cyan-500"
            />
            {searchQuery && (
              <button
                onClick={() => setSearchQuery('')}
                className="absolute right-2.5 top-1/2 -translate-y-1/2 text-zinc-500 hover:text-zinc-300"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            )}
          </div>

          {/* Project Selector Pills */}
          <div className="flex items-center gap-1.5 overflow-x-auto pb-1 md:pb-0 scrollbar-none">
            <button
              onClick={() => setSelectedProject('')}
              className={`px-2.5 py-1 text-xs font-medium rounded-md transition-colors whitespace-nowrap flex items-center gap-1.5 ${
                selectedProject === ''
                  ? 'bg-cyan-500/20 text-cyan-300 border border-cyan-500/30'
                  : 'bg-zinc-800/60 text-zinc-400 hover:text-zinc-200 border border-transparent'
              }`}
            >
              <Folder className="h-3 w-3" />
              <span>All Projects</span>
              <span className="text-[10px] opacity-70">({safeApps.length})</span>
            </button>

            {safeProjects.map((p) => (
              <button
                key={p.id}
                onClick={() => setSelectedProject(selectedProject === p.id ? '' : p.id)}
                className={`px-2.5 py-1 text-xs font-medium rounded-md transition-colors whitespace-nowrap flex items-center gap-1.5 ${
                  selectedProject === p.id
                    ? 'bg-cyan-500/20 text-cyan-300 border border-cyan-500/30'
                    : 'bg-zinc-800/60 text-zinc-400 hover:text-zinc-200 border border-transparent'
                }`}
              >
                <span>{p.name}</span>
                <span className="text-[10px] opacity-70">({p.app_count || 0})</span>
              </button>
            ))}
          </div>
        </div>

        {/* Tag Chip Filter Bar */}
        {safeTagsList.length > 0 && (
          <div className="pt-2 border-t border-zinc-800/60 flex items-center gap-2 overflow-x-auto scrollbar-none">
            <div className="flex items-center gap-1 text-zinc-500 text-[11px] font-medium shrink-0">
              <Filter className="h-3 w-3" />
              <span>Tags:</span>
            </div>

            <button
              onClick={() => setSelectedTag('')}
              className={`px-2 py-0.5 rounded-full text-[11px] font-medium transition-all ${
                selectedTag === ''
                  ? 'bg-zinc-100 text-zinc-900 font-semibold'
                  : 'bg-zinc-800/70 text-zinc-400 hover:text-zinc-200'
              }`}
            >
              All
            </button>

            {tagsList.map((t) => {
              const isKV = t.tag.includes(':');
              const isSelected = selectedTag === t.tag;
              const [k, v] = isKV ? t.tag.split(':') : [t.tag, ''];

              return (
                <button
                  key={t.tag}
                  onClick={() => setSelectedTag(isSelected ? '' : t.tag)}
                  className={`px-2 py-0.5 rounded-full text-[11px] font-mono transition-all flex items-center gap-1 shrink-0 ${
                    isSelected
                      ? 'bg-cyan-500 text-zinc-950 font-semibold shadow-sm ring-1 ring-cyan-400'
                      : isKV
                      ? 'bg-zinc-800/90 text-zinc-300 border border-zinc-700/60 hover:border-cyan-500/50'
                      : 'bg-zinc-800/60 text-zinc-400 border border-zinc-800 hover:border-zinc-600'
                  }`}
                >
                  {isKV ? (
                    <span>
                      <span className={isSelected ? 'text-zinc-950 font-bold' : 'text-cyan-400'}>
                        {k}:
                      </span>
                      <span>{v}</span>
                    </span>
                  ) : (
                    <span>#{t.tag}</span>
                  )}
                  <span
                    className={`text-[10px] ${
                      isSelected ? 'text-zinc-950 font-bold' : 'text-zinc-500'
                    }`}
                  >
                    ({t.count})
                  </span>
                </button>
              );
            })}
          </div>
        )}
      </Card>

      {/* Visual Project Cards Grouping */}
      {isAppsError || isProjectsError ? (
        <QueryErrorAlert
          title="Failed to Load Applications"
          error={appsError || projectsError}
          onRetry={() => {
            refetchApps();
            refetchProjects();
          }}
        />
      ) : isAppsLoading || isProjectsLoading ? (
        <Card className="p-8 text-center text-zinc-500 text-sm">
          <Loader2 className="h-5 w-5 animate-spin mx-auto mb-2 text-cyan-400" />
          Loading workspace and applications...
        </Card>
      ) : groupedProjects.length === 0 ? (
        <Card className="p-10 text-center text-zinc-500">
          No projects or applications match the current filter.
        </Card>
      ) : (
        <div className="space-y-6">
          {groupedProjects.map((proj) => {
            const isCollapsed = collapsedProjects[proj.id];
            const projApps = proj.apps || [];

            return (
              <Card
                key={proj.id}
                className="p-0 overflow-hidden border-zinc-800/80 bg-zinc-950/40"
              >
                {/* Project Header */}
                <div className="p-3.5 bg-zinc-900/90 border-b border-zinc-800 flex flex-col sm:flex-row sm:items-center justify-between gap-3">
                  <div className="flex items-center gap-3">
                    <button
                      onClick={() => toggleCollapse(proj.id)}
                      className="p-1 rounded text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800/60 transition-colors cursor-pointer"
                      title={isCollapsed ? 'Expand' : 'Collapse'}
                    >
                      {isCollapsed ? (
                        <ChevronRight className="h-4 w-4" />
                      ) : (
                        <ChevronDown className="h-4 w-4" />
                      )}
                    </button>

                    <div className="flex items-center gap-2">
                      <Folder className="h-4 w-4 text-cyan-400 shrink-0" />
                      <div>
                        <div className="flex items-center gap-2">
                          <span className="font-semibold text-sm text-zinc-100">
                            {proj.name}
                          </span>
                          <span className="text-[10px] font-mono text-zinc-500 bg-zinc-800/80 px-1.5 py-0.2 rounded">
                            {proj.slug || proj.id}
                          </span>
                        </div>
                        {proj.description && (
                          <p className="text-[11px] text-zinc-400 mt-0.5 line-clamp-1">
                            {proj.description}
                          </p>
                        )}
                      </div>
                    </div>
                  </div>

                  <div className="flex items-center gap-2 self-end sm:self-center">
                    {proj.tags && proj.tags.length > 0 && (
                      <div className="hidden md:flex items-center gap-1">
                        {proj.tags.map((t) => (
                          <span
                            key={t}
                            onClick={() => setSelectedTag(t)}
                            className="bg-zinc-800 text-zinc-400 hover:text-cyan-300 text-[10px] font-mono px-1.5 py-0.5 rounded cursor-pointer transition-colors"
                          >
                            {t}
                          </span>
                        ))}
                      </div>
                    )}

                    <Badge variant="outline" className="text-[11px] font-mono">
                      {projApps.length} {projApps.length === 1 ? 'service' : 'services'}
                    </Badge>

                    <Button
                      variant="subtle"
                      size="sm"
                      onClick={() => {
                        setIsFastCreateOpen(true);
                      }}
                      leftIcon={<Plus className="h-3 w-3" />}
                    >
                      Deploy App
                    </Button>
                  </div>
                </div>

                {/* Project Applications Table */}
                {!isCollapsed && (
                  <div>
                    {projApps.length === 0 ? (
                      <div className="p-8 text-center text-xs text-zinc-500">
                        No applications deployed in this project yet.{' '}
                        <button
                          onClick={() => setIsFastCreateOpen(true)}
                          className="text-cyan-400 hover:underline font-medium ml-1 cursor-pointer"
                        >
                          Deploy your first app
                        </button>
                      </div>
                    ) : (
                      <Table>
                        <TableHeader>
                          <TableRow>
                            <TableHead>Service Name</TableHead>
                            <TableHead>Stage</TableHead>
                            <TableHead>Runtime</TableHead>
                            <TableHead>Source / Image</TableHead>
                            <TableHead>Tags</TableHead>
                            <TableHead>Replicas</TableHead>
                            <TableHead>Domains</TableHead>
                            <TableHead>Status</TableHead>
                            <TableHead className="text-right">Actions</TableHead>
                          </TableRow>
                        </TableHeader>
                        <TableBody>
                          {projApps.map((app) => (
                            <TableRow
                              key={app.id}
                              className="cursor-pointer hover:bg-zinc-900/60 transition-colors"
                              onClick={() => setSelectedAppId(app.id)}
                            >
                              <TableCell className="font-semibold text-zinc-100">
                                <div className="flex items-center gap-2">
                                  <Box className="h-4 w-4 text-cyan-400 shrink-0" />
                                  <span>{app.name}</span>
                                </div>
                              </TableCell>

                              <TableCell>
                                <span
                                  className={`text-[10px] font-mono uppercase px-1.5 py-0.5 rounded font-semibold ${
                                    app.stage_id?.includes('staging')
                                      ? 'bg-amber-500/10 text-amber-400 border border-amber-500/20'
                                      : 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20'
                                  }`}
                                >
                                  {app.stage_id?.includes('staging') ? 'staging' : 'prod'}
                                </span>
                              </TableCell>

                              <TableCell className="font-mono text-xs text-zinc-400">
                                <span className="bg-zinc-900 px-1.5 py-0.5 rounded border border-zinc-800 text-[10px] uppercase font-bold text-zinc-300">
                                  {app.runtime_mode || 'swarm'}
                                </span>
                              </TableCell>

                              <TableCell className="font-mono text-xs text-zinc-300 truncate max-w-xs">
                                {app.git_repo_url ? (
                                  <span className="text-emerald-400">
                                    git:{app.git_repo_url.split('/').pop() || app.git_repo_url}
                                  </span>
                                ) : (
                                  app.image
                                )}
                              </TableCell>

                              <TableCell>
                                <div className="flex flex-wrap gap-1 max-w-[160px]">
                                  {app.tags && app.tags.length > 0 ? (
                                    app.tags.slice(0, 2).map((t) => (
                                      <span
                                        key={t}
                                        className="bg-zinc-800 text-zinc-300 text-[10px] font-mono px-1 py-0.2 rounded"
                                      >
                                        {t}
                                      </span>
                                    ))
                                  ) : (
                                    <span className="text-zinc-600 text-xs">—</span>
                                  )}
                                  {app.tags && app.tags.length > 2 && (
                                    <span className="text-[10px] text-zinc-500">
                                      +{app.tags.length - 2}
                                    </span>
                                  )}
                                </div>
                              </TableCell>

                              <TableCell className="font-mono text-xs text-zinc-400">
                                {app.replicas}
                              </TableCell>

                              <TableCell className="font-mono text-xs text-cyan-400 truncate max-w-xs">
                                {app.domains && app.domains.length > 0 ? (
                                  <span className="flex items-center gap-1">
                                    <Globe className="h-3 w-3" />
                                    <span>{app.domains[0]}</span>
                                  </span>
                                ) : (
                                  <span className="text-zinc-600">—</span>
                                )}
                              </TableCell>

                              <TableCell>
                                <Badge
                                  variant={
                                    app.status === 'running'
                                      ? 'success'
                                      : app.status === 'error'
                                      ? 'error'
                                      : 'default'
                                  }
                                  dot
                                >
                                  {app.status}
                                </Badge>
                              </TableCell>

                              <TableCell className="text-right" onClick={(e) => e.stopPropagation()}>
                                <div className="flex items-center justify-end gap-1">
                                  {app.status === 'running' ? (
                                    <>
                                      <Button
                                        variant="ghost"
                                        size="sm"
                                        title="Restart"
                                        onClick={() => restartMutation.mutate(app.id)}
                                      >
                                        <RotateCw className="h-3.5 w-3.5 text-cyan-400" />
                                      </Button>
                                      <Button
                                        variant="ghost"
                                        size="sm"
                                        title="Stop"
                                        onClick={() => stopMutation.mutate(app.id)}
                                      >
                                        <Square className="h-3.5 w-3.5 text-amber-400" />
                                      </Button>
                                    </>
                                  ) : (
                                    <Button
                                      variant="ghost"
                                      size="sm"
                                      title="Start"
                                      onClick={() => startMutation.mutate(app.id)}
                                    >
                                      <Play className="h-3.5 w-3.5 text-emerald-400" />
                                    </Button>
                                  )}

                                  <Button
                                    variant="ghost"
                                    size="sm"
                                    title="Move to Project"
                                    onClick={() => {
                                      setAppToMove(app);
                                      setTargetProjectId(app.project_id || 'prj_default');
                                      setIsMoveModalOpen(true);
                                    }}
                                  >
                                    <MoveRight className="h-3.5 w-3.5 text-cyan-400" />
                                  </Button>

                                  <Button
                                    variant="ghost"
                                    size="sm"
                                    title="Delete"
                                    onClick={() => {
                                      if (confirm(`Are you sure you want to delete ${app.name}?`)) {
                                        deleteMutation.mutate(app.id);
                                      }
                                    }}
                                  >
                                    <Trash2 className="h-3.5 w-3.5 text-rose-400" />
                                  </Button>
                                </div>
                              </TableCell>
                            </TableRow>
                          ))}
                        </TableBody>
                      </Table>
                    )}
                  </div>
                )}
              </Card>
            );
          })}
        </div>
      )}

      {/* Fast-Create Modal */}
      <FastCreateModal
        isOpen={isFastCreateOpen}
        onClose={() => setIsFastCreateOpen(false)}
        projects={projects}
        onQuickDeploy={handleQuickDeploy}
        onOpenAdvanced={handleOpenAdvanced}
        isLoading={createMutation.isPending}
      />

      {/* Create New Project Modal */}
      <Modal
        isOpen={isNewProjectModalOpen}
        onClose={() => setIsNewProjectModalOpen(false)}
        title="Create New Project Workspace"
        description="Organize microservices, stages, and databases under a shared project namespace"
      >
        <form onSubmit={handleNewProjectSubmit} className="space-y-4">
          <Input
            label="Project Name"
            placeholder="Billing & Invoicing"
            value={newProjectName}
            onChange={(e) => setNewProjectName(e.target.value)}
            required
            autoFocus
          />

          <Input
            label="Description (Optional)"
            placeholder="Financial transaction pipelines and Stripe webhooks"
            value={newProjectDesc}
            onChange={(e) => setNewProjectDesc(e.target.value)}
          />

          <Input
            label="Tags (Optional, comma-separated)"
            placeholder="finance, team:billing, tier:1"
            value={newProjectTags}
            onChange={(e) => setNewProjectTags(e.target.value)}
            helperText="Support key-value pairs (e.g. team:core) or flat labels"
          />

          <div className="flex justify-end gap-2 pt-4 border-t border-zinc-800">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setIsNewProjectModalOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              variant="primary"
              size="sm"
              isLoading={createProjectMutation.isPending}
            >
              Create Project
            </Button>
          </div>
        </form>
      </Modal>

      {/* Move App to Project Modal */}
      <Modal
        isOpen={isMoveModalOpen}
        onClose={() => {
          setIsMoveModalOpen(false);
          setAppToMove(null);
        }}
        title="Move Application to Project"
        description={`Reassign "${appToMove?.name}" to a different workspace project.`}
      >
        <div className="space-y-4">
          <div className="space-y-1.5">
            <label className="block text-xs font-medium text-zinc-300">
              Target Project Workspace
            </label>
            <select
              value={targetProjectId}
              onChange={(e) => setTargetProjectId(e.target.value)}
              className="w-full rounded-lg border border-zinc-800 bg-zinc-950/60 px-3.5 py-2 text-sm text-zinc-100 focus:border-cyan-500 focus:ring-1 focus:ring-cyan-500 focus:outline-none"
            >
              <option value="prj_default">Default Project</option>
              {projects
                .filter((p) => p.id !== 'prj_default')
                .map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name} ({p.slug || p.id})
                  </option>
                ))}
            </select>
          </div>

          <div className="flex justify-end gap-2 pt-4 border-t border-zinc-800">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => {
                setIsMoveModalOpen(false);
                setAppToMove(null);
              }}
            >
              Cancel
            </Button>
            <Button
              type="button"
              variant="primary"
              size="sm"
              isLoading={moveAppMutation.isPending}
              onClick={() => {
                if (appToMove) {
                  moveAppMutation.mutate({ appId: appToMove.id, projectId: targetProjectId });
                }
              }}
            >
              Move App
            </Button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
