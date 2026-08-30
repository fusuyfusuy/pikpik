import React, { useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { api } from '../../../../lib/api';
import { App } from '../../../../lib/types';
import { Modal } from '../../../../components/ui/Modal';
import { Input } from '../../../../components/ui/Input';
import { Button } from '../../../../components/ui/Button';
import { useToast } from '../../../../components/ui/Toast';
import { Globe, ShieldCheck, Server } from 'lucide-react';

interface NewRouteModalProps {
  isOpen: boolean;
  onClose: () => void;
  apps: App[];
  onSuccess?: () => void;
}

export function NewRouteModal({
  isOpen,
  onClose,
  apps,
  onSuccess,
}: NewRouteModalProps) {
  const queryClient = useQueryClient();
  const toast = useToast();

  const [domain, setDomain] = useState('');
  const [appId, setAppId] = useState(apps[0]?.id || '');
  const [pathPrefix, setPathPrefix] = useState('/*');
  const [autoTLS, setAutoTLS] = useState(true);

  const [isSubmitting, setIsSubmitting] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!domain.trim()) {
      toast.error('Validation Error', 'Domain name cannot be empty');
      return;
    }
    if (!appId) {
      toast.error('Validation Error', 'Target app must be selected');
      return;
    }

    setIsSubmitting(true);
    try {
      // Bind domain to primary app
      await api.ingress.bindDomain({
        app_id: appId,
        domain: domain.trim(),
        auto_tls: autoTLS,
      });

      toast.success('Route Created', `Dynamic route for ${domain} successfully registered in Caddy`);
      queryClient.invalidateQueries({ queryKey: ['domains'] });
      queryClient.invalidateQueries({ queryKey: ['caddy-diagnostics'] });

      // Reset form
      setDomain('');
      onClose();
      if (onSuccess) onSuccess();
    } catch (err: any) {
      toast.error('Failed to create route', err?.message || 'Route creation error');
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title="Create Dynamic Ingress Route"
      description="Register a new domain and direct 1:1 upstream routing."
      size="md"
    >
      <form onSubmit={handleSubmit} className="space-y-4 pt-2">
        {/* Domain Name */}
        <div>
          <label className="block text-xs font-semibold uppercase tracking-wider text-zinc-400 mb-1.5">
            Domain Name
          </label>
          <div className="relative">
            <div className="absolute inset-y-0 left-0 pl-3 flex items-center pointer-events-none text-zinc-500">
              <Globe className="w-4 h-4" />
            </div>
            <Input
              type="text"
              placeholder="e.g. app.example.com or api.domain.dev"
              value={domain}
              onChange={(e) => setDomain(e.target.value)}
              className="pl-9"
              required
            />
          </div>
        </div>

        {/* Primary Service / Target */}
        <div>
          <label className="block text-xs font-semibold uppercase tracking-wider text-zinc-400 mb-1.5">
            Target Service
          </label>
          <div className="relative">
            <div className="absolute inset-y-0 left-0 pl-3 flex items-center pointer-events-none text-zinc-500">
              <Server className="w-4 h-4" />
            </div>
            <select
              value={appId}
              onChange={(e) => setAppId(e.target.value)}
              className="w-full pl-9 pr-3 py-2 bg-zinc-900 border border-zinc-800 rounded-lg text-sm text-zinc-100 focus:outline-none focus:ring-2 focus:ring-cyan-500/40 focus:border-cyan-500/50"
              required
            >
              {apps.map((app) => (
                <option key={app.id} value={app.id}>
                  {app.name} - Port {app.container_port || 80}
                </option>
              ))}
            </select>
          </div>
        </div>

        {/* Path Prefix */}
        <div>
          <label className="block text-xs font-semibold uppercase tracking-wider text-zinc-400 mb-1.5">
            Path Prefix Matching
          </label>
          <Input
            type="text"
            placeholder="/* or /api/v1"
            value={pathPrefix}
            onChange={(e) => setPathPrefix(e.target.value)}
          />
          <p className="text-[11px] text-zinc-500 mt-1">
            Specify matching HTTP path prefix (e.g. <code>/*</code>, <code>/v1</code>, <code>/auth</code>).
          </p>
        </div>

        {/* Auto TLS Option */}
        <div className="flex items-center gap-2.5 p-3 rounded-lg bg-zinc-900/50 border border-zinc-800/80">
          <input
            type="checkbox"
            id="autoTLS"
            checked={autoTLS}
            onChange={(e) => setAutoTLS(e.target.checked)}
            className="w-4 h-4 rounded bg-zinc-800 border-zinc-700 text-cyan-500 focus:ring-cyan-500/40"
          />
          <label htmlFor="autoTLS" className="flex items-center gap-2 text-xs font-medium text-zinc-300 cursor-pointer select-none">
            <ShieldCheck className="w-4 h-4 text-emerald-400" />
            <span>Enable Automated ACME TLS (HTTP-01 / On-Demand)</span>
          </label>
        </div>

        {/* Modal Actions */}
        <div className="flex items-center justify-end gap-2 pt-3 border-t border-zinc-800/80">
          <Button type="button" variant="ghost" onClick={onClose} disabled={isSubmitting}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={isSubmitting}>
            {isSubmitting ? 'Creating Route...' : 'Create Dynamic Route'}
          </Button>
        </div>
      </form>
    </Modal>
  );
}
