import { useState, useEffect } from 'react';
import { useQuery } from '@tanstack/react-query';
import { api } from '../../lib/api';
import {
  App,
  Project,
  CreateAppRequest,
  UpdateAppRequest,
  InspectComposeResponse,
} from '../../lib/types';
import { SourceStrategyType } from './FastCreateModal';
import { EnvBulkPasteModal } from './EnvBulkPasteModal';
import { Card } from '../../components/ui/Card';
import { Button } from '../../components/ui/Button';
import { Input } from '../../components/ui/Input';
import { useToast } from '../../components/ui/Toast';
import {
  Code2,
  Box,
  GitBranch,
  GitCommit,
  FileCode,
  Globe,
  Lock,
  Key,
  Trash2,
  Sparkles,
  Layers,
  Server,
  Cpu,
  Tag as TagIcon,
  HardDrive,
  Check,
  Loader2,
  FileText,
  Rocket,
  Save,
  RotateCw,
  Eye,
  EyeOff,
  Sliders,
  ShieldCheck,
} from 'lucide-react';

const DEFAULT_COMPOSE_TEMPLATE = `version: '3.8'
services:
  web:
    image: nginx:alpine
    ports:
      - "80:80"
    environment:
      - APP_ENV=production
      - API_SECRET=\${API_SECRET}
    deploy:
      replicas: 2
`;

const DEFAULT_DOCKERFILE_TEMPLATE = `FROM node:18-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm install --ignore-scripts
COPY . .
RUN npm run build

FROM node:18-alpine AS runner
WORKDIR /app
ENV NODE_ENV=production
COPY --from=builder /app/dist ./dist
COPY --from=builder /app/node_modules ./node_modules
COPY package*.json ./
EXPOSE 3000
CMD ["npm", "start"]
`;

export interface TwoColumnConfigEditorProps {
  mode: 'create' | 'edit';
  initialApp?: App | null;
  projects: Project[];
  defaultProjectId?: string;
  defaultStageId?: string;
  defaultStrategy?: SourceStrategyType;
  defaultName?: string;
  defaultImage?: string;
  defaultGitRepo?: string;
  defaultGitBranch?: string;
  defaultDockerfile?: string;
  onSaveAndRedeploy?: (req: UpdateAppRequest | CreateAppRequest) => Promise<void> | void;
  onSaveChanges?: (req: UpdateAppRequest | CreateAppRequest) => Promise<void> | void;
  onCancel?: () => void;
  isLoading?: boolean;
}

interface PortMapping {
  hostPort: number;
  containerPort: number;
  protocol: 'tcp' | 'udp';
}

interface AttachedVolume {
  id: string;
  name: string;
  mountPath: string;
  driver: string;
}

export function TwoColumnConfigEditor({
  mode,
  initialApp,
  projects,
  defaultProjectId = 'prj_default',
  defaultStageId = 'stg_default_prod',
  defaultStrategy = 'compose',
  defaultName = '',
  defaultImage = '',
  defaultGitRepo = '',
  defaultGitBranch = 'main',
  defaultDockerfile = 'Dockerfile',
  onSaveAndRedeploy,
  onSaveChanges,
  onCancel,
  isLoading = false,
}: TwoColumnConfigEditorProps) {
  const toast = useToast();

  // Primary state
  const [name, setName] = useState(initialApp?.name || defaultName || '');
  const [projectId, setProjectId] = useState(
    initialApp?.project_id || defaultProjectId || 'prj_default'
  );
  const [stageId, setStageId] = useState(
    initialApp?.stage_id || defaultStageId || 'stg_default_prod'
  );
  const [strategy, setStrategy] = useState<SourceStrategyType>(
    initialApp
      ? initialApp.compose_yaml
        ? 'compose'
        : initialApp.git_repo_url
        ? 'git'
        : 'image'
      : defaultStrategy
  );

  // Monospace Editor State
  const [composeYAML, setComposeYAML] = useState(
    initialApp?.compose_yaml || DEFAULT_COMPOSE_TEMPLATE
  );
  const [dockerfileContent, setDockerfileContent] = useState(DEFAULT_DOCKERFILE_TEMPLATE);
  const [image, setImage] = useState(initialApp?.image || defaultImage || 'nginx:alpine');
  const [gitRepoUrl, setGitRepoUrl] = useState(
    initialApp?.git_repo_url || defaultGitRepo || ''
  );
  const [gitBranch, setGitBranch] = useState(
    initialApp?.git_branch || defaultGitBranch || 'main'
  );
  const [buildStrategy, setBuildStrategy] = useState<'dockerfile' | 'nixpacks' | 'compose'>(
    (initialApp?.build_strategy as any) || 'dockerfile'
  );
  const [dockerfilePath, setDockerfilePath] = useState(
    initialApp?.dockerfile_path || defaultDockerfile || 'Dockerfile'
  );
  const [webhookSecret, setWebhookSecret] = useState(initialApp?.webhook_secret || '');

  // Live Compose AST Inspection
  const [composeInspection, setComposeInspection] = useState<InspectComposeResponse | null>(null);
  const [isInspecting, setIsInspecting] = useState(false);
  const [inspectError, setInspectError] = useState<string | null>(null);

  // Secondary Controls State
  const [domainList, setDomainList] = useState<string[]>(initialApp?.domains || []);
  const [newDomain, setNewDomain] = useState('');

  const [runtimeMode, setRuntimeMode] = useState<'swarm' | 'standalone'>(
    (initialApp?.runtime_mode as 'swarm' | 'standalone') || 'swarm'
  );
  const [replicas, setReplicas] = useState<number>(initialApp?.replicas || 1);

  // Ports
  const [ports, setPorts] = useState<PortMapping[]>([
    {
      hostPort: initialApp?.container_port || 80,
      containerPort: initialApp?.container_port || 80,
      protocol: 'tcp',
    },
  ]);
  const [newHostPort, setNewHostPort] = useState('');
  const [newContainerPort, setNewContainerPort] = useState('');

  // Environment Variables Matrix
  const [envMap, setEnvMap] = useState<Record<string, string>>(initialApp?.env || {});
  const [showSecretMap, setShowSecretMap] = useState<Record<string, boolean>>({});
  const [newEnvKey, setNewEnvKey] = useState('');
  const [newEnvValue, setNewEnvValue] = useState('');
  const [isBulkPasteOpen, setIsBulkPasteOpen] = useState(false);

  // Tags
  const [tags, setTags] = useState<string[]>(initialApp?.tags || []);
  const [tagInput, setTagInput] = useState('');

  // Attached Scoped Volumes
  const [attachedVolumes, setAttachedVolumes] = useState<AttachedVolume[]>([
    {
      id: 'vol_1',
      name: `pikpik_vol_${name || 'app'}_data`,
      mountPath: '/app/data',
      driver: 'local',
    },
  ]);
  const [newVolMount, setNewVolMount] = useState('');
  const [newVolName, setNewVolName] = useState('');

  // Query existing volumes for suggestions
  const { data: projectVolumes } = useQuery({
    queryKey: ['volumes', projectId],
    queryFn: () => api.volumes.list(projectId),
  });

  // Debounced AST Inspection
  useEffect(() => {
    if (strategy !== 'compose' || !composeYAML.trim()) {
      return;
    }

    const timer = setTimeout(async () => {
      setIsInspecting(true);
      setInspectError(null);
      try {
        const res = await api.apps.inspectCompose(composeYAML);
        setComposeInspection(res);
        if (res.suggested_runtime === 'swarm' || res.suggested_runtime === 'standalone') {
          setRuntimeMode(res.suggested_runtime as 'swarm' | 'standalone');
        }
        if (!name && res.services.length > 0) {
          setName(res.services[0].name);
        }
        // Auto-merge detected variables with default values if not already defined
        setEnvMap((prev) => {
          const next = { ...prev };
          for (const v of res.variables) {
            if (next[v.name] === undefined && v.defaultValue) {
              next[v.name] = v.defaultValue;
            }
          }
          return next;
        });
      } catch (err: any) {
        setInspectError(err?.message || 'Invalid Compose YAML syntax');
      } finally {
        setIsInspecting(false);
      }
    }, 350);

    return () => clearTimeout(timer);
  }, [composeYAML, strategy, name]);

  // Keep domains array in sync
  const handleAddDomain = () => {
    if (!newDomain.trim()) return;
    const clean = newDomain.trim().toLowerCase();
    if (!domainList.includes(clean)) {
      setDomainList([...domainList, clean]);
    }
    setNewDomain('');
  };

  const handleRemoveDomain = (d: string) => {
    setDomainList(domainList.filter((item) => item !== d));
  };

  // Environment matrix helpers
  const handleAddEnv = () => {
    if (!newEnvKey.trim()) return;
    const key = newEnvKey.trim().toUpperCase();
    setEnvMap((prev) => ({ ...prev, [key]: newEnvValue }));
    setNewEnvKey('');
    setNewEnvValue('');
  };

  const handleRemoveEnv = (key: string) => {
    setEnvMap((prev) => {
      const next = { ...prev };
      delete next[key];
      return next;
    });
  };

  const generateRandomToken = (key: string) => {
    const randValues = new Uint8Array(16);
    window.crypto.getRandomValues(randValues);
    const token = Array.from(randValues)
      .map((b) => b.toString(16).padStart(2, '0'))
      .join('');
    setEnvMap((prev) => ({ ...prev, [key]: token }));
    toast.info('Secret Generated', `Assigned random 32-char token to ${key}`);
  };

  const handleBulkEnvApply = (parsed: Record<string, string>, applyMode: 'merge' | 'replace') => {
    if (applyMode === 'replace') {
      setEnvMap(parsed);
    } else {
      setEnvMap((prev) => ({ ...prev, ...parsed }));
    }
    toast.success('Environment Imported', `Loaded ${Object.keys(parsed).length} variables`);
  };

  // Tags helpers
  const handleAddTag = () => {
    if (!tagInput.trim()) return;
    const clean = tagInput.trim().toLowerCase();
    if (!tags.includes(clean)) {
      setTags([...tags, clean]);
    }
    setTagInput('');
  };

  const handleRemoveTag = (t: string) => {
    setTags(tags.filter((item) => item !== t));
  };

  // Port helpers
  const handleAddPort = () => {
    const hp = parseInt(newHostPort);
    const cp = parseInt(newContainerPort);
    if (!hp || !cp) return;
    setPorts([...ports, { hostPort: hp, containerPort: cp, protocol: 'tcp' }]);
    setNewHostPort('');
    setNewContainerPort('');
  };

  const handleRemovePort = (index: number) => {
    setPorts(ports.filter((_, idx) => idx !== index));
  };

  // Volume helpers
  const handleAddVolume = () => {
    if (!newVolMount.trim()) return;
    const volName =
      newVolName.trim() || `pikpik_vol_${name || 'app'}_${attachedVolumes.length + 1}`;
    setAttachedVolumes([
      ...attachedVolumes,
      {
        id: `vol_${Date.now()}`,
        name: volName,
        mountPath: newVolMount.trim(),
        driver: 'local',
      },
    ]);
    setNewVolMount('');
    setNewVolName('');
  };

  const handleRemoveVolume = (id: string) => {
    setAttachedVolumes(attachedVolumes.filter((v) => v.id !== id));
  };

  // Build payload
  const buildPayload = (): CreateAppRequest | UpdateAppRequest => {
    const primaryPort = ports.length > 0 ? ports[0].containerPort : 80;
    const primaryImage =
      strategy === 'compose'
        ? composeInspection?.services[0]?.image || image || 'compose-app:latest'
        : strategy === 'git'
        ? `${name || 'app'}:latest`
        : image;

    return {
      project_id: projectId,
      stage_id: stageId,
      name: name.trim() || 'unnamed-app',
      image: primaryImage,
      replicas: runtimeMode === 'swarm' ? Number(replicas) || 1 : 1,
      container_port: primaryPort,
      domains: domainList,
      tags: tags,
      runtime_mode: runtimeMode,
      compose_yaml: strategy === 'compose' ? composeYAML : undefined,
      env: envMap,
      git_repo_url: strategy === 'git' ? gitRepoUrl.trim() : undefined,
      git_branch: strategy === 'git' ? gitBranch.trim() || 'main' : undefined,
      build_strategy: strategy === 'git' ? buildStrategy : undefined,
      dockerfile_path:
        strategy === 'git' || strategy === 'dockerfile'
          ? dockerfilePath.trim() || 'Dockerfile'
          : undefined,
      webhook_secret: strategy === 'git' && webhookSecret.trim() ? webhookSecret.trim() : undefined,
    };
  };

  const handleSaveAndRedeploy = async () => {
    if (!name.trim()) {
      toast.error('Validation Error', 'Application name is required');
      return;
    }
    const payload = buildPayload();
    if (onSaveAndRedeploy) {
      await onSaveAndRedeploy(payload);
    }
  };

  const handleSaveChanges = async () => {
    if (!name.trim()) {
      toast.error('Validation Error', 'Application name is required');
      return;
    }
    const payload = buildPayload();
    if (onSaveChanges) {
      await onSaveChanges(payload);
    }
  };

  return (
    <div className="space-y-6">
      {/* Top Bar / Configuration Context */}
      <div className="p-4 bg-zinc-900/90 rounded-xl border border-zinc-800 flex flex-col md:flex-row md:items-center justify-between gap-4">
        <div className="flex items-center gap-3">
          <div className="p-2.5 rounded-lg bg-cyan-950/60 border border-cyan-800/50 text-cyan-400">
            <Sliders className="h-5 w-5" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <h2 className="text-base font-bold text-zinc-100 font-mono">
                {name || 'New Application'}
              </h2>
              <span
                className={`text-[10px] font-mono uppercase px-2 py-0.5 rounded font-semibold ${
                  stageId.includes('staging')
                    ? 'bg-amber-500/10 text-amber-400 border border-amber-500/20'
                    : 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20'
                }`}
              >
                {stageId.includes('staging') ? 'staging' : 'prod'}
              </span>
              <span className="text-[10px] font-mono text-zinc-400 bg-zinc-800 px-1.5 py-0.5 rounded">
                {runtimeMode.toUpperCase()}
              </span>
            </div>
            <p className="text-xs text-zinc-400 mt-0.5">
              Two-Column Power Configuration Editor with AST Inspection & Dynamic Routing
            </p>
          </div>
        </div>

        {/* Action Controls */}
        <div className="flex items-center gap-2 flex-wrap">
          {onCancel && (
            <Button variant="outline" size="sm" onClick={onCancel} disabled={isLoading}>
              Discard
            </Button>
          )}

          {mode === 'edit' && onSaveChanges && (
            <Button
              variant="secondary"
              size="sm"
              onClick={handleSaveChanges}
              isLoading={isLoading}
              leftIcon={<Save className="h-3.5 w-3.5 text-zinc-300" />}
            >
              Save Changes
            </Button>
          )}

          <Button
            variant="primary"
            size="sm"
            onClick={handleSaveAndRedeploy}
            isLoading={isLoading}
            leftIcon={
              mode === 'edit' ? (
                <RotateCw className="h-3.5 w-3.5 text-zinc-950" />
              ) : (
                <Rocket className="h-3.5 w-3.5 text-zinc-950" />
              )
            }
          >
            {mode === 'edit' ? 'Save & Redeploy' : 'Deploy Application'}
          </Button>
        </div>
      </div>

      {/* Two-Column Grid */}
      <div className="grid grid-cols-1 lg:grid-cols-12 gap-6 items-start">
        {/* ================= LEFT COLUMN: Main Controls & Monospace Editor (7 cols) ================= */}
        <div className="lg:col-span-7 space-y-5">
          {/* Main Controls Card */}
          <Card className="p-4 space-y-4 bg-zinc-950/60 border-zinc-800">
            <div className="flex items-center justify-between border-b border-zinc-800/80 pb-3">
              <span className="text-xs font-bold text-zinc-200 uppercase tracking-wider flex items-center gap-1.5">
                <Box className="h-4 w-4 text-cyan-400" />
                Primary Specification
              </span>
              <span className="text-[11px] text-zinc-500 font-mono">
                Project Workspace: {projectId}
              </span>
            </div>

            {/* Name, Project, Stage */}
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
              <div className="sm:col-span-1">
                <Input
                  label="App Name"
                  placeholder="e.g. backend-api"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  required
                />
              </div>

              <div>
                <label className="block text-xs font-medium text-zinc-300 mb-1.5">
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
                        {p.name}
                      </option>
                    ))}
                </select>
              </div>

              <div>
                <label className="block text-xs font-medium text-zinc-300 mb-1.5">
                  Stage
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

            {/* Source Strategy Selector */}
            <div className="space-y-1.5 pt-1">
              <label className="block text-xs font-medium text-zinc-300">
                Source Strategy
              </label>
              <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
                {[
                  {
                    id: 'compose',
                    label: 'Docker Compose',
                    icon: <Code2 className="h-3.5 w-3.5 text-cyan-400" />,
                  },
                  {
                    id: 'image',
                    label: 'Single Image',
                    icon: <Box className="h-3.5 w-3.5 text-blue-400" />,
                  },
                  {
                    id: 'git',
                    label: 'Git Repository',
                    icon: <GitBranch className="h-3.5 w-3.5 text-emerald-400" />,
                  },
                  {
                    id: 'dockerfile',
                    label: 'Dockerfile',
                    icon: <FileCode className="h-3.5 w-3.5 text-amber-400" />,
                  },
                ].map((s) => {
                  const isSelected = strategy === s.id;
                  return (
                    <button
                      key={s.id}
                      type="button"
                      onClick={() => setStrategy(s.id as SourceStrategyType)}
                      className={`p-2 rounded-lg border text-xs font-medium transition-all flex items-center justify-center gap-1.5 ${
                        isSelected
                          ? 'border-cyan-500 bg-cyan-950/30 text-cyan-300 ring-1 ring-cyan-500 font-semibold'
                          : 'border-zinc-800 bg-zinc-900/60 text-zinc-400 hover:text-zinc-200'
                      }`}
                    >
                      {s.icon}
                      <span>{s.label}</span>
                    </button>
                  );
                })}
              </div>
            </div>
          </Card>

          {/* Code & Configuration Editor */}
          <Card className="p-4 space-y-4 bg-zinc-950/60 border-zinc-800">
            {strategy === 'compose' && (
              <div className="space-y-3">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <Code2 className="h-4 w-4 text-cyan-400" />
                    <span className="text-xs font-semibold text-zinc-200">
                      Docker Compose Blueprint YAML
                    </span>
                  </div>
                  <div className="flex items-center gap-3">
                    {isInspecting ? (
                      <span className="text-[11px] text-cyan-400 font-mono flex items-center gap-1">
                        <Loader2 className="h-3 w-3 animate-spin" />
                        Analyzing AST...
                      </span>
                    ) : composeInspection ? (
                      <span className="text-[11px] text-emerald-400 font-mono flex items-center gap-1">
                        <Check className="h-3 w-3" />
                        Valid Compose AST
                      </span>
                    ) : null}
                    <button
                      type="button"
                      onClick={() => setComposeYAML(DEFAULT_COMPOSE_TEMPLATE)}
                      className="text-[11px] text-zinc-500 hover:text-zinc-300 font-mono transition-colors"
                    >
                      Reset Sample
                    </button>
                  </div>
                </div>

                {/* Monospace Code Editor */}
                <div className="relative rounded-lg border border-zinc-800 bg-zinc-950 overflow-hidden">
                  <textarea
                    value={composeYAML}
                    onChange={(e) => setComposeYAML(e.target.value)}
                    rows={14}
                    className="w-full bg-transparent font-mono text-xs text-zinc-100 p-3 leading-relaxed focus:outline-none focus:ring-1 focus:ring-cyan-500 selection:bg-cyan-900/60"
                    placeholder="version: '3.8'..."
                    spellCheck={false}
                  />
                </div>

                {inspectError && (
                  <div className="p-2.5 rounded-lg bg-rose-950/30 border border-rose-900/50 text-xs font-mono text-rose-400">
                    ⚠ AST Parsing Notice: {inspectError}
                  </div>
                )}

                {/* Detected Services & Blueprint Variables */}
                {composeInspection && (
                  <div className="space-y-3 pt-2">
                    {/* Detected Services */}
                    {composeInspection.services.length > 0 && (
                      <div className="p-3 bg-zinc-900/80 rounded-lg border border-zinc-800 space-y-2">
                        <span className="text-[11px] font-semibold text-zinc-300 flex items-center gap-1.5">
                          <Cpu className="h-3.5 w-3.5 text-cyan-400" />
                          Detected Services Topology ({composeInspection.services.length})
                        </span>
                        <div className="flex flex-wrap gap-2">
                          {composeInspection.services.map((s) => (
                            <div
                              key={s.name}
                              className="px-2.5 py-1.5 rounded-md bg-zinc-950 border border-zinc-800 text-xs font-mono flex items-center gap-2"
                            >
                              <Box className="h-3.5 w-3.5 text-cyan-400" />
                              <span className="text-zinc-100 font-semibold">{s.name}</span>
                              <span className="text-zinc-500 text-[10px]">({s.image})</span>
                              {s.ports && s.ports.length > 0 && (
                                <span className="text-cyan-300 bg-cyan-950/80 px-1 rounded text-[10px]">
                                  :{s.ports.map((p) => p.containerPort || p.hostPort).join(', :')}
                                </span>
                              )}
                            </div>
                          ))}
                        </div>
                      </div>
                    )}

                    {/* Detected Variables with 1-click token generators */}
                    {composeInspection.variables.length > 0 && (
                      <div className="p-3 bg-zinc-900/80 rounded-lg border border-zinc-800 space-y-2">
                        <div className="flex items-center justify-between">
                          <span className="text-[11px] font-semibold text-zinc-300 flex items-center gap-1.5">
                            <Key className="h-3.5 w-3.5 text-amber-400" />
                            Detected Compose Variables ({composeInspection.variables.length})
                          </span>
                          <span className="text-[10px] text-zinc-500">Auto-mapped to Environment Matrix</span>
                        </div>
                        <div className="space-y-1.5">
                          {composeInspection.variables.map((v) => (
                            <div
                              key={v.name}
                              className="p-2 rounded bg-zinc-950 border border-zinc-800/80 flex items-center justify-between text-xs"
                            >
                              <div className="flex items-center gap-2">
                                <span className="font-mono font-semibold text-cyan-300">{v.name}</span>
                                {v.isSecret && (
                                  <span className="text-[9px] px-1 rounded bg-amber-950 text-amber-400 border border-amber-800/40">
                                    SECRET
                                  </span>
                                )}
                              </div>
                              <div className="flex items-center gap-2">
                                <span className="font-mono text-zinc-400 text-xs truncate max-w-xs">
                                  {envMap[v.name] ? '••••••••' : '(empty)'}
                                </span>
                                {v.isSecret && (
                                  <button
                                    type="button"
                                    onClick={() => generateRandomToken(v.name)}
                                    className="px-2 py-0.5 rounded bg-zinc-800 hover:bg-zinc-700 text-zinc-200 text-[10px] font-mono flex items-center gap-1 transition-colors"
                                    title="Generate 32-char secret token"
                                  >
                                    <Sparkles className="h-3 w-3 text-amber-400" />
                                    🎲 Gen
                                  </button>
                                )}
                              </div>
                            </div>
                          ))}
                        </div>
                      </div>
                    )}
                  </div>
                )}
              </div>
            )}

            {strategy === 'image' && (
              <div className="space-y-4">
                <Input
                  label="Target Container Image Tag"
                  placeholder="e.g. nginx:alpine, redis:7-alpine, ghcr.io/org/repo:v1.0.0"
                  value={image}
                  onChange={(e) => setImage(e.target.value)}
                  required
                  leftIcon={<Box className="h-3.5 w-3.5 text-blue-400" />}
                  helperText="Supports public registries and configured private robot credentials"
                />

                <div className="p-3 bg-zinc-900/60 rounded-lg border border-zinc-800 space-y-2">
                  <span className="text-xs font-semibold text-zinc-300 block">
                    Quick Preset Images
                  </span>
                  <div className="flex flex-wrap gap-1.5">
                    {[
                      'nginx:alpine',
                      'redis:7-alpine',
                      'postgres:16-alpine',
                      'node:18-alpine',
                      'caddy:2-alpine',
                      'traefik:v3.0',
                    ].map((preset) => (
                      <button
                        key={preset}
                        type="button"
                        onClick={() => setImage(preset)}
                        className={`px-2 py-1 rounded text-xs font-mono transition-colors ${
                          image === preset
                            ? 'bg-cyan-500 text-zinc-950 font-bold'
                            : 'bg-zinc-800 text-zinc-300 hover:bg-zinc-700'
                        }`}
                      >
                        {preset}
                      </button>
                    ))}
                  </div>
                </div>
              </div>
            )}

            {strategy === 'git' && (
              <div className="space-y-4">
                <Input
                  label="Git Repository URL"
                  placeholder="https://github.com/org/repo.git"
                  value={gitRepoUrl}
                  onChange={(e) => setGitRepoUrl(e.target.value)}
                  required
                  leftIcon={<GitBranch className="h-3.5 w-3.5 text-emerald-400" />}
                />

                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                  <Input
                    label="Target Branch"
                    placeholder="main"
                    value={gitBranch}
                    onChange={(e) => setGitBranch(e.target.value)}
                    leftIcon={<GitCommit className="h-3.5 w-3.5" />}
                  />

                  <div className="space-y-1.5">
                    <label className="block text-xs font-medium text-zinc-300">
                      Build Strategy
                    </label>
                    <select
                      value={buildStrategy}
                      onChange={(e) => setBuildStrategy(e.target.value as any)}
                      className="w-full rounded-lg border border-zinc-800 bg-zinc-900 px-3 py-2 text-xs text-zinc-100 focus:border-cyan-500 focus:ring-1 focus:ring-cyan-500 focus:outline-none"
                    >
                      <option value="dockerfile">Dockerfile (Standard)</option>
                      <option value="nixpacks">Nixpacks (Auto-detect)</option>
                      <option value="compose">Docker Compose Build</option>
                    </select>
                  </div>
                </div>

                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                  <Input
                    label="Dockerfile Path"
                    placeholder="Dockerfile"
                    value={dockerfilePath}
                    onChange={(e) => setDockerfilePath(e.target.value)}
                    helperText="Relative path inside the Git repository"
                  />

                  <Input
                    label="Webhook Deploy Secret"
                    placeholder="Auto-trigger token"
                    type="password"
                    value={webhookSecret}
                    onChange={(e) => setWebhookSecret(e.target.value)}
                    helperText="Secures incoming push webhook triggers"
                  />
                </div>
              </div>
            )}

            {strategy === 'dockerfile' && (
              <div className="space-y-3">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-semibold text-zinc-200 flex items-center gap-1.5">
                    <FileCode className="h-4 w-4 text-amber-400" />
                    Custom Multi-Stage Dockerfile
                  </span>
                  <button
                    type="button"
                    onClick={() => setDockerfileContent(DEFAULT_DOCKERFILE_TEMPLATE)}
                    className="text-[11px] text-zinc-500 hover:text-zinc-300 font-mono transition-colors"
                  >
                    Reset Template
                  </button>
                </div>

                <div className="relative rounded-lg border border-zinc-800 bg-zinc-950 overflow-hidden">
                  <textarea
                    value={dockerfileContent}
                    onChange={(e) => setDockerfileContent(e.target.value)}
                    rows={12}
                    className="w-full bg-transparent font-mono text-xs text-zinc-100 p-3 leading-relaxed focus:outline-none focus:ring-1 focus:ring-cyan-500"
                    spellCheck={false}
                  />
                </div>

                <Input
                  label="Target Dockerfile Filename / Path"
                  placeholder="Dockerfile"
                  value={dockerfilePath}
                  onChange={(e) => setDockerfilePath(e.target.value)}
                />
              </div>
            )}
          </Card>
        </div>

        {/* ================= RIGHT COLUMN: Secondary Controls (5 cols) ================= */}
        <div className="lg:col-span-5 space-y-5">
          {/* Ingress & Custom Domain Bindings */}
          <Card className="p-4 space-y-3 bg-zinc-950/60 border-zinc-800">
            <div className="flex items-center justify-between border-b border-zinc-800/80 pb-2.5">
              <div className="flex items-center gap-1.5">
                <Globe className="h-4 w-4 text-cyan-400" />
                <span className="text-xs font-bold text-zinc-200">Ingress & Custom Domains</span>
              </div>
              <span className="px-2 py-0.5 rounded text-[10px] font-mono bg-emerald-950 text-emerald-400 border border-emerald-800/40 flex items-center gap-1">
                <ShieldCheck className="h-3 w-3" />
                Auto-TLS (Caddy)
              </span>
            </div>

            {/* Domain Add Bar */}
            <div className="flex gap-2">
              <Input
                placeholder="app.example.com"
                value={newDomain}
                onChange={(e) => setNewDomain(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault();
                    handleAddDomain();
                  }
                }}
                className="text-xs font-mono"
              />
              <Button type="button" variant="secondary" size="sm" onClick={handleAddDomain}>
                Add
              </Button>
            </div>

            {/* Domains List */}
            <div className="space-y-1.5">
              {domainList.length === 0 ? (
                <div className="p-3 text-center text-zinc-500 text-xs rounded border border-dashed border-zinc-800">
                  No custom domain bound yet.
                </div>
              ) : (
                domainList.map((d) => (
                  <div
                    key={d}
                    className="p-2 rounded bg-zinc-900 border border-zinc-800 flex items-center justify-between text-xs"
                  >
                    <div className="flex items-center gap-1.5">
                      <Lock className="h-3 w-3 text-emerald-400" />
                      <span className="font-mono text-zinc-200 font-semibold">{d}</span>
                    </div>
                    <button
                      type="button"
                      onClick={() => handleRemoveDomain(d)}
                      className="text-zinc-500 hover:text-rose-400 transition-colors"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  </div>
                ))
              )}
            </div>
          </Card>

          {/* Exposed Ports Mapping */}
          <Card className="p-4 space-y-3 bg-zinc-950/60 border-zinc-800">
            <div className="flex items-center justify-between border-b border-zinc-800/80 pb-2.5">
              <span className="text-xs font-bold text-zinc-200 flex items-center gap-1.5">
                <Server className="h-4 w-4 text-purple-400" />
                Exposed Port Mappings
              </span>
              <span className="text-[10px] text-zinc-500 font-mono">Host : Container [TCP]</span>
            </div>

            <div className="flex gap-2">
              <Input
                placeholder="Host (80)"
                type="number"
                value={newHostPort}
                onChange={(e) => setNewHostPort(e.target.value)}
                className="text-xs font-mono"
              />
              <Input
                placeholder="Container (80)"
                type="number"
                value={newContainerPort}
                onChange={(e) => setNewContainerPort(e.target.value)}
                className="text-xs font-mono"
              />
              <Button type="button" variant="secondary" size="sm" onClick={handleAddPort}>
                Map
              </Button>
            </div>

            <div className="space-y-1.5">
              {ports.map((p, idx) => (
                <div
                  key={idx}
                  className="p-2 rounded bg-zinc-900 border border-zinc-800 flex items-center justify-between text-xs font-mono"
                >
                  <div className="flex items-center gap-2">
                    <span className="text-purple-400 font-semibold">{p.hostPort}</span>
                    <span className="text-zinc-500">→</span>
                    <span className="text-zinc-200">{p.containerPort}</span>
                    <span className="text-[10px] text-zinc-500 uppercase">({p.protocol})</span>
                  </div>
                  {ports.length > 1 && (
                    <button
                      type="button"
                      onClick={() => handleRemovePort(idx)}
                      className="text-zinc-500 hover:text-rose-400 transition-colors"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  )}
                </div>
              ))}
            </div>
          </Card>

          {/* Encrypted Environment Variables Key-Value Matrix */}
          <Card className="p-4 space-y-3 bg-zinc-950/60 border-zinc-800">
            <div className="flex items-center justify-between border-b border-zinc-800/80 pb-2.5">
              <div className="flex items-center gap-1.5">
                <Key className="h-4 w-4 text-amber-400" />
                <span className="text-xs font-bold text-zinc-200">
                  Environment & Secrets Matrix ({Object.keys(envMap).length})
                </span>
              </div>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => setIsBulkPasteOpen(true)}
                leftIcon={<FileText className="h-3 w-3 text-cyan-400" />}
              >
                .env Paste
              </Button>
            </div>

            {/* Quick Add Row */}
            <div className="flex gap-2">
              <Input
                placeholder="KEY"
                value={newEnvKey}
                onChange={(e) => setNewEnvKey(e.target.value.toUpperCase())}
                className="text-xs font-mono uppercase"
              />
              <Input
                placeholder="VALUE"
                value={newEnvValue}
                onChange={(e) => setNewEnvValue(e.target.value)}
                className="text-xs font-mono"
              />
              <Button type="button" variant="secondary" size="sm" onClick={handleAddEnv}>
                Add
              </Button>
            </div>

            {/* Matrix List */}
            <div className="rounded-lg border border-zinc-800 divide-y divide-zinc-800 max-h-56 overflow-y-auto bg-zinc-950">
              {Object.keys(envMap).length === 0 ? (
                <div className="p-4 text-center text-zinc-500 text-xs">
                  No environment variables defined.
                </div>
              ) : (
                Object.entries(envMap).map(([k, v]) => {
                  const isVisible = showSecretMap[k];
                  return (
                    <div
                      key={k}
                      className="p-2.5 flex items-center justify-between text-xs hover:bg-zinc-900/60 transition-colors"
                    >
                      <span className="font-mono font-semibold text-cyan-300 truncate max-w-[120px]">
                        {k}
                      </span>
                      <div className="flex items-center gap-2">
                        <span className="font-mono text-zinc-300 truncate max-w-[140px]">
                          {isVisible ? v : '••••••••••••'}
                        </span>
                        <button
                          type="button"
                          onClick={() =>
                            setShowSecretMap((prev) => ({ ...prev, [k]: !prev[k] }))
                          }
                          className="text-zinc-500 hover:text-zinc-300"
                          title={isVisible ? 'Hide value' : 'Show value'}
                        >
                          {isVisible ? (
                            <EyeOff className="h-3.5 w-3.5" />
                          ) : (
                            <Eye className="h-3.5 w-3.5" />
                          )}
                        </button>
                        <button
                          type="button"
                          onClick={() => generateRandomToken(k)}
                          className="text-zinc-500 hover:text-amber-400"
                          title="Generate 32-char token"
                        >
                          <Sparkles className="h-3.5 w-3.5" />
                        </button>
                        <button
                          type="button"
                          onClick={() => handleRemoveEnv(k)}
                          className="text-zinc-500 hover:text-rose-400"
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </button>
                      </div>
                    </div>
                  );
                })
              )}
            </div>
          </Card>

          {/* Replicas & Swarm Scaling */}
          <Card className="p-4 space-y-3 bg-zinc-950/60 border-zinc-800">
            <div className="flex items-center justify-between border-b border-zinc-800/80 pb-2.5">
              <span className="text-xs font-bold text-zinc-200 flex items-center gap-1.5">
                <Layers className="h-4 w-4 text-purple-400" />
                Scale & Engine Controls
              </span>
              <span className="text-[10px] text-zinc-500 font-mono">
                {runtimeMode === 'swarm' ? 'Swarm Ingress Mesh' : 'Direct Host Container'}
              </span>
            </div>

            <div className="grid grid-cols-2 gap-2">
              <button
                type="button"
                onClick={() => setRuntimeMode('swarm')}
                className={`p-2.5 rounded-lg border text-left transition-all ${
                  runtimeMode === 'swarm'
                    ? 'border-purple-500 bg-purple-950/30 text-purple-300 ring-1 ring-purple-500'
                    : 'border-zinc-800 bg-zinc-900/60 text-zinc-400 hover:bg-zinc-900'
                }`}
              >
                <div className="font-semibold text-xs text-zinc-100 flex items-center justify-between">
                  <span>Docker Swarm</span>
                  {runtimeMode === 'swarm' && <Check className="h-3 w-3 text-purple-400" />}
                </div>
                <p className="text-[10px] text-zinc-400 mt-0.5">Multi-replica rolling updates</p>
              </button>

              <button
                type="button"
                onClick={() => setRuntimeMode('standalone')}
                className={`p-2.5 rounded-lg border text-left transition-all ${
                  runtimeMode === 'standalone'
                    ? 'border-cyan-500 bg-cyan-950/30 text-cyan-300 ring-1 ring-cyan-500'
                    : 'border-zinc-800 bg-zinc-900/60 text-zinc-400 hover:bg-zinc-900'
                }`}
              >
                <div className="font-semibold text-xs text-zinc-100 flex items-center justify-between">
                  <span>Standalone</span>
                  {runtimeMode === 'standalone' && <Check className="h-3 w-3 text-cyan-400" />}
                </div>
                <p className="text-[10px] text-zinc-400 mt-0.5">Single-node container direct</p>
              </button>
            </div>

            {runtimeMode === 'swarm' && (
              <div className="space-y-1.5 pt-1">
                <div className="flex items-center justify-between text-xs">
                  <label className="font-medium text-zinc-300">Target Replicas</label>
                  <span className="font-mono font-bold text-cyan-400">{replicas} Replicas</span>
                </div>
                <input
                  type="range"
                  min="1"
                  max="32"
                  value={replicas}
                  onChange={(e) => setReplicas(parseInt(e.target.value) || 1)}
                  className="w-full accent-cyan-500 cursor-pointer"
                />
              </div>
            )}
          </Card>

          {/* Tags Taxonomy */}
          <Card className="p-4 space-y-3 bg-zinc-950/60 border-zinc-800">
            <div className="flex items-center justify-between border-b border-zinc-800/80 pb-2.5">
              <span className="text-xs font-bold text-zinc-200 flex items-center gap-1.5">
                <TagIcon className="h-4 w-4 text-cyan-400" />
                Tags Taxonomy
              </span>
              <span className="text-[10px] text-zinc-500">Supports key:value</span>
            </div>

            <div className="flex gap-2">
              <Input
                placeholder="team:billing or ecommerce"
                value={tagInput}
                onChange={(e) => setTagInput(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault();
                    handleAddTag();
                  }
                }}
                className="text-xs font-mono"
              />
              <Button type="button" variant="secondary" size="sm" onClick={handleAddTag}>
                Tag
              </Button>
            </div>

            <div className="flex flex-wrap gap-1.5">
              {tags.map((t) => (
                <span
                  key={t}
                  className="bg-zinc-800 text-zinc-200 px-2 py-0.5 rounded text-xs font-mono flex items-center gap-1.5 border border-zinc-700"
                >
                  <span>{t}</span>
                  <button
                    type="button"
                    onClick={() => handleRemoveTag(t)}
                    className="text-zinc-400 hover:text-rose-400"
                  >
                    ×
                  </button>
                </span>
              ))}
            </div>
          </Card>

          {/* Persistent Scoped Volumes */}
          <Card className="p-4 space-y-3 bg-zinc-950/60 border-zinc-800">
            <div className="flex items-center justify-between border-b border-zinc-800/80 pb-2.5">
              <span className="text-xs font-bold text-zinc-200 flex items-center gap-1.5">
                <HardDrive className="h-4 w-4 text-emerald-400" />
                Attached Persistent Volumes
              </span>
              <span className="text-[10px] text-zinc-500 font-mono">Scoped Scaffolding</span>
            </div>

            <div className="flex gap-2">
              <Input
                placeholder="Mount Path (/app/data)"
                value={newVolMount}
                onChange={(e) => setNewVolMount(e.target.value)}
                className="text-xs font-mono"
              />
              <Button type="button" variant="secondary" size="sm" onClick={handleAddVolume}>
                Attach
              </Button>
            </div>

            {projectVolumes && projectVolumes.length > 0 && (
              <div className="flex items-center gap-1.5 flex-wrap pt-1">
                <span className="text-[10px] text-zinc-500 font-mono">Workspace Volumes:</span>
                {projectVolumes.map((pv) => (
                  <button
                    key={pv.id}
                    type="button"
                    onClick={() => setNewVolName(pv.name)}
                    className="px-1.5 py-0.5 rounded bg-zinc-900 border border-zinc-800 text-[10px] font-mono text-cyan-400 hover:border-cyan-500/50 transition-colors"
                  >
                    {pv.name}
                  </button>
                ))}
              </div>
            )}

            <div className="space-y-1.5">
              {attachedVolumes.map((v) => (
                <div
                  key={v.id}
                  className="p-2 rounded bg-zinc-900 border border-zinc-800 flex items-center justify-between text-xs font-mono"
                >
                  <div>
                    <span className="text-emerald-400 font-semibold">{v.name}</span>
                    <span className="text-zinc-400 ml-2">→ {v.mountPath}</span>
                  </div>
                  <button
                    type="button"
                    onClick={() => handleRemoveVolume(v.id)}
                    className="text-zinc-500 hover:text-rose-400 transition-colors"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </button>
                </div>
              ))}
            </div>
          </Card>
        </div>
      </div>

      {/* Bulk Paste Modal */}
      <EnvBulkPasteModal
        isOpen={isBulkPasteOpen}
        onClose={() => setIsBulkPasteOpen(false)}
        onApply={handleBulkEnvApply}
      />
    </div>
  );
}
