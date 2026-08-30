import { NetworkDTO, App } from '../../../../lib/types';
import { Network, Layers, ShieldCheck } from 'lucide-react';
import { Badge } from '../../../../components/ui/Badge';

interface NetworkTopologyProps {
  networks: NetworkDTO[];
  apps: App[];
}

export function NetworkTopology({ networks }: NetworkTopologyProps) {
  // Categorize networks
  const stackNetworks = networks.filter((n) => n.scope === 'stack' || n.name.includes('stack'));
  const projectNetworks = networks.filter((n) => n.scope === 'project' || n.name.includes('proj'));
  const ingressNetworks = networks.filter((n) => n.scope === 'ingress' || n.name.includes('ingress'));

  return (
    <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
      {/* 1. Ingress Overlay Card */}
      <div className="p-4 rounded-xl bg-cyan-950/20 border border-cyan-800/40 space-y-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <ShieldCheck className="w-4 h-4 text-cyan-400" />
            <h4 className="text-xs font-semibold text-cyan-200 uppercase tracking-wider">
              Ingress Overlay Mesh
            </h4>
          </div>
          <Badge variant="default" className="bg-cyan-900/60 text-cyan-300 border-cyan-700 text-[10px]">
            {ingressNetworks.length || 1} Active
          </Badge>
        </div>
        <p className="text-xs text-zinc-400">
          Encrypted Swarm overlay network bridging external Caddy ingress with internal container services.
        </p>
        <div className="pt-2 border-t border-cyan-900/40 flex items-center justify-between text-xs text-zinc-300">
          <span className="font-mono text-[11px] text-zinc-400">Subnet: 10.0.99.0/24</span>
          <span className="text-cyan-400 font-mono">Overlay / VXLAN</span>
        </div>
      </div>

      {/* 2. Project Mesh Card */}
      <div className="p-4 rounded-xl bg-purple-950/20 border border-purple-800/40 space-y-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Layers className="w-4 h-4 text-purple-400" />
            <h4 className="text-xs font-semibold text-purple-200 uppercase tracking-wider">
              Project Mesh Networks
            </h4>
          </div>
          <Badge variant="outline" className="border-purple-700 text-purple-300 text-[10px]">
            {projectNetworks.length} Networks
          </Badge>
        </div>
        <p className="text-xs text-zinc-400">
          Cross-service DNS resolution for databases, workers, and web microservices within a project.
        </p>
        <div className="pt-2 border-t border-purple-900/40 flex items-center justify-between text-xs text-zinc-300">
          <span className="font-mono text-[11px] text-zinc-400">Default Subnet: 172.18.0.0/16</span>
          <span className="text-purple-400 font-mono">Bridge / DNS</span>
        </div>
      </div>

      {/* 3. Stack Isolated Card */}
      <div className="p-4 rounded-xl bg-zinc-900/60 border border-zinc-800 space-y-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Network className="w-4 h-4 text-amber-400" />
            <h4 className="text-xs font-semibold text-zinc-200 uppercase tracking-wider">
              Stack Isolation
            </h4>
          </div>
          <Badge variant="outline" className="border-zinc-700 text-zinc-300 text-[10px]">
            {stackNetworks.length} Isolated
          </Badge>
        </div>
        <p className="text-xs text-zinc-400">
          Dedicated, isolated subnets created dynamically for multi-container Compose stacks.
        </p>
        <div className="pt-2 border-t border-zinc-800/80 flex items-center justify-between text-xs text-zinc-300">
          <span className="font-mono text-[11px] text-zinc-400">Zero Ingress Bleed</span>
          <span className="text-amber-400 font-mono">Private Mesh</span>
        </div>
      </div>
    </div>
  );
}
