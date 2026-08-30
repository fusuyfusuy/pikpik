import { Globe, ArrowRight, ShieldCheck, Server, Layers } from 'lucide-react';
import { DomainBinding, App } from '../../../../lib/types';
import { Badge } from '../../../../components/ui/Badge';

interface RouteGraphProps {
  domains: DomainBinding[];
  apps: App[];
  onSelectApp?: (appId: string) => void;
}

export function RouteGraph({
  domains,
  apps,
  onSelectApp,
}: RouteGraphProps) {
  const getAppName = (appId: string) => {
    const app = apps.find((a) => a.id === appId);
    return app ? app.name : appId || 'Default Service';
  };

  const getAppPort = (appId: string) => {
    const app = apps.find((a) => a.id === appId);
    return app?.container_port || 80;
  };

  if (!domains || domains.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center p-8 bg-zinc-900/40 border border-zinc-800/80 rounded-xl text-center">
        <div className="p-3 bg-zinc-800/60 rounded-xl text-zinc-400 mb-3">
          <Globe className="w-8 h-8 opacity-40 text-cyan-400" />
        </div>
        <h4 className="text-sm font-medium text-zinc-300">No active dynamic routes</h4>
        <p className="text-xs text-zinc-500 max-w-sm mt-1">
          Create a dynamic route to see the visual traffic routing topology and live upstream flow graphs.
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Layers className="w-4 h-4 text-cyan-400" />
          <span className="text-xs font-semibold uppercase tracking-wider text-zinc-400">
            Route-to-Upstream Visual Traffic Graph
          </span>
        </div>
        <span className="text-xs text-zinc-500">
          Sub-15ms Route Ingress Reconciler
        </span>
      </div>

      <div className="grid grid-cols-1 gap-4">
        {domains.map((binding) => {
          const primaryName = getAppName(binding.app_id);
          const port = getAppPort(binding.app_id);
          const upstreamDial = `${primaryName.toLowerCase().replace(/\s+/g, '-')}:${port}`;

          return (
            <div
              key={binding.id}
              className="bg-zinc-900/70 border border-zinc-800/80 hover:border-zinc-700/80 transition-all rounded-xl p-4 shadow-sm"
            >
              <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
                {/* Node 1: Ingress Edge / Domain */}
                <div className="flex items-center gap-3 min-w-[220px]">
                  <div className="p-2.5 rounded-lg bg-cyan-950/40 border border-cyan-800/30 text-cyan-400">
                    <Globe className="w-5 h-5" />
                  </div>
                  <div>
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-semibold text-zinc-100 font-mono">
                        {binding.domain}
                      </span>
                      {binding.auto_tls && (
                        <span title="Automated TLS Provisioned">
                          <ShieldCheck className="w-4 h-4 text-emerald-400 inline" />
                        </span>
                      )}
                    </div>
                    <div className="flex items-center gap-2 mt-0.5">
                      <Badge variant="outline" className="text-[10px] py-0 px-1.5 border-zinc-700 text-zinc-400">
                        Caddy Edge
                      </Badge>
                      <span className="text-[10px] text-zinc-500">
                        {binding.status === 'active' ? '🟢 Active' : '🟡 ' + binding.status}
                      </span>
                    </div>
                  </div>
                </div>

                {/* Arrow / Connector */}
                <div className="hidden md:flex items-center gap-2 text-zinc-500 px-2">
                  <div className="h-[1px] w-8 bg-zinc-700"></div>
                  <div className="px-2 py-0.5 rounded bg-zinc-800/80 border border-zinc-700/50 text-[11px] font-mono text-zinc-300">
                    /*
                  </div>
                  <ArrowRight className="w-4 h-4 text-zinc-400" />
                </div>

                {/* Node 2: Direct 1:1 Upstream Target */}
                <div className="flex-1">
                  <div
                    onClick={() => onSelectApp && onSelectApp(binding.app_id)}
                    className="flex items-center justify-between p-2.5 rounded-lg bg-zinc-800/60 border border-zinc-700/50 hover:bg-zinc-800 transition-colors cursor-pointer"
                  >
                    <div className="flex items-center gap-2.5">
                      <Server className="w-4 h-4 text-cyan-400" />
                      <div>
                        <div className="flex items-center gap-2">
                          <span className="text-xs font-medium text-zinc-200">
                            {primaryName}
                          </span>
                          <span className="text-[10px] text-zinc-400 font-mono">
                            ({upstreamDial})
                          </span>
                        </div>
                        <span className="text-[10px] text-emerald-400 font-medium">
                          ● 1:1 Direct Upstream
                        </span>
                      </div>
                    </div>
                    <div className="flex items-center gap-2">
                      <Badge variant="default" className="bg-cyan-900/50 text-cyan-300 border-cyan-700 text-xs font-mono">
                        100% Direct
                      </Badge>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
