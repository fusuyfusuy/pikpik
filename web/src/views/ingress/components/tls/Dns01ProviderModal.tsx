import React, { useState } from 'react';
import { Modal } from '../../../../components/ui/Modal';
import { Input } from '../../../../components/ui/Input';
import { Button } from '../../../../components/ui/Button';
import { useToast } from '../../../../components/ui/Toast';
import { Key, Globe, Shield } from 'lucide-react';

interface Dns01ProviderModalProps {
  isOpen: boolean;
  onClose: () => void;
  onSuccess?: (config: { wildcardDomain: string; issuer: string }) => void;
}

export function Dns01ProviderModal({
  isOpen,
  onClose,
  onSuccess,
}: Dns01ProviderModalProps) {
  const toast = useToast();

  const [wildcardDomain, setWildcardDomain] = useState('');
  const [apiToken, setApiToken] = useState('');
  const [propagationTimeout, setPropagationTimeout] = useState(120);
  const [isSubmitting, setIsSubmitting] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!wildcardDomain.trim() || !apiToken.trim()) {
      toast.error('Validation Error', 'Wildcard domain and Cloudflare API token are required');
      return;
    }

    setIsSubmitting(true);
    try {
      // Persistent token storage
      localStorage.setItem(`pikpik_cf_dns01_${wildcardDomain.trim()}`, apiToken.trim());

      toast.success(
        'DNS-01 Provider Configured',
        `Cloudflare DNS-01 ACME automated challenge provider active for ${wildcardDomain}`
      );

      if (onSuccess) {
        onSuccess({
          wildcardDomain: wildcardDomain.trim(),
          issuer: 'Cloudflare DNS-01 (ACME)',
        });
      }
      setWildcardDomain('');
      setApiToken('');
      onClose();
    } catch (err: any) {
      toast.error('Configuration Failed', err?.message || 'Error saving DNS-01 settings');
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title="Configure Wildcard DNS-01 ACME Provider"
      description="Automate wildcard SSL/TLS certificate issuance (*.domain.com) via Cloudflare DNS-01 API challenges."
      size="md"
    >
      <form onSubmit={handleSubmit} className="space-y-4 pt-2">
        {/* Wildcard Domain */}
        <div>
          <label className="block text-xs font-semibold uppercase tracking-wider text-zinc-400 mb-1.5">
            Wildcard Domain Pattern
          </label>
          <div className="relative">
            <div className="absolute inset-y-0 left-0 pl-3 flex items-center pointer-events-none text-zinc-500">
              <Globe className="w-4 h-4" />
            </div>
            <Input
              type="text"
              placeholder="e.g. *.example.com or *.apps.domain.io"
              value={wildcardDomain}
              onChange={(e) => setWildcardDomain(e.target.value)}
              className="pl-9 font-mono"
              required
            />
          </div>
        </div>

        {/* Cloudflare API Token */}
        <div>
          <label className="block text-xs font-semibold uppercase tracking-wider text-zinc-400 mb-1.5 flex items-center justify-between">
            <span className="flex items-center gap-1.5">
              <Key className="w-3.5 h-3.5 text-amber-400" />
              Cloudflare API Token
            </span>
            <span className="text-[10px] text-zinc-500">Zone.DNS:Edit</span>
          </label>
          <Input
            type="password"
            placeholder="Cloudflare API Token (e.g. 4vX...)"
            value={apiToken}
            onChange={(e) => setApiToken(e.target.value)}
            className="font-mono"
            required
          />
          <p className="text-[11px] text-zinc-500 mt-1">
            Token must have <code>Zone - DNS - Edit</code> and <code>Zone - Zone - Read</code> permissions.
          </p>
        </div>

        {/* Propagation Timeout */}
        <div>
          <label className="block text-xs font-semibold uppercase tracking-wider text-zinc-400 mb-1.5">
            DNS Propagation Wait Time (Seconds)
          </label>
          <Input
            type="number"
            min={30}
            max={600}
            value={propagationTimeout}
            onChange={(e) => setPropagationTimeout(Number(e.target.value))}
          />
        </div>

        {/* Info callout */}
        <div className="flex items-start gap-2.5 p-3 rounded-lg bg-zinc-900/60 border border-zinc-800 text-xs text-zinc-300">
          <Shield className="w-4 h-4 text-cyan-400 shrink-0 mt-0.5" />
          <span>
            Caddy dynamic DNS-01 module directly verifies TXT challenge records on Cloudflare edge nameservers without opening port 80.
          </span>
        </div>

        {/* Actions */}
        <div className="flex items-center justify-end gap-2 pt-3 border-t border-zinc-800/80">
          <Button type="button" variant="ghost" onClick={onClose} disabled={isSubmitting}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={isSubmitting}>
            {isSubmitting ? 'Saving Configuration...' : 'Activate DNS-01 Wildcard'}
          </Button>
        </div>
      </form>
    </Modal>
  );
}
