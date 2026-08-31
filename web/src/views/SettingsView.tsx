import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../lib/api';
import { User, CreateTokenRequest, APIToken, TeamUser, TeamInvitationResponse } from '../lib/types';
import { Card, CardHeader, CardTitle, CardContent } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Badge } from '../components/ui/Badge';
import { Input } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { Tabs } from '../components/ui/Tabs';
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
  Users,
  Bell,
  Send,
  UserPlus,
  KeyRound,
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

  const [activeTab, setActiveTab] = useState<'tokens' | 'team' | 'notifications'>('team');

  // Token Generation State
  const [isCreateModalOpen, setIsCreateModalOpen] = useState(false);
  const [createdToken, setCreatedToken] = useState<APIToken | null>(null);
  const [tokenName, setTokenName] = useState('');
  const [selectedScopes, setSelectedScopes] = useState<string[]>(['read:apps', 'write:apps']);
  const [copiedToken, setCopiedToken] = useState(false);

  // Team Invite State
  const [isInviteModalOpen, setIsInviteModalOpen] = useState(false);
  const [inviteEmail, setInviteEmail] = useState('');
  const [inviteRole, setInviteRole] = useState<'owner' | 'admin' | 'developer' | 'viewer'>('developer');
  const [inviteDays, setInviteDays] = useState(7);
  const [createdInvite, setCreatedInvite] = useState<TeamInvitationResponse | null>(null);
  const [copiedInvite, setCopiedInvite] = useState(false);

  // Password Reset State
  const [resetTargetUser, setResetTargetUser] = useState<TeamUser | null>(null);
  const [newPassword, setNewPassword] = useState('');

  // Notification Channels State
  const [isNotifyModalOpen, setIsNotifyModalOpen] = useState(false);
  const [notifyName, setNotifyName] = useState('');
  const [notifyType, setNotifyType] = useState<'webhook' | 'discord' | 'slack' | 'telegram'>('discord');
  const [notifyURL, setNotifyURL] = useState('');
  const [notifyAuthToken, setNotifyAuthToken] = useState('');
  const [notifyEvents, setNotifyEvents] = useState('deploy:failure,deploy:success,backup:failure,backup:success');

  // Queries
  const {
    data: tokens,
    isLoading: isTokensLoading,
    isError: isTokensError,
    error: tokensError,
    refetch: refetchTokens,
  } = useQuery({
    queryKey: ['apiTokens'],
    queryFn: api.auth.listTokens,
  });

  const {
    data: users = [],
    isLoading: isUsersLoading,
    isError: isUsersError,
    error: usersError,
    refetch: refetchUsers,
  } = useQuery({
    queryKey: ['users'],
    queryFn: api.users.list,
  });

  const {
    data: notificationChannels,
    isLoading: isNotifyLoading,
    isError: isNotifyError,
    error: notifyError,
    refetch: refetchNotify,
  } = useQuery({
    queryKey: ['notificationChannels'],
    queryFn: () => api.notifications.list(),
  });

  const safeTokens = tokens || [];
  const safeUsers = users || [];
  const safeChannels = notificationChannels || [];

  // Mutations
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

  const inviteMutation = useMutation({
    mutationFn: (req: { email: string; role: any; expires_in_days: number }) => api.users.invite(req),
    onSuccess: (inv) => {
      queryClient.invalidateQueries({ queryKey: ['users'] });
      setCreatedInvite(inv);
      toast.success('Invitation Created', `Invitation link generated for ${inv.email}`);
      setInviteEmail('');
    },
    onError: (err: Error) => toast.error('Invite Failed', err.message),
  });

  const updateRoleMutation = useMutation({
    mutationFn: ({ id, role }: { id: string; role: any }) => api.users.updateRole(id, { role }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['users'] });
      toast.success('Role Updated', 'User permissions updated successfully');
    },
    onError: (err: Error) => toast.error('Update Failed', err.message),
  });

  const deleteUserMutation = useMutation({
    mutationFn: (id: string) => api.users.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['users'] });
      toast.success('User Removed', 'Team member removed cleanly');
    },
    onError: (err: Error) => toast.error('Removal Failed', err.message),
  });

  const resetPasswordMutation = useMutation({
    mutationFn: ({ id, pwd }: { id: string; pwd: string }) =>
      api.users.resetPassword(id, { new_password: pwd }),
    onSuccess: () => {
      toast.success('Password Reset', 'New password saved and active sessions revoked');
      setResetTargetUser(null);
      setNewPassword('');
    },
    onError: (err: Error) => toast.error('Reset Failed', err.message),
  });

  const createNotifyMutation = useMutation({
    mutationFn: (req: { name: string; type: string; target_url: string; auth_token?: string; events: string[] }) =>
      api.notifications.create(req),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['notificationChannels'] });
      toast.success('Channel Added', 'New alert destination registered');
      setIsNotifyModalOpen(false);
      setNotifyName('');
      setNotifyURL('');
      setNotifyAuthToken('');
    },
    onError: (err: Error) => toast.error('Creation Failed', err.message),
  });

  const deleteNotifyMutation = useMutation({
    mutationFn: (id: string) => api.notifications.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['notificationChannels'] });
      toast.success('Channel Removed', 'Notification destination deleted');
    },
    onError: (err: Error) => toast.error('Deletion Failed', err.message),
  });

  const testNotifyMutation = useMutation({
    mutationFn: (id: string) => api.notifications.test(id),
    onSuccess: (res) => {
      toast.success('Ping Delivered', res.message || 'Test alert sent successfully');
    },
    onError: (err: Error) => toast.error('Delivery Failed', err.message),
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

  const copyInviteToClipboard = (urlStr: string) => {
    navigator.clipboard.writeText(urlStr);
    setCopiedInvite(true);
    toast.success('Copied Link', 'Invitation URL copied to clipboard');
    setTimeout(() => setCopiedInvite(false), 2000);
  };

  return (
    <div className="space-y-6">
      {/* Header */}
      <div>
        <h1 className="text-xl font-bold tracking-tight text-zinc-100 flex items-center gap-2">
          <Shield className="h-5 w-5 text-cyan-400" />
          <span>Platform Settings & Governance</span>
        </h1>
        <p className="text-xs text-zinc-400 mt-0.5">
          Multi-user team management, scoped RBAC permissions, API tokens, and alert channels
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

      {/* Tabs */}
      <Tabs
        variant="pills"
        activeTab={activeTab}
        onChange={(t) => setActiveTab(t as any)}
        tabs={[
          { id: 'team', label: 'Team & RBAC', icon: <Users className="h-4 w-4" />, count: users.length },
          { id: 'tokens', label: 'API Tokens', icon: <Key className="h-4 w-4" />, count: tokens?.length },
          { id: 'notifications', label: 'Alert Channels', icon: <Bell className="h-4 w-4" />, count: notificationChannels?.length },
        ]}
      />

      {/* 1. Team & RBAC Tab */}
      {activeTab === 'team' && (
        <div className="space-y-4">
          <div className="flex items-center justify-between">
            <div>
              <h2 className="text-base font-semibold text-zinc-100">Team Members & Access Control</h2>
              <p className="text-xs text-zinc-400">
                Manage organization tenants, assigned cluster authority, and secure shareable invitations
              </p>
            </div>

            <Button
              variant="primary"
              size="sm"
              onClick={() => {
                setCreatedInvite(null);
                setIsInviteModalOpen(true);
              }}
              leftIcon={<UserPlus className="h-4 w-4" />}
            >
              Invite Member
            </Button>
          </div>

          {isUsersError && (
            <QueryErrorAlert error={usersError} title="Failed to load team users" onRetry={() => refetchUsers()} />
          )}

          <Card className="p-0 overflow-hidden">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>User Email</TableHead>
                  <TableHead>Role</TableHead>
                  <TableHead>2FA Security</TableHead>
                  <TableHead>Joined</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {isUsersLoading ? (
                  <TableRow>
                    <TableCell colSpan={5} className="text-center py-6 text-zinc-500 text-xs">
                      Loading team members...
                    </TableCell>
                  </TableRow>
                ) : safeUsers.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={5} className="text-center py-6 text-zinc-500 text-xs">
                      No team members found.
                    </TableCell>
                  </TableRow>
                ) : (
                  safeUsers.map((u) => (
                    <TableRow key={u.id}>
                      <TableCell className="font-semibold text-zinc-200">
                        <div className="flex items-center gap-2">
                          <UserIcon className="h-4 w-4 text-cyan-400 shrink-0" />
                          <span>{u.email}</span>
                          {u.id === user?.id && (
                            <span className="text-[10px] bg-cyan-950 text-cyan-400 px-1.5 py-0.5 rounded border border-cyan-800">You</span>
                          )}
                        </div>
                      </TableCell>
                      <TableCell>
                        <select
                          value={u.role}
                          onChange={(e) => updateRoleMutation.mutate({ id: u.id, role: e.target.value })}
                          className="bg-zinc-900 border border-zinc-800 rounded px-2 py-1 text-xs text-zinc-200 focus:outline-none focus:border-cyan-500 font-mono"
                        >
                          <option value="owner">OWNER</option>
                          <option value="admin">ADMIN</option>
                          <option value="developer">DEVELOPER</option>
                          <option value="viewer">VIEWER</option>
                        </select>
                      </TableCell>
                      <TableCell>
                        <Badge variant={u.totp_enabled ? 'success' : 'default'} className="text-[10px]">
                          {u.totp_enabled ? 'TOTP 2FA' : 'Standard'}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-xs text-zinc-500 font-mono">
                        {formatDate(u.created_at)}
                      </TableCell>
                      <TableCell className="text-right">
                        <div className="flex items-center justify-end gap-1">
                          <Button
                            variant="ghost"
                            size="sm"
                            title="Reset password"
                            onClick={() => {
                              setResetTargetUser(u);
                              setNewPassword('');
                            }}
                          >
                            <KeyRound className="h-3.5 w-3.5 text-cyan-400" />
                          </Button>

                          {u.id !== user?.id && (
                            <Button
                              variant="ghost"
                              size="sm"
                              title="Remove user"
                              onClick={() => {
                                if (confirm(`Remove user ${u.email} from organization?`)) {
                                  deleteUserMutation.mutate(u.id);
                                }
                              }}
                            >
                              <Trash2 className="h-3.5 w-3.5 text-rose-400" />
                            </Button>
                          )}
                        </div>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </Card>
        </div>
      )}

      {/* 2. API Tokens Tab */}
      {activeTab === 'tokens' && (
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

          {isTokensError && (
            <QueryErrorAlert error={tokensError} title="Failed to load API tokens" onRetry={() => refetchTokens()} />
          )}

          <Card className="p-0 overflow-hidden">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Token Name</TableHead>
                  <TableHead>Prefix / ID</TableHead>
                  <TableHead>Granted Scopes</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {isTokensLoading ? (
                  <TableRow>
                    <TableCell colSpan={5} className="text-center py-6 text-zinc-500 text-xs">
                      Loading API tokens...
                    </TableCell>
                  </TableRow>
                ) : safeTokens.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={5} className="text-center py-6 text-zinc-500 text-xs">
                      No active API tokens found. Generate one for CLI access.
                    </TableCell>
                  </TableRow>
                ) : (
                  safeTokens.map((tok) => (
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
                      <TableCell className="text-xs text-zinc-500 font-mono">
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
      )}

      {/* 3. Notifications Tab */}
      {activeTab === 'notifications' && (
        <div className="space-y-4">
          <div className="flex items-center justify-between">
            <div>
              <h2 className="text-base font-semibold text-zinc-100">Multi-Channel Alert Destinations</h2>
              <p className="text-xs text-zinc-400">
                Real-time incident notifications dispatched to Webhooks, Discord, Slack, and Telegram
              </p>
            </div>

            <Button
              variant="primary"
              size="sm"
              onClick={() => setIsNotifyModalOpen(true)}
              leftIcon={<Plus className="h-4 w-4" />}
            >
              Add Channel
            </Button>
          </div>

          {isNotifyError && (
            <QueryErrorAlert error={notifyError} title="Failed to load notification channels" onRetry={() => refetchNotify()} />
          )}

          <Card className="p-0 overflow-hidden">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Channel Name</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Target URL</TableHead>
                  <TableHead>Triggers</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {isNotifyLoading ? (
                  <TableRow>
                    <TableCell colSpan={5} className="text-center py-6 text-zinc-500 text-xs">
                      Loading notification channels...
                    </TableCell>
                  </TableRow>
                ) : safeChannels.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={5} className="text-center py-6 text-zinc-500 text-xs">
                      No notification destinations configured yet.
                    </TableCell>
                  </TableRow>
                ) : (
                  safeChannels.map((ch) => (
                    <TableRow key={ch.id}>
                      <TableCell className="font-semibold text-zinc-200">
                        <div className="flex items-center gap-2">
                          <Bell className="h-4 w-4 text-cyan-400 shrink-0" />
                          <span>{ch.name}</span>
                        </div>
                      </TableCell>
                      <TableCell>
                        <Badge variant="outline" className="capitalize text-[11px]">
                          {ch.type}
                        </Badge>
                      </TableCell>
                      <TableCell className="font-mono text-xs text-zinc-400 max-w-xs truncate">
                        {ch.target_url}
                      </TableCell>
                      <TableCell>
                        <div className="flex flex-wrap gap-1">
                          {ch.events?.map((ev) => (
                            <span
                              key={ev}
                              className="px-1.5 py-0.5 rounded bg-zinc-800 border border-zinc-700 text-[11px] font-mono text-zinc-300"
                            >
                              {ev}
                            </span>
                          ))}
                        </div>
                      </TableCell>
                      <TableCell className="text-right">
                        <div className="flex items-center justify-end gap-1">
                          <Button
                            variant="ghost"
                            size="sm"
                            title="Send test ping"
                            onClick={() => testNotifyMutation.mutate(ch.id)}
                            isLoading={testNotifyMutation.isPending && testNotifyMutation.variables === ch.id}
                          >
                            <Send className="h-3.5 w-3.5 text-cyan-400" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            title="Delete channel"
                            onClick={() => {
                              if (confirm(`Delete notification channel ${ch.name}?`)) {
                                deleteNotifyMutation.mutate(ch.id);
                              }
                            }}
                          >
                            <Trash2 className="h-3.5 w-3.5 text-rose-400" />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </Card>
        </div>
      )}

      {/* Invite Member Modal */}
      <Modal
        isOpen={isInviteModalOpen}
        onClose={() => setIsInviteModalOpen(false)}
        title="Invite Team Member"
        description="Generate a cryptographically signed invitation URL with role assignment"
      >
        {createdInvite ? (
          <div className="space-y-4">
            <div className="p-4 rounded-xl bg-emerald-950/40 border border-emerald-800/50 space-y-2">
              <span className="text-xs font-semibold text-emerald-300 block">Invitation Ready</span>
              <p className="text-xs text-zinc-300">
                Share this link with <span className="font-semibold text-white">{createdInvite.email}</span>. It will expire in {inviteDays} days.
              </p>
              <div className="flex items-center gap-2 pt-2">
                <input
                  type="text"
                  readOnly
                  value={createdInvite.invite_url}
                  className="flex-1 px-3 py-2 bg-zinc-950 border border-zinc-800 rounded-lg text-xs font-mono text-cyan-300 select-all"
                />
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => copyInviteToClipboard(createdInvite.invite_url)}
                  leftIcon={copiedInvite ? <Check className="h-4 w-4 text-emerald-400" /> : <Copy className="h-4 w-4" />}
                >
                  {copiedInvite ? 'Copied' : 'Copy'}
                </Button>
              </div>
            </div>
            <div className="flex justify-end">
              <Button variant="primary" size="sm" onClick={() => setIsInviteModalOpen(false)}>
                Done
              </Button>
            </div>
          </div>
        ) : (
          <form
            onSubmit={(e) => {
              e.preventDefault();
              inviteMutation.mutate({
                email: inviteEmail,
                role: inviteRole,
                expires_in_days: Number(inviteDays),
              });
            }}
            className="space-y-4"
          >
            <Input
              label="Member Email Address"
              type="email"
              placeholder="developer@company.com"
              value={inviteEmail}
              onChange={(e) => setInviteEmail(e.target.value)}
              required
            />

            <div>
              <label className="block text-xs font-semibold text-zinc-300 mb-1.5">
                Assigned Role
              </label>
              <select
                value={inviteRole}
                onChange={(e) => setInviteRole(e.target.value as any)}
                className="w-full px-3 py-2 bg-zinc-950 border border-zinc-800 rounded-lg text-xs text-zinc-200 focus:outline-none focus:border-cyan-500"
              >
                <option value="developer">Developer (Deploy services, PTY shell, view logs)</option>
                <option value="admin">Admin (Manage infrastructure, users, domains)</option>
                <option value="viewer">Viewer (Read-only dashboards and metrics)</option>
                <option value="owner">Owner (Full cluster root authority)</option>
              </select>
            </div>

            <Input
              label="Expiry (Days)"
              type="number"
              min={1}
              max={30}
              value={inviteDays}
              onChange={(e) => setInviteDays(Number(e.target.value))}
            />

            <div className="flex justify-end gap-2 pt-4 border-t border-zinc-800">
              <Button type="button" variant="outline" size="sm" onClick={() => setIsInviteModalOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" variant="primary" size="sm" isLoading={inviteMutation.isPending}>
                Create Invitation Link
              </Button>
            </div>
          </form>
        )}
      </Modal>

      {/* Password Reset Modal */}
      <Modal
        isOpen={Boolean(resetTargetUser)}
        onClose={() => setResetTargetUser(null)}
        title={`Reset Password for ${resetTargetUser?.email}`}
        description="Sets a new password and revokes all active web sessions and API tokens"
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (resetTargetUser) {
              resetPasswordMutation.mutate({ id: resetTargetUser.id, pwd: newPassword });
            }
          }}
          className="space-y-4"
        >
          <Input
            label="New Password"
            type="password"
            placeholder="Minimum 8 characters"
            value={newPassword}
            onChange={(e) => setNewPassword(e.target.value)}
            required
            minLength={8}
          />

          <div className="flex justify-end gap-2 pt-4 border-t border-zinc-800">
            <Button type="button" variant="outline" size="sm" onClick={() => setResetTargetUser(null)}>
              Cancel
            </Button>
            <Button type="submit" variant="primary" size="sm" isLoading={resetPasswordMutation.isPending}>
              Reset & Invalidate Sessions
            </Button>
          </div>
        </form>
      </Modal>

      {/* Generate API Token Modal */}
      <Modal
        isOpen={isCreateModalOpen}
        onClose={() => setIsCreateModalOpen(false)}
        title="Generate Scoped API Token"
        description="Tokens inherit permissions according to selected scopes"
      >
        {createdToken ? (
          <div className="space-y-4">
            <div className="p-4 rounded-xl bg-cyan-950/40 border border-cyan-800/50 space-y-2">
              <span className="text-xs font-semibold text-cyan-300 block">Secret Token (Display Once)</span>
              <p className="text-xs text-zinc-400">
                Copy this secret immediately. It will never be displayed again.
              </p>
              <div className="flex items-center gap-2 pt-2">
                <input
                  type="text"
                  readOnly
                  value={createdToken.raw_secret}
                  className="flex-1 px-3 py-2 bg-zinc-950 border border-zinc-800 rounded-lg text-xs font-mono text-cyan-300 select-all"
                />
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => copyTokenToClipboard(createdToken.raw_secret || '')}
                  leftIcon={copiedToken ? <Check className="h-4 w-4 text-emerald-400" /> : <Copy className="h-4 w-4" />}
                >
                  {copiedToken ? 'Copied' : 'Copy'}
                </Button>
              </div>
            </div>
            <div className="flex justify-end">
              <Button variant="primary" size="sm" onClick={() => setIsCreateModalOpen(false)}>
                Done
              </Button>
            </div>
          </div>
        ) : (
          <form
            onSubmit={(e) => {
              e.preventDefault();
              createMutation.mutate({
                name: tokenName,
                scopes: selectedScopes,
              });
            }}
            className="space-y-4"
          >
            <Input
              label="Token Description"
              placeholder="e.g. GitHub Actions Deploy Bot"
              value={tokenName}
              onChange={(e) => setTokenName(e.target.value)}
              required
            />

            <div>
              <label className="block text-xs font-semibold text-zinc-300 mb-2">
                Granted Scopes
              </label>
              <div className="space-y-2">
                {AVAILABLE_SCOPES.map((sc) => {
                  const isChecked = selectedScopes.includes(sc.id);
                  return (
                    <div
                      key={sc.id}
                      onClick={() => toggleScope(sc.id)}
                      className={`p-3 rounded-lg border text-xs cursor-pointer transition-all flex items-center justify-between ${
                        isChecked
                          ? 'border-cyan-500 bg-cyan-950/20 text-cyan-200'
                          : 'border-zinc-800 bg-zinc-900/40 text-zinc-400 hover:border-zinc-700'
                      }`}
                    >
                      <span className="font-mono">{sc.id}</span>
                      <span className="text-[11px] text-zinc-500">{sc.label}</span>
                    </div>
                  );
                })}
              </div>
            </div>

            <div className="flex justify-end gap-2 pt-4 border-t border-zinc-800">
              <Button type="button" variant="outline" size="sm" onClick={() => setIsCreateModalOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" variant="primary" size="sm" isLoading={createMutation.isPending}>
                Generate Token
              </Button>
            </div>
          </form>
        )}
      </Modal>

      {/* Add Notification Channel Modal */}
      <Modal
        isOpen={isNotifyModalOpen}
        onClose={() => setIsNotifyModalOpen(false)}
        title="Add Alert Destination"
        description="Dispatch automated incident alerts on deploy failures or backup events"
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            const eventsArr = notifyEvents.split(',').map((s) => s.trim()).filter(Boolean);
            createNotifyMutation.mutate({
              name: notifyName,
              type: notifyType,
              target_url: notifyURL,
              auth_token: notifyAuthToken || undefined,
              events: eventsArr,
            });
          }}
          className="space-y-4"
        >
          <Input
            label="Channel Name"
            placeholder="e.g. SRE Discord Alerts"
            value={notifyName}
            onChange={(e) => setNotifyName(e.target.value)}
            required
          />

          <div>
            <label className="block text-xs font-semibold text-zinc-300 mb-1.5">Destination Type</label>
            <select
              value={notifyType}
              onChange={(e) => setNotifyType(e.target.value as any)}
              className="w-full px-3 py-2 bg-zinc-950 border border-zinc-800 rounded-lg text-xs text-zinc-200 focus:outline-none focus:border-cyan-500"
            >
              <option value="discord">Discord Webhook</option>
              <option value="slack">Slack Incoming Webhook</option>
              <option value="telegram">Telegram Bot</option>
              <option value="webhook">Generic HTTP Webhook</option>
            </select>
          </div>

          <Input
            label="Target Webhook URL / Endpoint"
            placeholder="https://discord.com/api/webhooks/..."
            value={notifyURL}
            onChange={(e) => setNotifyURL(e.target.value)}
            required
          />

          <Input
            label="Auth Token / Signing Secret (Optional)"
            placeholder="Optional Bearer token or secret"
            value={notifyAuthToken}
            onChange={(e) => setNotifyAuthToken(e.target.value)}
          />

          <Input
            label="Subscribed Events (Comma-separated)"
            placeholder="deploy:failure,deploy:success,backup:failure,backup:success"
            value={notifyEvents}
            onChange={(e) => setNotifyEvents(e.target.value)}
            required
          />

          <div className="flex justify-end gap-2 pt-4 border-t border-zinc-800">
            <Button type="button" variant="outline" size="sm" onClick={() => setIsNotifyModalOpen(false)}>
              Cancel
            </Button>
            <Button type="submit" variant="primary" size="sm" isLoading={createNotifyMutation.isPending}>
              Add Destination
            </Button>
          </div>
        </form>
      </Modal>
    </div>
  );
}
