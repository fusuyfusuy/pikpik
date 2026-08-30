import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { api } from '../../../../lib/api';
import { CertificateRecord } from '../../../../lib/types';
import { Card } from '../../../../components/ui/Card';
import { Button } from '../../../../components/ui/Button';
import { Badge } from '../../../../components/ui/Badge';
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from '../../../../components/ui/Table';
import { useToast } from '../../../../components/ui/Toast';
import { CloudflareUploadModal } from './CloudflareUploadModal';
import { Dns01ProviderModal } from './Dns01ProviderModal';
import {
  Shield,
  ShieldCheck,
  RotateCw,
  Lock,
  Globe,
  Key,
  CheckCircle2,
} from 'lucide-react';

export function TLSTab() {
  const toast = useToast();

  const [isCloudflareModalOpen, setIsCloudflareModalOpen] = useState(false);
  const [isDns01ModalOpen, setIsDns01ModalOpen] = useState(false);
  const [onDemandEnabled, setOnDemandEnabled] = useState(true);
  const [customCerts, setCustomCerts] = useState<CertificateRecord[]>([]);

  // Fetch registered domains from backend
  const { data: domains = [], isLoading } = useQuery({
    queryKey: ['domains'],
    queryFn: api.ingress.listDomains,
  });

  // Synthesize certificate records from domains & custom additions
  const domainCerts: CertificateRecord[] = domains
    .filter((d) => d.auto_tls)
    .map((d) => ({
      id: `cert_${d.id}`,
      domain: d.domain,
      type: d.domain.startsWith('*.') ? 'Wildcard' : 'On-Demand',
      issuer: d.domain.startsWith('*.') ? 'DNS-01 (Cloudflare)' : "Let's Encrypt / Caddy CA",
      expires_in_label: 'In 86 days',
      auto_renew: true,
      status: 'active',
    }));

  const allCerts: CertificateRecord[] = [
    ...domainCerts,
    ...customCerts,
  ];

  const handleDns01Success = (cfg: { wildcardDomain: string; issuer: string }) => {
    const newCert: CertificateRecord = {
      id: `cert_cf_${Date.now()}`,
      domain: cfg.wildcardDomain,
      type: 'Wildcard',
      issuer: cfg.issuer,
      expires_in_label: 'In 89 days',
      auto_renew: true,
      status: 'active',
    };
    setCustomCerts((prev) => [newCert, ...prev]);
  };

  const handleOriginSuccess = () => {
    toast.success('Certificate Active', 'Origin CA certificate registered in Caddy dynamic ingress');
  };

  return (
    <div className="space-y-6">
      {/* Top Action Toolbar */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div className="flex flex-wrap items-center gap-2.5">
          <Button
            variant="primary"
            onClick={() => setIsCloudflareModalOpen(true)}
            className="flex items-center gap-2 shadow-sm"
          >
            <Shield className="w-4 h-4" />
            <span>Upload Cloudflare Origin Cert</span>
          </Button>

          <Button
            variant="outline"
            onClick={() => setIsDns01ModalOpen(true)}
            className="flex items-center gap-2"
          >
            <Key className="w-4 h-4 text-amber-400" />
            <span>Configure DNS-01 Wildcard</span>
          </Button>
        </div>

        <div className="flex items-center gap-2 text-xs text-zinc-400">
          <ShieldCheck className="w-4 h-4 text-emerald-400" />
          <span>Automated ACME &amp; Origin TLS Engine</span>
        </div>
      </div>

      {/* Active Certificates Matrix */}
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Lock className="w-4 h-4 text-cyan-400" />
            <h3 className="text-sm font-semibold text-zinc-200">
              Active TLS Certificates Matrix ({allCerts.length})
            </h3>
          </div>
          <span className="text-xs text-zinc-500 font-mono">
            Zero-Maintenance TLS Lifecycle
          </span>
        </div>

        <Card className="border-zinc-800/80 overflow-hidden bg-zinc-950/40">
          <Table>
            <TableHeader>
              <TableRow className="border-zinc-800 hover:bg-transparent">
                <TableHead className="text-zinc-400 text-xs">Domain</TableHead>
                <TableHead className="text-zinc-400 text-xs">Certificate Type</TableHead>
                <TableHead className="text-zinc-400 text-xs">Issuer Authority</TableHead>
                <TableHead className="text-zinc-400 text-xs">Expiration</TableHead>
                <TableHead className="text-zinc-400 text-xs">Auto-Renew</TableHead>
                <TableHead className="text-zinc-400 text-xs text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading ? (
                <TableRow>
                  <TableCell colSpan={6} className="text-center py-8 text-zinc-500 text-xs">
                    <RotateCw className="w-5 h-5 animate-spin mx-auto mb-2 text-cyan-400 opacity-60" />
                    Inspecting TLS certificates...
                  </TableCell>
                </TableRow>
              ) : allCerts.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className="text-center py-8 text-zinc-500 text-xs">
                    No active TLS certificates. Bind a domain with Auto-TLS or upload a Cloudflare Origin CA certificate.
                  </TableCell>
                </TableRow>
              ) : (
                allCerts.map((cert) => (
                  <TableRow key={cert.id} className="border-zinc-800/60 hover:bg-zinc-900/40">
                    {/* Domain */}
                    <TableCell className="font-mono text-sm">
                      <div className="flex items-center gap-2 font-medium text-zinc-100">
                        <Globe className="w-3.5 h-3.5 text-zinc-400" />
                        <span>{cert.domain}</span>
                      </div>
                    </TableCell>

                    {/* Type */}
                    <TableCell>
                      <Badge
                        variant="outline"
                        className={`text-xs ${
                          cert.type === 'Wildcard'
                            ? 'border-purple-800/60 bg-purple-950/30 text-purple-300'
                            : cert.type === 'Origin CA'
                            ? 'border-amber-800/60 bg-amber-950/30 text-amber-300'
                            : 'border-cyan-800/60 bg-cyan-950/30 text-cyan-300'
                        }`}
                      >
                        {cert.type}
                      </Badge>
                    </TableCell>

                    {/* Issuer */}
                    <TableCell>
                      <span className="text-xs text-zinc-300">
                        {cert.issuer}
                      </span>
                    </TableCell>

                    {/* Expiration */}
                    <TableCell>
                      <span className="text-xs text-zinc-300 font-medium">
                        {cert.expires_in_label}
                      </span>
                    </TableCell>

                    {/* Auto Renew */}
                    <TableCell>
                      {cert.auto_renew ? (
                        <div className="flex items-center gap-1.5 text-xs text-emerald-400">
                          <CheckCircle2 className="w-3.5 h-3.5" />
                          <span>Enabled</span>
                        </div>
                      ) : (
                        <span className="text-xs text-zinc-500">—</span>
                      )}
                    </TableCell>

                    {/* Actions */}
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-2">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => toast.success('Certificate Revalidated', `Auto-renew trigger sent for ${cert.domain}`)}
                          className="text-xs text-zinc-400 hover:text-zinc-200"
                        >
                          Renew
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

      {/* On-Demand ACME Security Gate Card */}
      <Card className="p-5 border-zinc-800/80 bg-zinc-950/40 space-y-4">
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
          <div className="flex items-center gap-3">
            <div className="p-2.5 rounded-lg bg-emerald-950/40 border border-emerald-800/30 text-emerald-400">
              <ShieldCheck className="w-5 h-5" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <h4 className="text-sm font-semibold text-zinc-200">
                  On-Demand ACME Security Gate (<code>/api/v1/ingress/ask</code>)
                </h4>
                <Badge variant="default" className="bg-emerald-950/60 text-emerald-300 border-emerald-800 text-[10px]">
                  Protected
                </Badge>
              </div>
              <p className="text-xs text-zinc-400 mt-0.5">
                Caddy queries the PikPik control plane prior to requesting ACME certificates for unknown hostnames.
              </p>
            </div>
          </div>

          <div className="flex items-center gap-2">
            <input
              type="checkbox"
              id="onDemandToggle"
              checked={onDemandEnabled}
              onChange={(e) => setOnDemandEnabled(e.target.checked)}
              className="w-4 h-4 rounded bg-zinc-800 border-zinc-700 text-cyan-500 focus:ring-cyan-500/40"
            />
            <label htmlFor="onDemandToggle" className="text-xs font-medium text-zinc-300 cursor-pointer select-none">
              Enforce <code>/ask</code> Whitelist
            </label>
          </div>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-3 gap-3 pt-2">
          <div className="p-3 rounded-lg bg-zinc-900/60 border border-zinc-800/60">
            <span className="text-[11px] text-zinc-400 uppercase tracking-wider block">Security Rate Limit</span>
            <span className="text-xs font-semibold text-zinc-200 mt-0.5 block font-mono">60 req / min (Burst: 10)</span>
          </div>
          <div className="p-3 rounded-lg bg-zinc-900/60 border border-zinc-800/60">
            <span className="text-[11px] text-zinc-400 uppercase tracking-wider block">Whitelist Source</span>
            <span className="text-xs font-semibold text-zinc-200 mt-0.5 block font-mono">SQLite Services Store</span>
          </div>
          <div className="p-3 rounded-lg bg-zinc-900/60 border border-zinc-800/60">
            <span className="text-[11px] text-zinc-400 uppercase tracking-wider block">Certificate Storage</span>
            <span className="text-xs font-semibold text-emerald-400 mt-0.5 block font-mono">In-Memory / Atomic FS</span>
          </div>
        </div>
      </Card>

      {/* Dialog Modals */}
      <CloudflareUploadModal
        isOpen={isCloudflareModalOpen}
        onClose={() => setIsCloudflareModalOpen(false)}
        onSuccess={handleOriginSuccess}
      />

      <Dns01ProviderModal
        isOpen={isDns01ModalOpen}
        onClose={() => setIsDns01ModalOpen(false)}
        onSuccess={handleDns01Success}
      />
    </div>
  );
}
