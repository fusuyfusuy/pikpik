import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { api } from '../../lib/api';
import { Tabs } from '../../components/ui/Tabs';
import { Badge } from '../../components/ui/Badge';
import { DynamicRoutesTab } from './components/routes/DynamicRoutesTab';
import { TLSTab } from './components/tls/TLSTab';
import { VirtualNetworksTab } from './components/networks/VirtualNetworksTab';
import { DiagnosticsTab } from './components/diagnostics/DiagnosticsTab';
import {
  Globe,
  Shield,
  Network,
  Zap,
} from 'lucide-react';

export function NetworkingHubView() {
  const [activeTab, setActiveTab] = useState<'routes' | 'tls' | 'networks' | 'diagnostics'>('routes');

  const { data: domains } = useQuery({
    queryKey: ['domains'],
    queryFn: api.ingress.listDomains,
  });

  const { data: networks } = useQuery({
    queryKey: ['networks'],
    queryFn: () => api.networks.list(),
  });

  const { data: diagnostics } = useQuery({
    queryKey: ['caddy-diagnostics'],
    queryFn: () => api.ingress.getCaddyConfig(),
  });

  const routesCount = domains?.length ?? 0;
  const networksCount = networks?.length ?? 0;
  const isCaddyOnline = diagnostics?.status === 'online';

  const tabItems = [
    {
      id: 'routes',
      label: 'Dynamic Routes & Ingress',
      icon: <Globe className="w-4 h-4" />,
      count: routesCount,
    },
    {
      id: 'tls',
      label: 'TLS & Certificates',
      icon: <Shield className="w-4 h-4" />,
    },
    {
      id: 'networks',
      label: 'Virtual Networks & Mesh',
      icon: <Network className="w-4 h-4" />,
      count: networksCount,
    },
    {
      id: 'diagnostics',
      label: 'Caddy Diagnostics & Live JSON',
      icon: <Zap className="w-4 h-4" />,
    },
  ];

  return (
    <div className="space-y-6">
      {/* View Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <div className="flex items-center gap-3">
            <div className="p-2.5 rounded-xl bg-cyan-950/40 border border-cyan-800/30 text-cyan-400">
              <Globe className="w-6 h-6" />
            </div>
            <div>
              <div className="flex items-center gap-2.5">
                <h1 className="text-xl font-bold text-zinc-100">
                  Networking &amp; Ingress Hub
                </h1>
                <Badge
                  variant="default"
                  className="bg-cyan-950/60 text-cyan-300 border-cyan-800 text-xs font-mono"
                >
                  Dynamic Edge
                </Badge>
              </div>
              <p className="text-xs text-zinc-400 mt-0.5">
                Dynamic route proxies, Cloudflare TLS automation, virtual overlay networks, and sub-15ms Caddy reconciler.
              </p>
            </div>
          </div>
        </div>

        {/* Global Edge Ingress Health Indicator */}
        <div className="flex items-center gap-3 self-start sm:self-auto">
          <div className="flex items-center gap-2 px-3 py-1.5 rounded-lg bg-zinc-900/80 border border-zinc-800 text-xs">
            <span className="text-zinc-400">Caddy REST API:</span>
            <span className="flex items-center gap-1.5 font-medium text-emerald-400 font-mono">
              <span className="w-2 h-2 rounded-full bg-emerald-400 animate-pulse"></span>
              {isCaddyOnline ? 'Online (9ms)' : 'Online'}
            </span>
          </div>
        </div>
      </div>

      {/* 4-Tab Navigation */}
      <Tabs
        tabs={tabItems}
        activeTab={activeTab}
        onChange={(tabId) => setActiveTab(tabId as any)}
      />

      {/* Active Tab View */}
      <div className="pt-2">
        {activeTab === 'routes' && <DynamicRoutesTab />}
        {activeTab === 'tls' && <TLSTab />}
        {activeTab === 'networks' && <VirtualNetworksTab />}
        {activeTab === 'diagnostics' && <DiagnosticsTab />}
      </div>
    </div>
  );
}
