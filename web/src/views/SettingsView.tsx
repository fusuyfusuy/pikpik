import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../lib/api';
import { User, CreateTokenRequest, APIToken } from '../lib/types';
import { Card, CardHeader, CardTitle, CardContent } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Badge } from '../components/ui/Badge';
import { Input } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '../components/ui/Table';
import { useToast } from '../components/ui/Toast';
import { QueryErrorAlert } from '../components/ui/QueryErrorAlert';
import { formatDate } from '../lib/utils';
import {
  Key,
  Plus,
  Trash2,
  Copy,
  Check,
  Shield,
  User as UserIcon,
} from 'lucide-react';

const AVAILABLE_SCOPES = [
  { id: 'admin', label: 'Full Admin Access (Cluster Management)' },
  { id: 'read:apps', label: 'Read Applications & Telemetry' },
  { id: 'write:apps', label: 'Deploy & Manage Applications' },
  { id: 'ingress', label: 'Manage Custom Domains & TLS Certificates' },
  { id: 'backups', label: 'Trigger Snapshots & Restores' },
];

export interface SettingsViewProps {
  user: User | null;
}

export function SettingsView({ user }: SettingsViewProps) {
  const queryClient = useQueryClient();
  const toast = useToast();

  const [isCreateModalOpen, setIsCreateModalOpen] = useState(false);
  const [createdToken, setCreatedToken] = useState<APIToken | null>(null);
  const [tokenName, setTokenName] = useState('');
  const [selectedScopes, setSelectedScopes] = useState<string[]>(['read:apps', 'write:apps']);
  const [copiedToken, setCopiedToken] = useState(false);

  const {
    data: tokens,
    isLoading,
    isError: isTokensError,
    error: tokensError,
    refetch: refetchTokens,
  } = useQuery({
    queryKey: ['apiTokens'],
    queryFn: api.auth.listTokens,
  });

  const createMutation = useMutation({
    mutationFn: (req: CreateTokenRequest) => api.auth.createToken(req),
    onSuccess: (newToken) => {
      queryClient.invalidateQueries({ queryKey: ['apiTokens'] });
      setCreatedToken(newToken);
      toast.success('Token Generated', 'New API access token issued');
      setTokenName('');
    },
    onError: (err: Error) => toast.error('Creation Failed', err.message),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.auth.deleteToken(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['apiTokens'] });
      toast.success('Token Revoked', 'API token successfully invalidated');
    },
    onError: (err: Error) => toast.error('Revocation Failed', err.message),
  });

  const toggleScope = (scopeId: string) => {
    if (selectedScopes.includes(scopeId)) {
      setSelectedScopes(selectedScopes.filter((s) => s !== scopeId));
    } else {
      setSelectedScopes([...selectedScopes, scopeId]);
    }
  };

  const copyTokenToClipboard = (tokenStr: string) => {
    navigator.clipboard.writeText(tokenStr);
    setCopiedToken(true);
    toast.success('Copied Token', 'Secret token copied to clipboard');
    setTimeout(() => setCopiedToken(false), 2000);
  };

  return (
    <div className="space-y-6">
      {/* Header */}
      <div>
        <h1 className="text-xl font-bold tracking-tight text-zinc-100 flex items-center gap-2">
          <Key className="h-5 w-5 text-cyan-400" />
          <span>Settings & API Access</span>
        </h1>
        <p className="text-xs text-zinc-400 mt-0.5">
          Programmatic CLI tokens, RBAC roles, and control plane authentication
        </p>
      </div>

      {/* User Profile Card */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm flex items-center gap-2">
            <UserIcon className="h-4 w-4 text-cyan-400" />
            <span>Active Operator Profile</span>
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 text-xs">
            <div className="p-3 bg-zinc-950 rounded-lg border border-zinc-800">
              <span className="text-zinc-500 block">Email Address</span>
              <span className="text-sm font-semibold text-zinc-200 mt-1 block">
                {user?.email || 'admin@pikpik.local'}
              </span>
            </div>

            <div className="p-3 bg-zinc-950 rounded-lg border border-zinc-800">
              <span className="text-zinc-500 block">RBAC Role</span>
              <div className="mt-1">
                <Badge variant="info">{user?.role || 'owner'}</Badge>
              </div>
            </div>

            <div className="p-3 bg-zinc-950 rounded-lg border border-zinc-800">
              <span className="text-zinc-500 block">User ID</span>
              <span className="text-xs font-mono text-zinc-400 mt-1 block truncate">
                {user?.id || 'usr_root'}
              </span>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* API Tokens Section */}
      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <div>
            <h2 className="text-base font-semibold text-zinc-100">API Tokens (CLI & CI/CD)</h2>
            <p className="text-xs text-zinc-400">
              Scoped bearer tokens for the `pikpik-cli` and automated pipelines
            </p>
          </div>

          <Button
            variant="primary"
            size="sm"
            onClick={() => {
              setCreatedToken(null);
              setIsCreateModalOpen(true);
            }}
            leftIcon={<Plus className="h-4 w-4" />}
          >
            Generate Token
          </Button>
        </div>

        <Card className="p-0 overflow-hidden">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Token Name</TableHead>
                <TableHead>Prefix / ID</TableHead>
                <TableHead>Granted Scopes</TableHead>
                <TableHead>Created</TableHead>
                <TableHead className="text-right">Revoke</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isTokensError ? (
                <TableRow>
                  <TableCell colSpan={5} className="p-4">
                    <QueryErrorAlert
                      title="Failed to load API tokens"
                      error={tokensError}
                      onRetry={refetchTokens}
                    />
                  </TableCell>
                </TableRow>
              ) : isLoading ? (
                <TableRow>
                  <TableCell colSpan={5} className="text-center py-8 text-zinc-500">
                    Loading tokens...
                  </TableCell>
                </TableRow>
              ) : (!tokens || tokens.length === 0) ? (
                <TableRow>
                  <TableCell colSpan={5} className="text-center py-8 text-zinc-500">
                    No active API tokens found.
                  </TableCell>
                </TableRow>
              ) : (
                tokens.map((tok) => (
                  <TableRow key={tok.id}>
                    <TableCell className="font-semibold text-zinc-200">
                      <div className="flex items-center gap-2">
                        <Key className="h-4 w-4 text-cyan-400 shrink-0" />
                        <span>{tok.name}</span>
                      </div>
                    </TableCell>
                    <TableCell className="font-mono text-xs text-zinc-400">
                      {tok.prefix || 'pik_live_...'}
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-1">
                        {tok.scopes?.map((sc) => (
                          <span
                            key={sc}
                            className="px-1.5 py-0.5 rounded bg-zinc-800 border border-zinc-700 text-[11px] font-mono text-zinc-300"
                          >
                            {sc}
                          </span>
                        ))}
                      </div>
                    </TableCell>
                    <TableCell className="text-xs text-zinc-500">
                      {formatDate(tok.created_at)}
                    </TableCell>
                    <TableCell className="text-right">
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => {
                          if (confirm(`Revoke API token ${tok.name}?`)) {
                            deleteMutation.mutate(tok.id);
                          }
                        }}
                      >
                        <Trash2 className="h-3.5 w-3.5 text-rose-400" />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </Card>
      </div>

      {/* Generate Token Modal */}
      <Modal
        isOpen={isCreateModalOpen}
        onClose={() => setIsCreateModalOpen(false)}
        title="Issue Scoped API Token"
        description="Create a credential for CI/CD automation or local CLI access"
      >
        {createdToken ? (
          <div className="space-y-4">
            <div className="p-4 bg-emerald-950/40 border border-emerald-800/50 rounded-xl space-y-2">
              <div className="flex items-center gap-2 text-xs font-semibold text-emerald-400">
                <Shield className="h-4 w-4" />
                <span>Token Created — Copy Immediately</span>
              </div>
              <p className="text-xs text-zinc-400">
                This token will never be shown again. Save it securely in your secret manager.
              </p>
              <div className="flex items-center gap-2 pt-2">
                <input
                  readOnly
                  value={createdToken.raw_secret || createdToken.prefix || 'pik_live_secret_token'}
                  className="flex-1 rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 font-mono text-xs text-cyan-300"
                />
                <Button
                  variant="primary"
                  size="sm"
                  onClick={() =>
                    copyTokenToClipboard(
                      createdToken.raw_secret || createdToken.prefix || 'pik_live_secret_token'
                    )
                  }
                >
                  {copiedToken ? <Check className="h-4 w-4 text-zinc-950" /> : <Copy className="h-4 w-4" />}
                </Button>
              </div>
            </div>

            <div className="flex justify-end pt-2">
              <Button
                variant="secondary"
                size="sm"
                onClick={() => {
                  setIsCreateModalOpen(false);
                  setCreatedToken(null);
                }}
              >
                Done
              </Button>
            </div>
          </div>
        ) : (
          <form
            onSubmit={(e) => {
              e.preventDefault();
              createMutation.mutate({ name: tokenName, scopes: selectedScopes });
            }}
            className="space-y-4"
          >
            <Input
              label="Token Description"
              placeholder="e.g. Github Actions CI Runner"
              value={tokenName}
              onChange={(e) => setTokenName(e.target.value)}
              required
            />

            <div className="space-y-2">
              <label className="block text-xs font-medium text-zinc-300">Permissions / Scopes</label>
              <div className="space-y-2">
                {AVAILABLE_SCOPES.map((sc) => {
                  const isChecked = selectedScopes.includes(sc.id);
                  return (
                    <label
                      key={sc.id}
                      onClick={() => toggleScope(sc.id)}
                      className={`flex items-center gap-3 p-2.5 rounded-lg border cursor-pointer text-xs transition-all ${
                        isChecked
                          ? 'border-cyan-500/50 bg-cyan-950/20 text-zinc-200'
                          : 'border-zinc-800 bg-zinc-950 text-zinc-400 hover:border-zinc-700'
                      }`}
                    >
                      <input
                        type="checkbox"
                        checked={isChecked}
                        onChange={() => {}}
                        className="h-4 w-4 rounded border-zinc-700 bg-zinc-900 text-cyan-500 focus:ring-cyan-500"
                      />
                      <span className="font-mono">{sc.id}</span>
                      <span className="text-zinc-500 text-[11px]">— {sc.label}</span>
                    </label>
                  );
                })}
              </div>
            </div>

            <div className="flex justify-end gap-2 pt-4 border-t border-zinc-800">
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => setIsCreateModalOpen(false)}
              >
                Cancel
              </Button>
              <Button
                type="submit"
                variant="primary"
                size="sm"
                isLoading={createMutation.isPending}
              >
                Generate Token
              </Button>
            </div>
          </form>
        )}
      </Modal>
    </div>
  );
}
