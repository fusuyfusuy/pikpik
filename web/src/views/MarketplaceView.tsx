import { useState, useMemo } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../lib/api';
import { Template, DeployTemplateRequest, DeployTemplateResponse } from '../lib/types';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Badge } from '../components/ui/Badge';
import { Input } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { useToast } from '../components/ui/Toast';
import {
  Store,
  Search,
  Zap,
  Sparkles,
  ExternalLink,
  Eye,
  EyeOff,
  RefreshCw,
  Sliders,
  CheckCircle2,
} from 'lucide-react';

const CATEGORIES = [
  'All',
  'DevTools',
  'Analytics',
  'CMS',
  'Databases',
  'Productivity',
] as const;

// Helper to generate secure random strings
function generateSecureToken(type: string = 'hex_32'): string {
  const randValues = new Uint8Array(24);
  window.crypto.getRandomValues(randValues);

  if (type === 'pass_16') {
    const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789!@#$%^&*';
    let res = '';
    for (let i = 0; i < 16; i++) {
      res += chars.charAt(randValues[i % randValues.length] % chars.length);
    }
    return res;
  } else if (type === 'base64_32') {
    return btoa(String.fromCharCode(...randValues)).replace(/[^a-zA-Z0-9]/g, '').slice(0, 32);
  } else {
    // Default hex_32
    return Array.from(randValues.slice(0, 16))
      .map((b) => b.toString(16).padStart(2, '0'))
      .join('');
  }
}

export function MarketplaceView() {
  const queryClient = useQueryClient();
  const toast = useToast();

  const [selectedCategory, setSelectedCategory] = useState<string>('All');
  const [searchQuery, setSearchQuery] = useState<string>('');

  // Deploy Wizard Modal State
  const [selectedTemplate, setSelectedTemplate] = useState<Template | null>(null);
  const [appName, setAppName] = useState('');
  const [domain, setDomain] = useState('');
  const [envVars, setEnvVars] = useState<Record<string, string>>({});
  const [showSecrets, setShowSecrets] = useState<Record<string, boolean>>({});
  const [deployResult, setDeployResult] = useState<DeployTemplateResponse | null>(null);

  const { data: templates, isLoading } = useQuery({
    queryKey: ['templates', selectedCategory, searchQuery],
    queryFn: () => api.templates.list(selectedCategory, searchQuery),
  });

  const deployMutation = useMutation({
    mutationFn: ({ id, req }: { id: string; req: DeployTemplateRequest }) =>
      api.templates.deploy(id, req),
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ['apps'] });
      queryClient.invalidateQueries({ queryKey: ['stacks'] });
      setDeployResult(res);
      toast.success(
        '1-Click Deploy Succeeded',
        `${res.name} deployed successfully across ${res.services.length} services`
      );
    },
    onError: (err: Error) => toast.error('Deployment Failed', err.message),
  });

  // Open Wizard and pre-populate defaults & auto-generated secrets
  const handleOpenDeploy = (template: Template) => {
    setSelectedTemplate(template);
    setAppName(`${template.id}-${Math.floor(1000 + Math.random() * 9000)}`);
    setDomain(`${template.id}.local`);
    setDeployResult(null);

    const initialVars: Record<string, string> = {};
    const initialSecrets: Record<string, boolean> = {};

    template.env_vars?.forEach((ev) => {
      if (ev.auto_generate) {
        initialVars[ev.key] = generateSecureToken(ev.auto_generate);
      } else if (ev.default) {
        initialVars[ev.key] = ev.default;
      } else if (ev.is_secret) {
        initialVars[ev.key] = generateSecureToken('pass_16');
      } else {
        initialVars[ev.key] = '';
      }
      if (ev.is_secret) {
        initialSecrets[ev.key] = false;
      }
    });

    setEnvVars(initialVars);
    setShowSecrets(initialSecrets);
  };

  const handleGenerateAll = () => {
    if (!selectedTemplate?.env_vars) return;
    const updated = { ...envVars };
    selectedTemplate.env_vars.forEach((ev) => {
      if (ev.is_secret || ev.auto_generate) {
        updated[ev.key] = generateSecureToken(ev.auto_generate || 'pass_16');
      }
    });
    setEnvVars(updated);
    toast.info('Secrets Regenerated', 'Generated new cryptographic keys and passwords');
  };

  const handleDeploySubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedTemplate) return;

    deployMutation.mutate({
      id: selectedTemplate.id,
      req: {
        name: appName.trim(),
        domain: domain.trim() || undefined,
        variables: envVars,
        auto_generate_missing: true,
      },
    });
  };

  const filteredTemplates = useMemo(() => {
    if (!templates) return [];
    return templates.filter((t) => {
      if (selectedCategory !== 'All') {
        const catLower = selectedCategory.toLowerCase();
        const tplCatLower = t.category.toLowerCase();
        if (!tplCatLower.includes(catLower)) {
          return false;
        }
      }
      if (searchQuery.trim()) {
        const q = searchQuery.toLowerCase().trim();
        const matchesName = t.name.toLowerCase().includes(q);
        const matchesDesc = t.description.toLowerCase().includes(q);
        const matchesTags = t.tags?.some((tag) => tag.toLowerCase().includes(q));
        const matchesCat = t.category.toLowerCase().includes(q);
        return matchesName || matchesDesc || matchesTags || matchesCat;
      }
      return true;
    });
  }, [templates, selectedCategory, searchQuery]);

  return (
    <div className="space-y-6">
      {/* Header Banner */}
      <div className="relative overflow-hidden rounded-2xl bg-gradient-to-r from-cyan-950/40 via-zinc-900/60 to-zinc-950 border border-cyan-800/30 p-6 sm:p-8">
        <div className="relative z-10 max-w-2xl">
          <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full bg-cyan-500/10 border border-cyan-500/30 text-cyan-400 text-xs font-semibold uppercase tracking-wider mb-3">
            <Sparkles className="h-3.5 w-3.5" />
            1-Click App Marketplace
          </div>
          <h1 className="text-2xl sm:text-3xl font-bold tracking-tight text-zinc-100">
            Curated Open-Source Stack Catalog
          </h1>
          <p className="text-sm text-zinc-300 mt-2">
            Instantly deploy pre-configured databases, devtools, CMS platforms, and analytics with
            automatic secrets provisioning, volume persistence, and TLS routing.
          </p>
        </div>
      </div>

      {/* Filter and Search Bar */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        {/* Category Pills */}
        <div className="flex items-center gap-1.5 overflow-x-auto pb-1 sm:pb-0 scrollbar-none">
          {CATEGORIES.map((cat) => (
            <button
              key={cat}
              onClick={() => setSelectedCategory(cat)}
              className={`px-3 py-1.5 rounded-lg text-xs font-medium transition-all whitespace-nowrap ${
                selectedCategory === cat
                  ? 'bg-cyan-500 text-zinc-950 font-semibold shadow-md shadow-cyan-500/20'
                  : 'bg-zinc-900/80 text-zinc-400 hover:text-zinc-100 hover:bg-zinc-800 border border-zinc-800'
              }`}
            >
              {cat}
            </button>
          ))}
        </div>

        {/* Search Bar */}
        <div className="relative w-full sm:w-72">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-zinc-500" />
          <input
            type="text"
            placeholder="Search apps, tags, stacks..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="w-full pl-9 pr-3.5 py-1.5 rounded-lg bg-zinc-900/90 border border-zinc-800 text-xs text-zinc-200 placeholder-zinc-500 focus:outline-none focus:border-cyan-500 transition-colors"
          />
        </div>
      </div>

      {/* Grid of Templates */}
      {isLoading ? (
        <div className="text-center py-16 text-zinc-500 text-xs font-mono">
          <RefreshCw className="h-5 w-5 animate-spin mx-auto mb-2 text-cyan-400" />
          Loading curated marketplace templates...
        </div>
      ) : filteredTemplates.length === 0 ? (
        <Card className="text-center py-12">
          <Store className="h-8 w-8 text-zinc-600 mx-auto mb-2" />
          <h3 className="text-sm font-semibold text-zinc-200">No Templates Found</h3>
          <p className="text-xs text-zinc-500 mt-1 max-w-sm mx-auto">
            Try adjusting your search query or selecting a different category pill.
          </p>
        </Card>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {filteredTemplates.map((tpl) => (
            <Card
              key={tpl.id}
              className="flex flex-col justify-between hover:border-zinc-700/80 transition-all group bg-zinc-900/40 hover:bg-zinc-900/70"
            >
              <div className="space-y-3">
                <div className="flex items-start justify-between gap-3">
                  <div className="flex items-center gap-3">
                    <div className="h-10 w-10 rounded-xl bg-gradient-to-tr from-cyan-950 to-zinc-800 border border-cyan-500/20 flex items-center justify-center text-cyan-300 font-bold text-sm shadow-inner">
                      {tpl.name.slice(0, 2).toUpperCase()}
                    </div>
                    <div>
                      <h3 className="text-sm font-bold text-zinc-100 group-hover:text-cyan-300 transition-colors">
                        {tpl.name}
                      </h3>
                      <span className="text-[11px] text-zinc-400 font-medium">
                        {tpl.category}
                      </span>
                    </div>
                  </div>
                  <Badge variant="outline" className="text-[10px] font-mono border-zinc-700">
                    v{tpl.version}
                  </Badge>
                </div>

                <p className="text-xs text-zinc-300 line-clamp-2 leading-relaxed">
                  {tpl.description}
                </p>

                {/* Tags */}
                {tpl.tags && tpl.tags.length > 0 && (
                  <div className="flex flex-wrap gap-1 pt-1">
                    {tpl.tags.slice(0, 4).map((tag) => (
                      <span
                        key={tag}
                        className="px-2 py-0.5 rounded text-[10px] bg-zinc-800/80 text-zinc-400 border border-zinc-800"
                      >
                        #{tag}
                      </span>
                    ))}
                  </div>
                )}
              </div>

              {/* Footer specs & deploy trigger */}
              <div className="pt-4 mt-4 border-t border-zinc-800/60 flex items-center justify-between">
                <div className="flex items-center gap-3 text-[11px] text-zinc-500 font-mono">
                  <span title="Container Services">
                    {tpl.services?.length || 1} svc
                  </span>
                  <span>•</span>
                  <span title="Default HTTP Port">:{tpl.default_port}</span>
                  {tpl.documentation_url && (
                    <>
                      <span>•</span>
                      <a
                        href={tpl.documentation_url}
                        target="_blank"
                        rel="noreferrer"
                        className="text-zinc-500 hover:text-cyan-400 transition-colors"
                        title="Documentation"
                      >
                        <ExternalLink className="h-3 w-3 inline" />
                      </a>
                    </>
                  )}
                </div>

                <Button
                  variant="primary"
                  size="sm"
                  onClick={() => handleOpenDeploy(tpl)}
                  leftIcon={<Zap className="h-3.5 w-3.5 fill-current" />}
                  className="shadow-sm shadow-cyan-500/20 text-xs"
                >
                  Deploy
                </Button>
              </div>
            </Card>
          ))}
        </div>
      )}

      {/* 1-Click Deploy Wizard Modal */}
      {selectedTemplate && (
        <Modal
          isOpen={Boolean(selectedTemplate)}
          onClose={() => {
            setSelectedTemplate(null);
            setDeployResult(null);
          }}
          title={deployResult ? 'Deployment Result' : `Deploy ${selectedTemplate.name}`}
          description={
            deployResult
              ? 'Your application has been deployed to the Pikpik cluster'
              : 'Configure parameters and auto-generate secure variables for 1-Click deployment'
          }
          size="lg"
        >
          {deployResult ? (
            <div className="space-y-4">
              <div className="p-4 rounded-xl bg-emerald-950/30 border border-emerald-500/30 flex items-start gap-3">
                <CheckCircle2 className="h-5 w-5 text-emerald-400 shrink-0 mt-0.5" />
                <div className="space-y-1 text-xs">
                  <div className="font-semibold text-emerald-200">
                    Application Deployed Successfully!
                  </div>
                  <p className="text-emerald-300/80">
                    {deployResult.message || `${deployResult.name} is online and operational.`}
                  </p>
                </div>
              </div>

              <div className="grid grid-cols-2 gap-3 text-xs">
                <div className="p-3 bg-zinc-900 rounded-lg border border-zinc-800">
                  <div className="text-zinc-500 text-[11px]">App ID</div>
                  <div className="font-mono text-zinc-200 mt-0.5">{deployResult.app_id}</div>
                </div>
                <div className="p-3 bg-zinc-900 rounded-lg border border-zinc-800">
                  <div className="text-zinc-500 text-[11px]">Status</div>
                  <div className="font-mono text-cyan-400 capitalize mt-0.5">
                    {deployResult.status}
                  </div>
                </div>
              </div>

              {deployResult.endpoints && deployResult.endpoints.length > 0 && (
                <div className="space-y-1.5">
                  <label className="text-xs font-semibold text-zinc-300">Access Endpoints</label>
                  <div className="space-y-1">
                    {deployResult.endpoints.map((ep, idx) => (
                      <div
                        key={idx}
                        className="p-2.5 bg-zinc-900 rounded-lg border border-zinc-800 flex items-center justify-between text-xs font-mono text-cyan-300"
                      >
                        <span>{ep}</span>
                        <a
                          href={ep.startsWith('http') ? ep : `http://${ep}`}
                          target="_blank"
                          rel="noreferrer"
                          className="text-zinc-400 hover:text-zinc-100"
                        >
                          <ExternalLink className="h-3.5 w-3.5" />
                        </a>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              <div className="flex justify-end pt-4 border-t border-zinc-800">
                <Button
                  variant="primary"
                  size="sm"
                  onClick={() => {
                    setSelectedTemplate(null);
                    setDeployResult(null);
                  }}
                >
                  Done
                </Button>
              </div>
            </div>
          ) : (
            <form onSubmit={handleDeploySubmit} className="space-y-4">
              {/* Stack Overview */}
              <div className="flex items-center gap-3 p-3 bg-zinc-900/90 rounded-xl border border-zinc-800">
                <div className="h-10 w-10 rounded-lg bg-cyan-950/60 border border-cyan-500/30 flex items-center justify-center text-cyan-300 font-bold">
                  {selectedTemplate.name.slice(0, 2).toUpperCase()}
                </div>
                <div className="flex-1">
                  <div className="text-xs font-bold text-zinc-100">{selectedTemplate.name}</div>
                  <div className="text-[11px] text-zinc-400">
                    Stack: {selectedTemplate.services?.map((s) => s.name).join(', ') || '1 service'}
                  </div>
                </div>
                <Badge variant="info">Port {selectedTemplate.default_port}</Badge>
              </div>

              {/* General Config */}
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <Input
                  label="Application Name"
                  placeholder="my-app"
                  value={appName}
                  onChange={(e) => setAppName(e.target.value)}
                  required
                />
                <Input
                  label="Custom Domain / Routing (Optional)"
                  placeholder="app.example.com"
                  value={domain}
                  onChange={(e) => setDomain(e.target.value)}
                />
              </div>

              {/* Environment Variables & Secrets Schema */}
              {selectedTemplate.env_vars && selectedTemplate.env_vars.length > 0 && (
                <div className="space-y-3 pt-2">
                  <div className="flex items-center justify-between">
                    <label className="text-xs font-semibold text-zinc-300 flex items-center gap-1.5">
                      <Sliders className="h-3.5 w-3.5 text-cyan-400" />
                      Configuration & Generated Passwords
                    </label>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={handleGenerateAll}
                      leftIcon={<RefreshCw className="h-3 w-3" />}
                      className="text-[11px] text-cyan-400 hover:text-cyan-300 h-7"
                    >
                      Regenerate All
                    </Button>
                  </div>

                  <div className="space-y-2.5 max-h-60 overflow-y-auto pr-1">
                    {selectedTemplate.env_vars.map((ev) => {
                      const isSecret = ev.is_secret;
                      const isShowing = showSecrets[ev.key];
                      return (
                        <div
                          key={ev.key}
                          className="p-3 bg-zinc-900/70 border border-zinc-800 rounded-lg space-y-1.5"
                        >
                          <div className="flex items-center justify-between">
                            <span className="text-xs font-mono font-medium text-zinc-200">
                              {ev.label || ev.key}
                            </span>
                            <div className="flex items-center gap-2">
                              {ev.required && (
                                <span className="text-[10px] text-rose-400 font-mono">
                                  Required
                                </span>
                              )}
                              {ev.auto_generate && (
                                <Badge variant="outline" className="text-[9px] font-mono text-cyan-400">
                                  {ev.auto_generate}
                                </Badge>
                              )}
                            </div>
                          </div>

                          {ev.description && (
                            <p className="text-[11px] text-zinc-400">{ev.description}</p>
                          )}

                          <div className="flex items-center gap-2">
                            <div className="relative flex-1">
                              <input
                                type={isSecret && !isShowing ? 'password' : 'text'}
                                value={envVars[ev.key] || ''}
                                onChange={(e) =>
                                  setEnvVars((prev) => ({ ...prev, [ev.key]: e.target.value }))
                                }
                                placeholder={ev.default || `Enter ${ev.key}...`}
                                className="w-full rounded-md border border-zinc-700 bg-zinc-950 px-3 py-1.5 text-xs text-zinc-100 font-mono focus:border-cyan-500 focus:outline-none"
                                required={ev.required}
                              />
                            </div>

                            {isSecret && (
                              <Button
                                type="button"
                                variant="ghost"
                                size="sm"
                                onClick={() =>
                                  setShowSecrets((prev) => ({ ...prev, [ev.key]: !prev[ev.key] }))
                                }
                                className="p-1.5 h-8 w-8 text-zinc-400"
                                title={isShowing ? 'Hide' : 'Show'}
                              >
                                {isShowing ? (
                                  <EyeOff className="h-3.5 w-3.5" />
                                ) : (
                                  <Eye className="h-3.5 w-3.5" />
                                )}
                              </Button>
                            )}

                            {(isSecret || ev.auto_generate) && (
                              <Button
                                type="button"
                                variant="outline"
                                size="sm"
                                onClick={() => {
                                  const newVal = generateSecureToken(ev.auto_generate || 'pass_16');
                                  setEnvVars((prev) => ({ ...prev, [ev.key]: newVal }));
                                }}
                                className="h-8 text-[11px]"
                                title="Generate Random"
                              >
                                <RefreshCw className="h-3 w-3" />
                              </Button>
                            )}
                          </div>
                        </div>
                      );
                    })}
                  </div>
                </div>
              )}

              {/* Actions */}
              <div className="flex justify-end gap-2 pt-4 border-t border-zinc-800">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => setSelectedTemplate(null)}
                >
                  Cancel
                </Button>
                <Button
                  type="submit"
                  variant="primary"
                  size="sm"
                  isLoading={deployMutation.isPending}
                  leftIcon={<Zap className="h-3.5 w-3.5 fill-current" />}
                >
                  Confirm & Deploy 1-Click
                </Button>
              </div>
            </form>
          )}
        </Modal>
      )}
    </div>
  );
}
