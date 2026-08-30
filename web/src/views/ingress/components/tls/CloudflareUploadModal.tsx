import React, { useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { api } from '../../../../lib/api';
import { Modal } from '../../../../components/ui/Modal';
import { Input, Textarea } from '../../../../components/ui/Input';
import { Button } from '../../../../components/ui/Button';
import { useToast } from '../../../../components/ui/Toast';
import { Shield, Key, Globe, Sparkles } from 'lucide-react';

interface CloudflareUploadModalProps {
  isOpen: boolean;
  onClose: () => void;
  onSuccess?: () => void;
}

export function CloudflareUploadModal({
  isOpen,
  onClose,
  onSuccess,
}: CloudflareUploadModalProps) {
  const queryClient = useQueryClient();
  const toast = useToast();

  const [domain, setDomain] = useState('');
  const [certPEM, setCertPEM] = useState('');
  const [keyPEM, setKeyPEM] = useState('');
  const [isSubmitting, setIsSubmitting] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!domain.trim() || !certPEM.trim() || !keyPEM.trim()) {
      toast.error('Validation Error', 'Domain, Certificate PEM, and Private Key PEM are required');
      return;
    }

    setIsSubmitting(true);
    try {
      await api.ingress.uploadCertificate({
        domain: domain.trim(),
        cert_pem: certPEM.trim(),
        key_pem: keyPEM.trim(),
      });

      toast.success(
        'Origin Certificate Installed',
        `Cloudflare 15-year Origin CA certificate for ${domain} loaded into Caddy TLS storage`
      );
      queryClient.invalidateQueries({ queryKey: ['domains'] });
      queryClient.invalidateQueries({ queryKey: ['certificates'] });

      // Reset
      setDomain('');
      setCertPEM('');
      setKeyPEM('');
      onClose();
      if (onSuccess) onSuccess();
    } catch (err: any) {
      toast.error('Certificate Upload Failed', err?.message || 'Error processing certificate payload');
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title="Install Cloudflare 15-Year Origin CA Certificate"
      description="Paste the Origin CA certificate and private key generated from the Cloudflare Dashboard (SSL/TLS → Origin Server)."
      size="lg"
    >
      <form onSubmit={handleSubmit} className="space-y-4 pt-2">
        {/* Domain name */}
        <div>
          <label className="block text-xs font-semibold uppercase tracking-wider text-zinc-400 mb-1.5">
            Domain Name or Wildcard
          </label>
          <div className="relative">
            <div className="absolute inset-y-0 left-0 pl-3 flex items-center pointer-events-none text-zinc-500">
              <Globe className="w-4 h-4" />
            </div>
            <Input
              type="text"
              placeholder="e.g. *.example.com or api.domain.com"
              value={domain}
              onChange={(e) => setDomain(e.target.value)}
              className="pl-9 font-mono"
              required
            />
          </div>
          <p className="text-[11px] text-zinc-500 mt-1">
            Specify the exact domain or wildcard matching the Cloudflare Origin Certificate hostnames.
          </p>
        </div>

        {/* Certificate PEM */}
        <div>
          <label className="block text-xs font-semibold uppercase tracking-wider text-zinc-400 mb-1.5 flex items-center justify-between">
            <span className="flex items-center gap-1.5">
              <Shield className="w-3.5 h-3.5 text-cyan-400" />
              Origin Certificate (PEM Format)
            </span>
            <span className="text-[10px] text-zinc-500 font-mono">-----BEGIN CERTIFICATE-----</span>
          </label>
          <Textarea
            rows={5}
            placeholder="-----BEGIN CERTIFICATE-----&#10;MIIE...&#10;-----END CERTIFICATE-----"
            value={certPEM}
            onChange={(e) => setCertPEM(e.target.value)}
            className="font-mono text-xs"
            required
          />
        </div>

        {/* Private Key PEM */}
        <div>
          <label className="block text-xs font-semibold uppercase tracking-wider text-zinc-400 mb-1.5 flex items-center justify-between">
            <span className="flex items-center gap-1.5">
              <Key className="w-3.5 h-3.5 text-amber-400" />
              Private Key (PEM Format)
            </span>
            <span className="text-[10px] text-zinc-500 font-mono">-----BEGIN PRIVATE KEY-----</span>
          </label>
          <Textarea
            rows={5}
            placeholder="-----BEGIN PRIVATE KEY-----&#10;MIIE...&#10;-----END PRIVATE KEY-----"
            value={keyPEM}
            onChange={(e) => setKeyPEM(e.target.value)}
            className="font-mono text-xs"
            required
          />
        </div>

        {/* Informational Callout */}
        <div className="flex items-start gap-2.5 p-3 rounded-lg bg-cyan-950/20 border border-cyan-800/40 text-xs text-cyan-200">
          <Sparkles className="w-4 h-4 text-cyan-400 shrink-0 mt-0.5" />
          <span>
            Cloudflare Origin CA certificates provide end-to-end encryption with 15-year validity and eliminate automated ACME challenge rate limits.
          </span>
        </div>

        {/* Modal Actions */}
        <div className="flex items-center justify-end gap-2 pt-3 border-t border-zinc-800/80">
          <Button type="button" variant="ghost" onClick={onClose} disabled={isSubmitting}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={isSubmitting}>
            {isSubmitting ? 'Installing Certificate...' : 'Install Origin Certificate'}
          </Button>
        </div>
      </form>
    </Modal>
  );
}
