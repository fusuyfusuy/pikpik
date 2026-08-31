import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../../../../lib/api';
import { Card } from '../../../../components/ui/Card';
import { Button } from '../../../../components/ui/Button';
import { Badge } from '../../../../components/ui/Badge';
import { Input } from '../../../../components/ui/Input';
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '../../../../components/ui/Table';
import { useToast } from '../../../../components/ui/Toast';
import { QueryErrorAlert } from '../../../../components/ui/QueryErrorAlert';
import { RouteGraph } from './RouteGraph';
import { NewRouteModal } from './NewRouteModal';
import {
  Globe,
  Plus,
  RotateCw,
  Search,
  Trash2,
  ExternalLink,
  ShieldCheck,
  ShieldAlert,
  Server,
} from 'lucide-react';

export function DynamicRoutesTab() {
  const queryClient = useQueryClient();
  const toast = useToast();

  const [searchQuery, setSearchQuery] = useState('');
  const [isNewRouteModalOpen, setIsNewRouteModalOpen] = useState(false);

  // Queries
  const {
    data: domains = [],
    isLoading: isLoadingDomains,
    isError: isDomainsError,
    error: domainsError,
    refetch: refetchDomains,
  } = useQuery({
    queryKey: ['domains'],
    queryFn: api.ingress.listDomains,
  });

  const { data: apps = [] } = useQuery({
    queryKey: ['apps'],
    queryFn: () => api.apps.list(),
  });

  // Reconcile Mutation
  const reconcileMutation = useMutation({
    mutationFn: api.ingress.reconcile,
    onSuccess: () => {
      toast.success('Ingress Reconciled', 'Caddy routes and TLS state successfully resynchronized');
      queryClient.invalidateQueries({ queryKey: ['domains'] });
      queryClient.invalidateQueries({ queryKey: ['caddy-diagnostics'] });
    },
    onError: (err: any) => {
      toast.error('Reconciliation Failed', err?.message || 'Could not sync Caddy');
    },
  });

  // Delete Domain Mutation
  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.ingress.deleteDomain(id),
    onSuccess: () => {
      toast.success('Route Removed', 'Domain binding and Caddy route deleted');
      queryClient.invalidateQueries({ queryKey: ['domains'] });
      queryClient.invalidateQueries({ queryKey: ['caddy-diagnostics'] });
    },
    onError: (err: any) => {
      toast.error('Deletion Failed', err?.message || 'Could not delete route');
    },
  });

  // Safe arrays
  const safeDomains = domains || [];
  const safeApps = apps || [];

  // Filtered domains
  const filteredDomains = safeDomains.filter((d) => {
    const query = searchQuery.toLowerCase();
    const app = safeApps.find((a) => a.id === d.app_id);
    return (
      d.domain.toLowerCase().includes(query) ||
      d.app_id.toLowerCase().includes(query) ||
      (app && app.name.toLowerCase().includes(query))
    );
  });

  const getAppName = (appId: string) => {
    const app = safeApps.find((a) => a.id === appId);
    return app ? app.name : appId || '—';
  };

  return (
    <div className="space-y-6">
      {/* Top Action Toolbar */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div className="flex items-center gap-2.5">
          <Button
            variant="primary"
            onClick={() => setIsNewRouteModalOpen(true)}
            className="flex items-center gap-2 shadow-sm"
          >
            <Plus className="w-4 h-4" />
            <span>New Route</span>
          </Button>
          <Button
            variant="outline"
            onClick={() => reconcileMutation.mutate()}
            disabled={reconcileMutation.isPending}
            className="flex items-center gap-2"
          >
            <RotateCw className={`w-4 h-4 ${reconcileMutation.isPending ? 'animate-spin text-cyan-400' : ''}`} />
            <span>Reconcile Caddy</span>
          </Button>
        </div>

        {/* Search Bar */}
        <div className="w-full sm:w-72 relative">
          <Search className="w-4 h-4 absolute left-3 top-1/2 -translate-y-1/2 text-zinc-500 pointer-events-none" />
          <Input
            placeholder="Search domains or upstreams..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="pl-9 bg-zinc-900/80 border-zinc-800 text-xs"
          />
        </div>
      </div>

      {/* Visual Route-to-Upstream Graph */}
      <Card className="p-5 border-zinc-800/80 bg-zinc-950/40">
        <RouteGraph
          domains={domains}
          apps={apps}
        />
      </Card>

      {/* Routes Table */}
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Globe className="w-4 h-4 text-cyan-400" />
            <h3 className="text-sm font-semibold text-zinc-200">
              Active Ingress Routes ({filteredDomains.length})
            </h3>
          </div>
          <span className="text-xs text-zinc-500 font-mono">
            Direct Docker Socket &amp; Dynamic Ingress
          </span>
        </div>

        <Card className="border-zinc-800/80 overflow-hidden bg-zinc-950/40">
          <Table>
            <TableHeader>
              <TableRow className="border-zinc-800 hover:bg-transparent">
                <TableHead className="text-zinc-400 text-xs">Domain</TableHead>
                <TableHead className="text-zinc-400 text-xs">Path Prefix</TableHead>
                <TableHead className="text-zinc-400 text-xs">Target Service Upstream</TableHead>
                <TableHead className="text-zinc-400 text-xs">Traffic Weight</TableHead>
                <TableHead className="text-zinc-400 text-xs">TLS Status</TableHead>
                <TableHead className="text-zinc-400 text-xs text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isDomainsError ? (
                <TableRow>
                  <TableCell colSpan={6} className="p-4">
                    <QueryErrorAlert
                      title="Failed to load ingress routes"
                      error={domainsError}
                      onRetry={refetchDomains}
                    />
                  </TableCell>
                </TableRow>
              ) : isLoadingDomains ? (
                <TableRow>
                  <TableCell colSpan={6} className="text-center py-8 text-zinc-500 text-xs">
                    <RotateCw className="w-5 h-5 animate-spin mx-auto mb-2 text-cyan-400 opacity-60" />
                    Loading dynamic routes...
                  </TableCell>
                </TableRow>
              ) : filteredDomains.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className="text-center py-8 text-zinc-500 text-xs">
                    No ingress routes found matching your criteria.
                  </TableCell>
                </TableRow>
              ) : (
                filteredDomains.map((binding) => {
                  const appName = getAppName(binding.app_id);
                  const isHttps = binding.auto_tls;
                  const href = `${isHttps ? 'https' : 'http'}://${binding.domain}`;

                  return (
                    <TableRow key={binding.id} className="border-zinc-800/60 hover:bg-zinc-900/40">
                      {/* Domain */}
                      <TableCell className="font-mono text-sm">
                        <div className="flex items-center gap-2">
                          <a
                            href={href}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="font-medium text-zinc-100 hover:text-cyan-400 transition-colors flex items-center gap-1.5"
                          >
                            <span>{binding.domain}</span>
                            <ExternalLink className="w-3 h-3 opacity-60" />
                          </a>
                        </div>
                      </TableCell>

                      {/* Path */}
                      <TableCell>
                        <span className="px-2 py-0.5 rounded bg-zinc-800/80 border border-zinc-700/50 text-[11px] font-mono text-zinc-300">
                          /*
                        </span>
                      </TableCell>

                      {/* Target Service Upstream */}
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <Server className="w-3.5 h-3.5 text-zinc-400" />
                          <span className="text-xs font-medium text-zinc-200">
                            {appName}
                          </span>
                          <span className="text-[11px] text-zinc-500 font-mono">
                            ({binding.app_id})
                          </span>
                        </div>
                      </TableCell>

                      {/* Traffic Weight */}
                      <TableCell>
                        <Badge variant="default" className="bg-cyan-950/60 text-cyan-300 border-cyan-800/50 text-[11px] font-mono">
                          100% Primary
                        </Badge>
                      </TableCell>

                      {/* TLS Status */}
                      <TableCell>
                        {binding.auto_tls ? (
                          <div className="flex items-center gap-1.5 text-xs text-emerald-400">
                            <ShieldCheck className="w-3.5 h-3.5" />
                            <span>Auto TLS Active</span>
                          </div>
                        ) : (
                          <div className="flex items-center gap-1.5 text-xs text-zinc-500">
                            <ShieldAlert className="w-3.5 h-3.5" />
                            <span>HTTP Only</span>
                          </div>
                        )}
                      </TableCell>

                      {/* Actions */}
                      <TableCell className="text-right">
                        <div className="flex items-center justify-end gap-1.5">
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => {
                              if (confirm(`Remove dynamic route for domain ${binding.domain}?`)) {
                                deleteMutation.mutate(binding.id);
                              }
                            }}
                            className="text-red-400 hover:text-red-300 hover:bg-red-950/30 p-1.5 h-7 w-7"
                            title="Delete Route"
                          >
                            <Trash2 className="w-3.5 h-3.5" />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  );
                })
              )}
            </TableBody>
          </Table>
        </Card>
      </div>

      {/* New Route Dialog Modal */}
      <NewRouteModal
        isOpen={isNewRouteModalOpen}
        onClose={() => setIsNewRouteModalOpen(false)}
        apps={safeApps}
      />
    </div>
  );
}
