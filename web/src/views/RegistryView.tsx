import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../lib/api';
import { Card, CardHeader, CardTitle, CardDescription } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Badge } from '../components/ui/Badge';
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '../components/ui/Table';
import { useToast } from '../components/ui/Toast';
import { QueryErrorAlert } from '../components/ui/QueryErrorAlert';
import { formatBytes, formatDate } from '../lib/utils';
import {
  HardDrive,
  RotateCw,
  Trash2,
  ShieldCheck,
  Tag,
} from 'lucide-react';

export function RegistryView() {
  const queryClient = useQueryClient();
  const toast = useToast();

  const {
    data: status,
    isLoading: isStatusLoading,
    isError: isStatusError,
    error: statusError,
    refetch: refetchStatus,
  } = useQuery({
    queryKey: ['registryStatus'],
    queryFn: api.registry.getStatus,
  });

  const {
    data: catalog,
    isError: isCatalogError,
    error: catalogError,
    refetch: refetchCatalog,
  } = useQuery({
    queryKey: ['registryCatalog'],
    queryFn: api.registry.listRepositories,
  });

  const {
    data: credentials,
    isError: isCredentialsError,
    error: credentialsError,
    refetch: refetchCredentials,
  } = useQuery({
    queryKey: ['registryCredentials'],
    queryFn: () => api.registry.getCredentials(),
  });

  const gcMutation = useMutation({
    mutationFn: api.registry.garbageCollect,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['registryStatus'] });
      toast.success('Garbage Collection Finished', 'Unreferenced blobs and manifests pruned');
    },
    onError: (err: Error) => toast.error('GC Failed', err.message),
  });

  const rotateMutation = useMutation({
    mutationFn: (id: string) => api.registry.rotateCredentials(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['registryCredentials'] });
      toast.success('Robot Token Rotated', 'New secret credential issued');
    },
    onError: (err: Error) => toast.error('Rotate Failed', err.message),
  });

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-zinc-100 flex items-center gap-2">
            <HardDrive className="h-5 w-5 text-cyan-400" />
            <span>Embedded Container Registry</span>
          </h1>
          <p className="text-xs text-zinc-400 mt-0.5">
            Private OCI v2 distribution registry with htpasswd token authentication
          </p>
        </div>

        <Button
          variant="outline"
          size="sm"
          onClick={() => gcMutation.mutate()}
          isLoading={gcMutation.isPending}
          leftIcon={<Trash2 className="h-3.5 w-3.5 text-zinc-400" />}
        >
          Run Garbage Collection
        </Button>
      </div>

      {(isStatusError || isCatalogError || isCredentialsError) && (
        <QueryErrorAlert
          title="Registry Service Error"
          error={statusError || catalogError || credentialsError}
          onRetry={() => {
            refetchStatus();
            refetchCatalog();
            refetchCredentials();
          }}
        />
      )}

      {/* Registry Status Cards */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
        <Card>
          <div className="flex items-center justify-between">
            <span className="text-xs text-zinc-400">Daemon Status</span>
            <Badge variant={status?.is_running ? 'success' : 'warning'} dot>
              {status?.is_running ? 'Online' : 'Stopped'}
            </Badge>
          </div>
          <div className="mt-3 text-lg font-bold font-mono text-zinc-100">
            {status?.is_running ? 'OCI v2.8 Active' : 'Offline'}
          </div>
          <div className="text-[11px] text-zinc-500 font-mono mt-1 truncate">
            {status?.container_id ? `Container: ${status.container_id.slice(0, 12)}` : 'Managed daemon'}
          </div>
        </Card>

        <Card>
          <div className="flex items-center justify-between">
            <span className="text-xs text-zinc-400">Registry Storage</span>
            <HardDrive className="h-4 w-4 text-cyan-400" />
          </div>
          <div className="mt-3 text-lg font-bold font-mono text-cyan-300">
            {status ? formatBytes(status.storage_bytes) : '0 B'}
          </div>
          <div className="text-[11px] text-zinc-500 mt-1">Total disk footprint</div>
        </Card>

        <Card>
          <div className="flex items-center justify-between">
            <span className="text-xs text-zinc-400">Total Repositories</span>
            <Tag className="h-4 w-4 text-purple-400" />
          </div>
          <div className="mt-3 text-lg font-bold font-mono text-purple-300">
            {catalog?.repositories?.length ?? status?.repositories_count ?? 0}
          </div>
          <div className="text-[11px] text-zinc-500 mt-1">OCI image namespaces</div>
        </Card>
      </div>

      {/* Repositories Catalog & Robot Credentials Grid */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* Catalog Card */}
        <Card className="p-0 overflow-hidden">
          <CardHeader className="p-4 border-b border-zinc-800">
            <CardTitle className="text-sm">Image Catalog</CardTitle>
            <CardDescription>Available container images hosted on localhost:5000</CardDescription>
          </CardHeader>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Repository</TableHead>
                <TableHead>Tags</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isStatusLoading ? (
                <TableRow>
                  <TableCell colSpan={2} className="text-center py-6 text-zinc-500">
                    Loading catalog...
                  </TableCell>
                </TableRow>
              ) : (!catalog?.repositories || catalog.repositories.length === 0) ? (
                <TableRow>
                  <TableCell colSpan={2} className="text-center py-6 text-zinc-500">
                    No images pushed yet.
                  </TableCell>
                </TableRow>
              ) : (
                catalog.repositories.map((repo) => {
                  const tags = catalog.tags?.[repo] || ['latest'];
                  return (
                    <TableRow key={repo}>
                      <TableCell className="font-mono text-xs font-semibold text-zinc-200">
                        {repo}
                      </TableCell>
                      <TableCell>
                        <div className="flex flex-wrap gap-1">
                          {tags.map((t) => (
                            <span
                              key={t}
                              className="px-1.5 py-0.5 rounded bg-zinc-800 border border-zinc-700 text-[11px] font-mono text-cyan-300"
                            >
                              {t}
                            </span>
                          ))}
                        </div>
                      </TableCell>
                    </TableRow>
                  );
                })
              )}
            </TableBody>
          </Table>
        </Card>

        {/* Robot Credentials Card */}
        <Card className="p-0 overflow-hidden">
          <CardHeader className="p-4 border-b border-zinc-800">
            <CardTitle className="text-sm">Robot Push/Pull Credentials</CardTitle>
            <CardDescription>Automated CI/CD machine user authentication</CardDescription>
          </CardHeader>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Robot Account</TableHead>
                <TableHead>Project</TableHead>
                <TableHead>Created</TableHead>
                <TableHead className="text-right">Rotate</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(!credentials || credentials.length === 0) ? (
                <TableRow>
                  <TableCell colSpan={4} className="text-center py-6 text-zinc-500">
                    Default cluster robot credential active.
                  </TableCell>
                </TableRow>
              ) : (
                credentials.map((cred) => (
                  <TableRow key={cred.id}>
                    <TableCell className="font-semibold text-xs text-zinc-200">
                      <div className="flex items-center gap-2">
                        <ShieldCheck className="h-4 w-4 text-emerald-400 shrink-0" />
                        <span className="font-mono">{cred.username}</span>
                      </div>
                    </TableCell>
                    <TableCell className="font-mono text-xs text-zinc-400">
                      {cred.project_id || 'global'}
                    </TableCell>
                    <TableCell className="text-xs text-zinc-500">
                      {formatDate(cred.created_at)}
                    </TableCell>
                    <TableCell className="text-right">
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => rotateMutation.mutate(cred.id)}
                        isLoading={rotateMutation.isPending && rotateMutation.variables === cred.id}
                        title="Rotate Secret Token"
                      >
                        <RotateCw className="h-3.5 w-3.5" />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </Card>
      </div>
    </div>
  );
}
