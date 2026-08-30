import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../lib/api';
import { BindDomainRequest, CertificateUploadRequest } from '../lib/types';
import { Card } from '../components/ui/Card';
import { Button } from '../components/ui/Button';
import { Badge } from '../components/ui/Badge';
import { Input, Textarea } from '../components/ui/Input';
import { Modal } from '../components/ui/Modal';
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '../components/ui/Table';
import { useToast } from '../components/ui/Toast';
import { formatDate } from '../lib/utils';
import {
  Globe,
  Plus,
  Shield,
  ShieldCheck,
  RotateCw,
  Trash2,
  Lock,
} from 'lucide-react';

export function IngressView() {
  const queryClient = useQueryClient();
  const toast = useToast();

  const [isBindModalOpen, setIsBindModalOpen] = useState(false);
  const [isCertModalOpen, setIsCertModalOpen] = useState(false);

  // Form states
  const [domain, setDomain] = useState('');
  const [appId, setAppId] = useState('');
  const [autoTLS, setAutoTLS] = useState(true);

  // Cert states
  const [certDomain, setCertDomain] = useState('');
  const [certPem, setCertPem] = useState('');
  const [keyPem, setKeyPem] = useState('');

  const { data: domains, isLoading } = useQuery({
    queryKey: ['domains'],
    queryFn: api.ingress.listDomains,
  });

  const { data: apps } = useQuery({
    queryKey: ['apps'],
    queryFn: api.apps.list,
  });

  const bindMutation = useMutation({
    mutationFn: (req: BindDomainRequest) => api.ingress.bindDomain(req),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['domains'] });
      toast.success('Domain Bound', `Domain ${domain} bound to application`);
      setIsBindModalOpen(false);
      setDomain('');
    },
    onError: (err: Error) => toast.error('Binding Failed', err.message),
  });

  const uploadCertMutation = useMutation({
    mutationFn: (req: CertificateUploadRequest) => api.ingress.uploadCertificate(req),
    onSuccess: () => {
      toast.success('Certificate Uploaded', 'Custom TLS certificate installed in Caddy');
      setIsCertModalOpen(false);
      setCertPem('');
      setKeyPem('');
    },
    onError: (err: Error) => toast.error('Upload Failed', err.message),
  });

  const deleteDomainMutation = useMutation({
    mutationFn: (id: string) => api.ingress.deleteDomain(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['domains'] });
      toast.success('Domain Removed', 'Domain binding removed from ingress router');
    },
    onError: (err: Error) => toast.error('Delete Failed', err.message),
  });

  const reconcileMutation = useMutation({
    mutationFn: api.ingress.reconcile,
    onSuccess: () => {
      toast.success('Ingress Reconciled', 'Caddy dynamic routes synchronization complete');
    },
    onError: (err: Error) => toast.error('Reconcile Failed', err.message),
  });

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-zinc-100 flex items-center gap-2">
            <Globe className="h-5 w-5 text-cyan-400" />
            <span>Ingress & Auto-TLS</span>
          </h1>
          <p className="text-xs text-zinc-400 mt-0.5">
            Caddy dynamic reverse-proxy with automated Let's Encrypt certificates & custom SNI
          </p>
        </div>

        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => reconcileMutation.mutate()}
            isLoading={reconcileMutation.isPending}
            leftIcon={<RotateCw className="h-3.5 w-3.5" />}
          >
            Reconcile Caddy
          </Button>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => setIsCertModalOpen(true)}
            leftIcon={<Shield className="h-3.5 w-3.5" />}
          >
            Upload Cert
          </Button>
          <Button
            variant="primary"
            size="sm"
            onClick={() => setIsBindModalOpen(true)}
            leftIcon={<Plus className="h-4 w-4" />}
          >
            Bind Domain
          </Button>
        </div>
      </div>

      {/* Domains Table */}
      <Card className="p-0 overflow-hidden">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Domain Name</TableHead>
              <TableHead>Target App / Service</TableHead>
              <TableHead>TLS Provider</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Registered</TableHead>
              <TableHead className="text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <TableRow>
                <TableCell colSpan={6} className="text-center py-8 text-zinc-500">
                  Loading domain routes...
                </TableCell>
              </TableRow>
            ) : (!domains || domains.length === 0) ? (
              <TableRow>
                <TableCell colSpan={6} className="text-center py-10 text-zinc-500">
                  No ingress domains mapped yet. Click "Bind Domain" to route HTTP traffic.
                </TableCell>
              </TableRow>
            ) : (
              domains.map((binding) => {
                const targetApp = apps?.find((a) => a.id === binding.app_id);
                return (
                  <TableRow key={binding.id}>
                    <TableCell className="font-semibold text-zinc-100">
                      <div className="flex items-center gap-2">
                        <Globe className="h-4 w-4 text-cyan-400 shrink-0" />
                        <span className="font-mono">{binding.domain}</span>
                      </div>
                    </TableCell>
                    <TableCell className="font-mono text-xs text-zinc-300">
                      {targetApp ? targetApp.name : binding.app_id}
                    </TableCell>
                    <TableCell>
                      {binding.auto_tls ? (
                        <span className="inline-flex items-center gap-1 text-xs text-emerald-400 font-medium">
                          <ShieldCheck className="h-3.5 w-3.5" />
                          Auto Let's Encrypt
                        </span>
                      ) : (
                        <span className="inline-flex items-center gap-1 text-xs text-zinc-400 font-medium">
                          <Lock className="h-3.5 w-3.5" />
                          Custom TLS
                        </span>
                      )}
                    </TableCell>
                    <TableCell>
                      <Badge variant={binding.status === 'active' ? 'success' : 'warning'} dot>
                        {binding.status}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-xs text-zinc-500">
                      {formatDate(binding.created_at)}
                    </TableCell>
                    <TableCell className="text-right">
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => {
                          if (confirm(`Remove domain ${binding.domain}?`)) {
                            deleteDomainMutation.mutate(binding.id);
                          }
                        }}
                      >
                        <Trash2 className="h-3.5 w-3.5 text-rose-400" />
                      </Button>
                    </TableCell>
                  </TableRow>
                );
              })
            )}
          </TableBody>
        </Table>
      </Card>

      {/* Bind Domain Modal */}
      <Modal
        isOpen={isBindModalOpen}
        onClose={() => setIsBindModalOpen(false)}
        title="Bind Custom Domain"
        description="Route external traffic through Caddy with automated HTTPS certificate provisioning"
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            bindMutation.mutate({ app_id: appId, domain, auto_tls: autoTLS });
          }}
          className="space-y-4"
        >
          <Input
            label="Domain Name (FQDN)"
            placeholder="api.yourdomain.com"
            value={domain}
            onChange={(e) => setDomain(e.target.value)}
            required
          />

          <div className="space-y-1.5">
            <label className="block text-xs font-medium text-zinc-300">Target Application</label>
            <select
              value={appId}
              onChange={(e) => setAppId(e.target.value)}
              className="w-full rounded-lg border border-zinc-800 bg-zinc-950 px-3.5 py-2 text-sm text-zinc-100 focus:border-cyan-500 focus:outline-none"
              required
            >
              <option value="">Select an application...</option>
              {apps?.map((app) => (
                <option key={app.id} value={app.id}>
                  {app.name} ({app.id})
                </option>
              ))}
            </select>
          </div>

          <div className="flex items-center gap-2 pt-2">
            <input
              type="checkbox"
              id="autoTLS"
              checked={autoTLS}
              onChange={(e) => setAutoTLS(e.target.checked)}
              className="h-4 w-4 rounded border-zinc-700 bg-zinc-900 text-cyan-500 focus:ring-cyan-500"
            />
            <label htmlFor="autoTLS" className="text-xs text-zinc-300 select-none">
              Automatically issue and manage free SSL/TLS certificate (Let's Encrypt / ACME)
            </label>
          </div>

          <div className="flex justify-end gap-2 pt-4 border-t border-zinc-800">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setIsBindModalOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              variant="primary"
              size="sm"
              isLoading={bindMutation.isPending}
            >
              Bind Domain
            </Button>
          </div>
        </form>
      </Modal>

      {/* Upload Cert Modal */}
      <Modal
        isOpen={isCertModalOpen}
        onClose={() => setIsCertModalOpen(false)}
        title="Upload Custom SSL/TLS Certificate"
        description="Install custom wildcard or enterprise certificates"
        size="lg"
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            uploadCertMutation.mutate({ domain: certDomain, cert_pem: certPem, key_pem: keyPem });
          }}
          className="space-y-4"
        >
          <Input
            label="Domain / Wildcard Pattern"
            placeholder="*.example.com"
            value={certDomain}
            onChange={(e) => setCertDomain(e.target.value)}
            required
          />

          <Textarea
            label="Certificate PEM (fullchain.pem)"
            rows={5}
            placeholder="-----BEGIN CERTIFICATE-----&#10;...&#10;-----END CERTIFICATE-----"
            value={certPem}
            onChange={(e) => setCertPem(e.target.value)}
            required
          />

          <Textarea
            label="Private Key PEM (privkey.pem)"
            rows={5}
            placeholder="-----BEGIN PRIVATE KEY-----&#10;...&#10;-----END PRIVATE KEY-----"
            value={keyPem}
            onChange={(e) => setKeyPem(e.target.value)}
            required
          />

          <div className="flex justify-end gap-2 pt-4 border-t border-zinc-800">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setIsCertModalOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              variant="primary"
              size="sm"
              isLoading={uploadCertMutation.isPending}
            >
              Install Certificate
            </Button>
          </div>
        </form>
      </Modal>
    </div>
  );
}
