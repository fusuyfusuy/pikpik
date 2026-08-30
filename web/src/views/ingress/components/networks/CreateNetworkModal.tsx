import React, { useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { api } from '../../../../lib/api';
import { Modal } from '../../../../components/ui/Modal';
import { Input } from '../../../../components/ui/Input';
import { Button } from '../../../../components/ui/Button';
import { useToast } from '../../../../components/ui/Toast';
import { Network } from 'lucide-react';

interface CreateNetworkModalProps {
  isOpen: boolean;
  onClose: () => void;
  onSuccess?: () => void;
}

export function CreateNetworkModal({
  isOpen,
  onClose,
  onSuccess,
}: CreateNetworkModalProps) {
  const queryClient = useQueryClient();
  const toast = useToast();

  const [name, setName] = useState('');
  const [driver, setDriver] = useState<'bridge' | 'overlay' | 'macvlan'>('bridge');
  const [scope, setScope] = useState('project');
  const [isExternal, setIsExternal] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) {
      toast.error('Validation Error', 'Network name is required');
      return;
    }

    setIsSubmitting(true);
    try {
      await api.networks.create({
        name: name.trim(),
        driver,
        scope,
        is_external: isExternal,
      });

      toast.success('Virtual Network Created', `Network ${name} registered on Docker/Swarm host`);
      queryClient.invalidateQueries({ queryKey: ['networks'] });

      // Reset
      setName('');
      setDriver('bridge');
      setScope('project');
      setIsExternal(false);
      onClose();
      if (onSuccess) onSuccess();
    } catch (err: any) {
      toast.error('Failed to create network', err?.message || 'Network creation failed');
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title="Create Virtual Network / Mesh"
      description="Provision an isolated bridge or Swarm multi-host overlay network for project and stack services."
      size="md"
    >
      <form onSubmit={handleSubmit} className="space-y-4 pt-2">
        {/* Network Name */}
        <div>
          <label className="block text-xs font-semibold uppercase tracking-wider text-zinc-400 mb-1.5">
            Network Name
          </label>
          <div className="relative">
            <div className="absolute inset-y-0 left-0 pl-3 flex items-center pointer-events-none text-zinc-500">
              <Network className="w-4 h-4" />
            </div>
            <Input
              type="text"
              placeholder="e.g. pikpik_net_proj_custom or stack_mesh"
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="pl-9 font-mono"
              required
            />
          </div>
        </div>

        {/* Driver */}
        <div>
          <label className="block text-xs font-semibold uppercase tracking-wider text-zinc-400 mb-1.5">
            Network Driver
          </label>
          <select
            value={driver}
            onChange={(e) => setDriver(e.target.value as any)}
            className="w-full px-3 py-2 bg-zinc-900 border border-zinc-800 rounded-lg text-sm text-zinc-100 focus:outline-none focus:ring-2 focus:ring-cyan-500/40 focus:border-cyan-500/50"
          >
            <option value="bridge">Bridge (Single-Host Container Mesh)</option>
            <option value="overlay">Overlay (Multi-Host Swarm Mesh &amp; Ingress)</option>
            <option value="macvlan">Macvlan (Direct Physical L2 Network)</option>
          </select>
        </div>

        {/* Scope */}
        <div>
          <label className="block text-xs font-semibold uppercase tracking-wider text-zinc-400 mb-1.5">
            Network Scope
          </label>
          <select
            value={scope}
            onChange={(e) => setScope(e.target.value)}
            className="w-full px-3 py-2 bg-zinc-900 border border-zinc-800 rounded-lg text-sm text-zinc-100 focus:outline-none focus:ring-2 focus:ring-cyan-500/40 focus:border-cyan-500/50"
          >
            <option value="project">Project Scope (Default)</option>
            <option value="stack">Stack Isolated Scope</option>
            <option value="ingress">Ingress Overlay Scope</option>
            <option value="global">Global Mesh Scope</option>
          </select>
        </div>

        {/* External Flag */}
        <div className="flex items-center gap-2.5 p-3 rounded-lg bg-zinc-900/50 border border-zinc-800/80">
          <input
            type="checkbox"
            id="isExternal"
            checked={isExternal}
            onChange={(e) => setIsExternal(e.target.checked)}
            className="w-4 h-4 rounded bg-zinc-800 border-zinc-700 text-cyan-500 focus:ring-cyan-500/40"
          />
          <label htmlFor="isExternal" className="text-xs font-medium text-zinc-300 cursor-pointer select-none">
            Attach to Existing Pre-Configured Host Network (External)
          </label>
        </div>

        {/* Actions */}
        <div className="flex items-center justify-end gap-2 pt-3 border-t border-zinc-800/80">
          <Button type="button" variant="ghost" onClick={onClose} disabled={isSubmitting}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={isSubmitting}>
            {isSubmitting ? 'Creating Network...' : 'Create Network'}
          </Button>
        </div>
      </form>
    </Modal>
  );
}
