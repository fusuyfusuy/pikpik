import { useState, useMemo } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../lib/api';
import { App, Database, Stack } from '../lib/types';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Badge } from '../components/ui/Badge';
import { Input } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { useToast } from '../components/ui/Toast';
import { QueryErrorAlert } from '../components/ui/QueryErrorAlert';
import { UniversalCreateModal } from '../components/UniversalCreateModal';
import {
  Folder,
  FolderPlus,
  Box,
  Database as DbIcon,
  Layers,
  Plus,
  Search,
  ChevronRight,
  ChevronDown,
  Trash2,
} from 'lucide-react';

export interface ProjectsViewProps {
  onNavigate: (view: string) => void;
}

export function ProjectsView({ onNavigate }: ProjectsViewProps) {
  const queryClient = useQueryClient();
  const toast = useToast();

  const [searchQuery, setSearchQuery] = useState('');
  const [selectedTag] = useState('');
  const [isNewProjectModalOpen, setIsNewProjectModalOpen] = useState(false);
  const [newProjectName, setNewProjectName] = useState('');
  const [newProjectSlug, setNewProjectSlug] = useState('');
  const [newProjectDesc, setNewProjectDesc] = useState('');
  const [newProjectTags, setNewProjectTags] = useState('');

  const [isUniversalCreateOpen, setIsUniversalCreateOpen] = useState(false);
  const [createTargetProjectId, setCreateTargetProjectId] = useState('prj_default');

  const [expandedProjects, setExpandedProjects] = useState<Record<string, boolean>>({
    prj_default: true,
  });

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

  const {
    data: apps = [],
  } = useQuery({
    queryKey: ['apps'],
    queryFn: () => api.apps.list(),
  });

  const {
    data: databases = [],
  } = useQuery({
    queryKey: ['databases'],
    queryFn: api.databases.list,
  });

  const {
    data: stacks = [],
  } = useQuery({
    queryKey: ['stacks'],
    queryFn: api.stacks.list,
  });

  // Mutations
  const createProjectMutation = useMutation({
    mutationFn: (req: any) => api.projects.create(req),
    onSuccess: (newProj) => {
      queryClient.invalidateQueries({ queryKey: ['projects'] });
      toast.success('Project Created', `Workspace '${newProj.name}' is ready`);
      setIsNewProjectModalOpen(false);
      setNewProjectName('');
      setNewProjectSlug('');
      setNewProjectDesc('');
      setNewProjectTags('');
      setExpandedProjects((prev) => ({ ...prev, [newProj.id]: true }));
    },
    onError: (err: Error) => toast.error('Creation Failed', err.message),
  });

  const deleteProjectMutation = useMutation({
    mutationFn: (id: string) => api.projects.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projects'] });
      toast.success('Project Deleted', 'Workspace removed cleanly');
    },
    onError: (err: Error) => toast.error('Deletion Failed', err.message),
  });

  const toggleProjectExpand = (projId: string) => {
    setExpandedProjects((prev) => ({
      ...prev,
      [projId]: !prev[projId],
    }));
  };

  // Group workloads by project
  const projectWorkloads = useMemo(() => {
    const map: Record<string, { apps: App[]; databases: Database[]; stacks: Stack[] }> = {};
    projects.forEach((p) => {
      map[p.id] = { apps: [], databases: [], stacks: [] };
    });

    apps.forEach((a) => {
      const pid = a.project_id || 'prj_default';
      if (!map[pid]) map[pid] = { apps: [], databases: [], stacks: [] };
      map[pid].apps.push(a);
    });

    // Databases belong to projects or fallback to default
    databases.forEach((d) => {
      const pid = 'prj_default';
      if (!map[pid]) map[pid] = { apps: [], databases: [], stacks: [] };
      map[pid].databases.push(d);
    });

    stacks.forEach((s) => {
      const pid = s.project_id || 'prj_default';
      if (!map[pid]) map[pid] = { apps: [], databases: [], stacks: [] };
      map[pid].stacks.push(s);
    });

    return map;
  }, [projects, apps, databases, stacks]);

  // Filter projects by search query & tag
  const filteredProjects = useMemo(() => {
    return projects.filter((p) => {
      const matchesSearch =
        p.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
        p.slug.toLowerCase().includes(searchQuery.toLowerCase()) ||
        (p.description && p.description.toLowerCase().includes(searchQuery.toLowerCase()));
      const matchesTag = !selectedTag || (p.tags && p.tags.includes(selectedTag));
      return matchesSearch && matchesTag;
    });
  }, [projects, searchQuery, selectedTag]);

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-zinc-100 flex items-center gap-2">
            <Folder className="h-5 w-5 text-cyan-400" />
            <span>Project Workspaces</span>
          </h1>
          <p className="text-xs text-zinc-400 mt-0.5">
            Organize applications, managed databases, Compose stacks, and domains into logical environments
          </p>
        </div>

        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => setIsNewProjectModalOpen(true)}
            className="flex items-center gap-1.5"
          >
            <FolderPlus className="h-4 w-4 text-cyan-400" />
            <span>New Project</span>
          </Button>
          <Button
            variant="primary"
            size="sm"
            onClick={() => {
              setCreateTargetProjectId('prj_default');
              setIsUniversalCreateOpen(true);
            }}
            className="flex items-center gap-1.5"
          >
            <Plus className="h-4 w-4" />
            <span>New Resource</span>
          </Button>
        </div>
      </div>

      {/* Filter Toolbar */}
      <div className="flex flex-col sm:flex-row items-center gap-3">
        <div className="relative flex-1 w-full">
          <Search className="absolute left-3 top-2.5 h-4 w-4 text-zinc-500" />
          <input
            type="text"
            placeholder="Search projects by name, slug, or description..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="w-full pl-9 pr-3 py-2 bg-zinc-950 border border-zinc-800 rounded-lg text-xs text-zinc-200 focus:outline-none focus:border-cyan-500 transition-colors"
          />
        </div>
      </div>

      {isProjectsError && (
        <QueryErrorAlert
          error={projectsError}
          title="Failed to load project workspaces"
          onRetry={() => refetchProjects()}
        />
      )}

      {/* Projects List */}
      <div className="space-y-4">
        {isProjectsLoading ? (
          <Card className="p-8 text-center text-zinc-500 text-xs">
            Loading project workspaces...
          </Card>
        ) : filteredProjects.length === 0 ? (
          <Card className="p-12 text-center text-zinc-500 text-xs">
            No projects found matching your query.
          </Card>
        ) : (
          filteredProjects.map((project) => {
            const isExpanded = Boolean(expandedProjects[project.id]);
            const workloads = projectWorkloads[project.id] || { apps: [], databases: [], stacks: [] };
            const totalWorkloads = workloads.apps.length + workloads.databases.length + workloads.stacks.length;

            return (
              <Card key={project.id} className="overflow-hidden border-zinc-800/80 bg-zinc-950/60">
                {/* Project Header Bar */}
                <div className="p-4 flex items-center justify-between border-b border-zinc-800/60 bg-zinc-900/30">
                  <div
                    onClick={() => toggleProjectExpand(project.id)}
                    className="flex items-center gap-3 cursor-pointer select-none group"
                  >
                    <button
                      type="button"
                      className="p-1 rounded bg-zinc-800/50 text-zinc-400 group-hover:text-cyan-400 transition-colors"
                    >
                      {isExpanded ? (
                        <ChevronDown className="h-4 w-4" />
                      ) : (
                        <ChevronRight className="h-4 w-4" />
                      )}
                    </button>

                    <div>
                      <div className="flex items-center gap-2">
                        <span className="font-semibold text-sm text-zinc-100 group-hover:text-cyan-300 transition-colors">
                          {project.name}
                        </span>
                        <span className="font-mono text-[11px] text-zinc-500">
                          {project.slug || project.id}
                        </span>
                      </div>
                      {project.description && (
                        <p className="text-xs text-zinc-400 mt-0.5">{project.description}</p>
                      )}
                    </div>
                  </div>

                  <div className="flex items-center gap-2">
                    {/* Workload Badges */}
                    <div className="hidden sm:flex items-center gap-1.5 mr-2">
                      <Badge variant="outline" className="text-[10px] gap-1 font-mono text-zinc-300">
                        <Box className="h-3 w-3 text-cyan-400" />
                        <span>{workloads.apps.length} Apps</span>
                      </Badge>
                      <Badge variant="outline" className="text-[10px] gap-1 font-mono text-zinc-300">
                        <DbIcon className="h-3 w-3 text-emerald-400" />
                        <span>{workloads.databases.length} DBs</span>
                      </Badge>
                      <Badge variant="outline" className="text-[10px] gap-1 font-mono text-zinc-300">
                        <Layers className="h-3 w-3 text-purple-400" />
                        <span>{workloads.stacks.length} Stacks</span>
                      </Badge>
                    </div>

                    <Button
                      variant="secondary"
                      size="sm"
                      onClick={() => {
                        setCreateTargetProjectId(project.id);
                        setIsUniversalCreateOpen(true);
                      }}
                      className="flex items-center gap-1 text-xs"
                    >
                      <Plus className="h-3.5 w-3.5 text-cyan-400" />
                      <span>Add Workload</span>
                    </Button>

                    {project.id !== 'prj_default' && (
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => {
                          if (confirm(`Delete project workspace '${project.name}' and all associated metadata?`)) {
                            deleteProjectMutation.mutate(project.id);
                          }
                        }}
                      >
                        <Trash2 className="h-3.5 w-3.5 text-rose-400" />
                      </Button>
                    )}
                  </div>
                </div>

                {/* Expanded Workload Drawer */}
                {isExpanded && (
                  <div className="p-4 space-y-4">
                    {totalWorkloads === 0 ? (
                      <div className="text-center py-6 border border-dashed border-zinc-800 rounded-xl">
                        <Box className="h-6 w-6 text-zinc-600 mx-auto mb-2" />
                        <p className="text-xs text-zinc-400 font-medium">No active workloads in this workspace</p>
                        <p className="text-[11px] text-zinc-500 mt-1">Deploy an application, database, or stack to get started.</p>
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => {
                            setCreateTargetProjectId(project.id);
                            setIsUniversalCreateOpen(true);
                          }}
                          className="mt-3"
                        >
                          <Plus className="h-3.5 w-3.5 mr-1 text-cyan-400" />
                          Deploy First Service
                        </Button>
                      </div>
                    ) : (
                      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
                        {/* Apps Cards */}
                        {workloads.apps.map((app) => (
                          <div
                            key={app.id}
                            onClick={() => onNavigate('apps')}
                            className="p-3.5 rounded-xl border border-zinc-800/80 bg-zinc-900/40 hover:border-zinc-700 hover:bg-zinc-900/70 transition-all cursor-pointer space-y-2 group"
                          >
                            <div className="flex items-center justify-between">
                              <div className="flex items-center gap-2">
                                <Box className="h-4 w-4 text-cyan-400" />
                                <span className="font-semibold text-xs text-zinc-200 group-hover:text-cyan-300 transition-colors truncate">
                                  {app.name}
                                </span>
                              </div>
                              <Badge
                                variant={app.status === 'running' ? 'success' : app.status === 'error' ? 'error' : 'default'}
                                className="text-[10px] capitalize"
                              >
                                {app.status}
                              </Badge>
                            </div>

                            <p className="font-mono text-[11px] text-zinc-400 truncate">
                              {app.image || app.git_repo_url || 'Custom Service'}
                            </p>

                            <div className="flex items-center justify-between pt-1 border-t border-zinc-800/50 text-[11px] text-zinc-500">
                              <span>Port {app.container_port || 80}</span>
                              <span className="flex items-center gap-1 text-cyan-400 font-mono">
                                Details <ChevronRight className="h-3 w-3" />
                              </span>
                            </div>
                          </div>
                        ))}

                        {/* Databases Cards */}
                        {workloads.databases.map((db) => (
                          <div
                            key={db.id}
                            onClick={() => onNavigate('databases')}
                            className="p-3.5 rounded-xl border border-zinc-800/80 bg-zinc-900/40 hover:border-zinc-700 hover:bg-zinc-900/70 transition-all cursor-pointer space-y-2 group"
                          >
                            <div className="flex items-center justify-between">
                              <div className="flex items-center gap-2">
                                <DbIcon className="h-4 w-4 text-emerald-400" />
                                <span className="font-semibold text-xs text-zinc-200 group-hover:text-emerald-300 transition-colors truncate">
                                  {db.name}
                                </span>
                              </div>
                              <Badge variant="success" className="text-[10px] capitalize">
                                {db.status || 'running'}
                              </Badge>
                            </div>

                            <p className="font-mono text-[11px] text-zinc-400 truncate">
                              {db.engine}
                            </p>

                            <div className="flex items-center justify-between pt-1 border-t border-zinc-800/50 text-[11px] text-zinc-500">
                              <span>Port {db.port}</span>
                              <span className="flex items-center gap-1 text-emerald-400 font-mono">
                                Manage <ChevronRight className="h-3 w-3" />
                              </span>
                            </div>
                          </div>
                        ))}

                        {/* Stacks Cards */}
                        {workloads.stacks.map((stk) => (
                          <div
                            key={stk.id}
                            onClick={() => onNavigate('stacks')}
                            className="p-3.5 rounded-xl border border-zinc-800/80 bg-zinc-900/40 hover:border-zinc-700 hover:bg-zinc-900/70 transition-all cursor-pointer space-y-2 group"
                          >
                            <div className="flex items-center justify-between">
                              <div className="flex items-center gap-2">
                                <Layers className="h-4 w-4 text-purple-400" />
                                <span className="font-semibold text-xs text-zinc-200 group-hover:text-purple-300 transition-colors truncate">
                                  {stk.name}
                                </span>
                              </div>
                              <Badge variant="outline" className="text-[10px] capitalize">
                                {stk.status}
                              </Badge>
                            </div>

                            <p className="font-mono text-[11px] text-zinc-400 truncate">
                              {stk.services?.length || 1} Services
                            </p>

                            <div className="flex items-center justify-between pt-1 border-t border-zinc-800/50 text-[11px] text-zinc-500">
                              <span>Compose v2</span>
                              <span className="flex items-center gap-1 text-purple-400 font-mono">
                                Compose <ChevronRight className="h-3 w-3" />
                              </span>
                            </div>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                )}
              </Card>
            );
          })
        )}
      </div>

      {/* Create Project Modal */}
      <Modal
        isOpen={isNewProjectModalOpen}
        onClose={() => setIsNewProjectModalOpen(false)}
        title="Create Project Workspace"
        description="Group related services, environment variables, and domains together"
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            const tagsArr = newProjectTags.split(',').map((t) => t.trim()).filter(Boolean);
            createProjectMutation.mutate({
              name: newProjectName,
              slug: newProjectSlug || undefined,
              description: newProjectDesc || undefined,
              tags: tagsArr,
            });
          }}
          className="space-y-4"
        >
          <Input
            label="Project Name"
            placeholder="e.g. E-Commerce Platform"
            value={newProjectName}
            onChange={(e) => setNewProjectName(e.target.value)}
            required
          />

          <Input
            label="Project Slug (Optional)"
            placeholder="e.g. ecommerce-prod"
            value={newProjectSlug}
            onChange={(e) => setNewProjectSlug(e.target.value)}
          />

          <Input
            label="Description (Optional)"
            placeholder="Production API, Frontend, and Redis Cache services"
            value={newProjectDesc}
            onChange={(e) => setNewProjectDesc(e.target.value)}
          />

          <Input
            label="Environment Tags (Comma-separated)"
            placeholder="env:production, team:backend, ecommerce"
            value={newProjectTags}
            onChange={(e) => setNewProjectTags(e.target.value)}
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
              Create Workspace
            </Button>
          </div>
        </form>
      </Modal>

      {/* Universal Create Hub Modal */}
      <UniversalCreateModal
        isOpen={isUniversalCreateOpen}
        onClose={() => setIsUniversalCreateOpen(false)}
        onNavigate={onNavigate}
        defaultProjectId={createTargetProjectId}
      />
    </div>
  );
}
