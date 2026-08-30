import { useState } from 'react';
import { Project } from '../../lib/types';
import { Modal } from '../../components/ui/Modal';
import { Button } from '../../components/ui/Button';
import { Input } from '../../components/ui/Input';
import {
  Code2,
  Box,
  GitBranch,
  FileCode,
  SlidersHorizontal,
  Rocket,
  Layers,
  Sparkles,
} from 'lucide-react';

export type SourceStrategyType = 'compose' | 'image' | 'git' | 'dockerfile';

export interface FastCreateModalProps {
  isOpen: boolean;
  onClose: () => void;
  projects: Project[];
  defaultProjectId?: string;
  onQuickDeploy: (data: {
    name: string;
    projectId: string;
    stageId: string;
    strategy: SourceStrategyType;
    image?: string;
    gitRepoUrl?: string;
    gitBranch?: string;
    dockerfilePath?: string;
  }) => void;
  onOpenAdvanced: (initialState: {
    name: string;
    projectId: string;
    stageId: string;
    strategy: SourceStrategyType;
    image?: string;
    gitRepoUrl?: string;
    gitBranch?: string;
    dockerfilePath?: string;
  }) => void;
  isLoading?: boolean;
}

export function FastCreateModal({
  isOpen,
  onClose,
  projects,
  defaultProjectId = 'prj_default',
  onQuickDeploy,
  onOpenAdvanced,
  isLoading = false,
}: FastCreateModalProps) {
  const [name, setName] = useState('');
  const [projectId, setProjectId] = useState(defaultProjectId);
  const [stageId, setStageId] = useState('stg_default_prod');
  const [strategy, setStrategy] = useState<SourceStrategyType>('compose');

  // Quick inputs
  const [image, setImage] = useState('nginx:alpine');
  const [gitRepoUrl, setGitRepoUrl] = useState('');
  const [gitBranch, setGitBranch] = useState('main');
  const [dockerfilePath, setDockerfilePath] = useState('Dockerfile');

  const strategies: Array<{
    id: SourceStrategyType;
    label: string;
    desc: string;
    icon: React.ReactNode;
    badge?: string;
  }> = [
    {
      id: 'compose',
      label: 'Docker Compose',
      desc: 'Multi-container stack blueprint with dynamic topology',
      icon: <Code2 className="h-4 w-4 text-cyan-400" />,
      badge: 'Recommended',
    },
    {
      id: 'image',
      label: 'Single Image',
      desc: 'Deploy pre-built image from Docker Hub or GHCR',
      icon: <Box className="h-4 w-4 text-blue-400" />,
    },
    {
      id: 'git',
      label: 'Git Repository',
      desc: 'Auto-build from GitHub or public Git repo',
      icon: <GitBranch className="h-4 w-4 text-emerald-400" />,
    },
    {
      id: 'dockerfile',
      label: 'Dockerfile',
      desc: 'Custom build recipe with multi-stage directives',
      icon: <FileCode className="h-4 w-4 text-amber-400" />,
    },
  ];

  const handleQuickSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;

    onQuickDeploy({
      name: name.trim(),
      projectId,
      stageId,
      strategy,
      image: strategy === 'image' ? image.trim() : undefined,
      gitRepoUrl: strategy === 'git' ? gitRepoUrl.trim() : undefined,
      gitBranch: strategy === 'git' ? gitBranch.trim() : undefined,
      dockerfilePath: strategy === 'dockerfile' ? dockerfilePath.trim() : undefined,
    });
  };

  const handleAdvancedClick = () => {
    onOpenAdvanced({
      name: name.trim(),
      projectId,
      stageId,
      strategy,
      image: strategy === 'image' ? image.trim() : undefined,
      gitRepoUrl: strategy === 'git' ? gitRepoUrl.trim() : undefined,
      gitBranch: strategy === 'git' ? gitBranch.trim() : undefined,
      dockerfilePath: strategy === 'dockerfile' ? dockerfilePath.trim() : undefined,
    });
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title={
        <div className="flex items-center gap-2">
          <Layers className="h-5 w-5 text-cyan-400" />
          <span>Deploy Application</span>
        </div>
      }
      description="Quick scaffold for new microservices, background workers, or Compose stacks"
      size="lg"
    >
      <form onSubmit={handleQuickSubmit} className="space-y-4">
        {/* Project & Stage Selection */}
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 p-3 bg-zinc-950/80 rounded-lg border border-zinc-800">
          <div className="space-y-1.5">
            <label className="block text-xs font-medium text-zinc-300">
              Project Workspace
            </label>
            <select
              value={projectId}
              onChange={(e) => setProjectId(e.target.value)}
              className="w-full rounded-lg border border-zinc-800 bg-zinc-900 px-3 py-2 text-xs text-zinc-100 focus:border-cyan-500 focus:ring-1 focus:ring-cyan-500 focus:outline-none"
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

          <div className="space-y-1.5">
            <label className="block text-xs font-medium text-zinc-300">
              Deployment Stage
            </label>
            <select
              value={stageId}
              onChange={(e) => setStageId(e.target.value)}
              className="w-full rounded-lg border border-zinc-800 bg-zinc-900 px-3 py-2 text-xs text-zinc-100 focus:border-cyan-500 focus:ring-1 focus:ring-cyan-500 focus:outline-none"
            >
              <option value="stg_default_prod">Production (prod)</option>
              <option value="stg_default_staging">Staging (staging)</option>
              <option value="stg_default_preview">Preview (preview)</option>
            </select>
          </div>
        </div>

        {/* App Name */}
        <Input
          label="Application / Service Name"
          placeholder="e.g. billing-api, web-frontend, cache-cluster"
          value={name}
          onChange={(e) => setName(e.target.value)}
          required
          autoFocus
          helperText="Unique identifier within this project workspace"
        />

        {/* Source Strategy 4-way Cards */}
        <div className="space-y-2">
          <label className="block text-xs font-medium text-zinc-300">
            Source Strategy
          </label>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-2.5">
            {strategies.map((s) => {
              const isSelected = strategy === s.id;
              return (
                <button
                  key={s.id}
                  type="button"
                  onClick={() => setStrategy(s.id)}
                  className={`p-3 rounded-lg border text-left transition-all relative ${
                    isSelected
                      ? 'border-cyan-500 bg-cyan-950/20 ring-1 ring-cyan-500 shadow-sm'
                      : 'border-zinc-800 bg-zinc-950/60 hover:bg-zinc-900/80 text-zinc-400'
                  }`}
                >
                  {s.badge && (
                    <span className="absolute top-2 right-2 px-1.5 py-0.2 rounded bg-cyan-950 text-cyan-400 border border-cyan-800/40 text-[9px] font-medium">
                      {s.badge}
                    </span>
                  )}
                  <div className="flex items-center gap-2">
                    {s.icon}
                    <span
                      className={`text-xs font-semibold ${
                        isSelected ? 'text-zinc-100' : 'text-zinc-300'
                      }`}
                    >
                      {s.label}
                    </span>
                  </div>
                  <p className="text-[11px] text-zinc-400 mt-1 line-clamp-2 leading-relaxed">
                    {s.desc}
                  </p>
                </button>
              );
            })}
          </div>
        </div>

        {/* Contextual Quick Input */}
        {strategy === 'image' && (
          <Input
            label="Container Image Tag"
            placeholder="e.g. nginx:alpine, redis:7-alpine, ghcr.io/org/repo:latest"
            value={image}
            onChange={(e) => setImage(e.target.value)}
            required
            leftIcon={<Box className="h-3.5 w-3.5 text-blue-400" />}
          />
        )}

        {strategy === 'git' && (
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-2">
            <div className="sm:col-span-2">
              <Input
                label="Git Repository URL"
                placeholder="https://github.com/org/repo.git"
                value={gitRepoUrl}
                onChange={(e) => setGitRepoUrl(e.target.value)}
                required
                leftIcon={<GitBranch className="h-3.5 w-3.5 text-emerald-400" />}
              />
            </div>
            <div>
              <Input
                label="Branch"
                placeholder="main"
                value={gitBranch}
                onChange={(e) => setGitBranch(e.target.value)}
              />
            </div>
          </div>
        )}

        {strategy === 'dockerfile' && (
          <Input
            label="Dockerfile Path / Recipe"
            placeholder="Dockerfile"
            value={dockerfilePath}
            onChange={(e) => setDockerfilePath(e.target.value)}
            leftIcon={<FileCode className="h-3.5 w-3.5 text-amber-400" />}
            helperText="Relative path to Dockerfile or custom build specification"
          />
        )}

        {strategy === 'compose' && (
          <div className="p-2.5 bg-zinc-950/60 rounded-lg border border-zinc-800 text-[11px] text-zinc-400 flex items-center justify-between">
            <span className="flex items-center gap-1.5">
              <Sparkles className="h-3.5 w-3.5 text-cyan-400" />
              <span>Default multi-container template ready with AST inspection</span>
            </span>
            <span className="text-zinc-500 font-mono text-[10px]">Compose v3.8</span>
          </div>
        )}

        {/* Footer Actions */}
        <div className="flex flex-col sm:flex-row items-center justify-between gap-2 pt-4 border-t border-zinc-800">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={handleAdvancedClick}
            leftIcon={<SlidersHorizontal className="h-3.5 w-3.5 text-cyan-400" />}
            className="w-full sm:w-auto"
          >
            Two-Column Power Editor →
          </Button>

          <div className="flex items-center gap-2 w-full sm:w-auto justify-end">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={onClose}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              variant="primary"
              size="sm"
              isLoading={isLoading}
              disabled={!name.trim()}
              leftIcon={<Rocket className="h-3.5 w-3.5" />}
            >
              Quick Deploy
            </Button>
          </div>
        </div>
      </form>
    </Modal>
  );
}
