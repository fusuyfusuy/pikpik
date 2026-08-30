import React, { useState } from 'react';
import {
  LayoutDashboard,
  Box,
  Layers,
  Database as DbIcon,
  Server,
  Globe,
  Archive,
  HardDrive,
  Cpu,
  Key,
  LogOut,
  Menu,
  X,
  Zap,
  Activity,
} from 'lucide-react';
import { Badge } from './ui/Badge';
import { User } from '../lib/types';
import { cn } from '../lib/utils';
import { SSEConnectionStatus } from '../hooks/useSSE';

export interface LayoutProps {
  currentView: string;
  onNavigate: (view: string) => void;
  user: User | null;
  onLogout: () => void;
  sseStatus?: SSEConnectionStatus;
  children: React.ReactNode;
}

interface NavItem {
  id: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  badge?: string;
}

const NAV_ITEMS: NavItem[] = [
  { id: 'dashboard', label: 'Overview', icon: LayoutDashboard },
  { id: 'apps', label: 'Applications', icon: Box },
  { id: 'stacks', label: 'Compose Stacks', icon: Layers },
  { id: 'databases', label: 'Databases', icon: DbIcon },
  { id: 'nodes', label: 'Swarm Nodes', icon: Server },
  { id: 'ingress', label: 'Ingress & TLS', icon: Globe },
  { id: 'backups', label: 'Backups & S3', icon: Archive },
  { id: 'registry', label: 'Container Registry', icon: HardDrive },
  { id: 'system', label: 'System & Disk', icon: Cpu },
  { id: 'settings', label: 'API Tokens & Auth', icon: Key },
];

export function Layout({
  currentView,
  onNavigate,
  user,
  onLogout,
  sseStatus = 'connected',
  children,
}: LayoutProps) {
  const [isMobileNavOpen, setIsMobileNavOpen] = useState(false);

  return (
    <div className="min-h-screen bg-[#09090b] text-zinc-100 flex flex-col antialiased font-sans">
      {/* Top Navbar */}
      <header className="sticky top-0 z-40 h-14 border-b border-zinc-800/80 bg-zinc-950/80 backdrop-blur-md px-4 sm:px-6 flex items-center justify-between">
        <div className="flex items-center gap-3 sm:gap-6">
          <button
            onClick={() => setIsMobileNavOpen(!isMobileNavOpen)}
            className="md:hidden p-1.5 text-zinc-400 hover:text-zinc-100 hover:bg-zinc-800 rounded-lg"
          >
            {isMobileNavOpen ? <X className="h-5 w-5" /> : <Menu className="h-5 w-5" />}
          </button>

          {/* Logo & Version */}
          <div
            onClick={() => onNavigate('dashboard')}
            className="flex items-center gap-2.5 cursor-pointer group select-none"
          >
            <div className="h-8 w-8 rounded-lg bg-gradient-to-tr from-cyan-600 to-cyan-400 flex items-center justify-center shadow-md shadow-cyan-500/20 group-hover:scale-105 transition-transform">
              <Zap className="h-4 w-4 text-zinc-950 fill-zinc-950" />
            </div>
            <div className="flex items-center gap-2">
              <span className="font-bold tracking-tight text-base text-zinc-100 font-mono">
                pikpik
              </span>
              <span className="px-1.5 py-0.5 text-[10px] font-medium bg-zinc-800 border border-zinc-700/60 rounded text-cyan-300">
                v0.2
              </span>
            </div>
          </div>

          <div className="hidden sm:block h-4 w-px bg-zinc-800" />

          {/* Active Cluster Context Indicator */}
          <div className="hidden sm:flex items-center gap-2 text-xs text-zinc-400">
            <span className="text-zinc-500">Cluster:</span>
            <span className="font-mono text-zinc-200 font-medium bg-zinc-900 px-2 py-0.5 rounded border border-zinc-800">
              pikpik-swarm
            </span>
          </div>
        </div>

        {/* Right side status & user info */}
        <div className="flex items-center gap-3 sm:gap-4">
          {/* Real-time SSE / Cluster Status Pill */}
          <div className="flex items-center gap-2 bg-zinc-900/90 border border-zinc-800 px-2.5 py-1 rounded-full text-xs">
            <Activity className="h-3.5 w-3.5 text-cyan-400" />
            <Badge
              variant={
                sseStatus === 'connected'
                  ? 'success'
                  : sseStatus === 'connecting'
                  ? 'warning'
                  : 'error'
              }
              dot
              pulse={sseStatus === 'connected'}
              className="bg-transparent border-0 px-0 py-0 text-[11px]"
            >
              {sseStatus === 'connected'
                ? 'Live'
                : sseStatus === 'connecting'
                ? 'Connecting...'
                : 'Offline'}
            </Badge>
          </div>

          {/* User & Role */}
          {user && (
            <div className="flex items-center gap-3">
              <div className="hidden lg:flex flex-col items-end">
                <span className="text-xs font-medium text-zinc-200">{user.email}</span>
                <span className="text-[10px] text-cyan-400 capitalize font-mono">{user.role}</span>
              </div>

              <button
                onClick={onLogout}
                title="Sign Out"
                className="p-1.5 text-zinc-400 hover:text-rose-400 hover:bg-zinc-800/80 rounded-lg transition-colors"
              >
                <LogOut className="h-4 w-4" />
              </button>
            </div>
          )}
        </div>
      </header>

      {/* Main Layout Body */}
      <div className="flex-1 flex overflow-hidden">
        {/* Desktop Sidebar Navigation */}
        <aside className="hidden md:flex w-60 flex-col border-r border-zinc-800/80 bg-zinc-950/50 p-3 select-none justify-between">
          <nav className="space-y-1">
            <div className="px-3 py-2 text-[10px] font-semibold uppercase tracking-wider text-zinc-500">
              Platform & Swarm
            </div>
            {NAV_ITEMS.map((item) => {
              const Icon = item.icon;
              const isActive = currentView === item.id;
              return (
                <button
                  key={item.id}
                  onClick={() => onNavigate(item.id)}
                  className={cn(
                    'w-full flex items-center justify-between px-3 py-2 rounded-lg text-xs font-medium transition-all',
                    isActive
                      ? 'bg-zinc-800/90 text-cyan-300 font-semibold shadow-sm border border-zinc-700/50'
                      : 'text-zinc-400 hover:text-zinc-200 hover:bg-zinc-900/60'
                  )}
                >
                  <div className="flex items-center gap-2.5">
                    <Icon className={cn('h-4 w-4', isActive ? 'text-cyan-400' : 'text-zinc-500')} />
                    <span>{item.label}</span>
                  </div>
                  {item.badge && (
                    <span className="text-[10px] bg-zinc-800 px-1.5 py-0.5 rounded text-zinc-400">
                      {item.badge}
                    </span>
                  )}
                </button>
              );
            })}
          </nav>

          {/* Sidebar Footer */}
          <div className="pt-4 border-t border-zinc-800/60 px-3">
            <div className="flex items-center justify-between text-[11px] text-zinc-500 font-mono">
              <span>Control Plane</span>
              <span className="text-zinc-400">v0.2-unified</span>
            </div>
          </div>
        </aside>

        {/* Mobile Navigation Drawer */}
        {isMobileNavOpen && (
          <div className="fixed inset-0 z-50 md:hidden flex">
            <div
              className="fixed inset-0 bg-black/70 backdrop-blur-sm"
              onClick={() => setIsMobileNavOpen(false)}
            />
            <div className="relative w-64 max-w-[80vw] bg-zinc-900 border-r border-zinc-800 p-4 flex flex-col justify-between z-10">
              <nav className="space-y-1">
                <div className="flex items-center justify-between pb-4 border-b border-zinc-800 mb-2">
                  <div className="flex items-center gap-2">
                    <div className="h-6 w-6 rounded bg-cyan-500 flex items-center justify-center">
                      <Zap className="h-3.5 w-3.5 text-zinc-950" />
                    </div>
                    <span className="font-bold font-mono text-sm">pikpik</span>
                  </div>
                  <button
                    onClick={() => setIsMobileNavOpen(false)}
                    className="p-1 text-zinc-400 hover:text-zinc-100"
                  >
                    <X className="h-5 w-5" />
                  </button>
                </div>
                {NAV_ITEMS.map((item) => {
                  const Icon = item.icon;
                  const isActive = currentView === item.id;
                  return (
                    <button
                      key={item.id}
                      onClick={() => {
                        onNavigate(item.id);
                        setIsMobileNavOpen(false);
                      }}
                      className={cn(
                        'w-full flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium',
                        isActive
                          ? 'bg-zinc-800 text-cyan-300 font-semibold'
                          : 'text-zinc-400 hover:text-zinc-200'
                      )}
                    >
                      <Icon className={cn('h-4 w-4', isActive ? 'text-cyan-400' : 'text-zinc-500')} />
                      <span>{item.label}</span>
                    </button>
                  );
                })}
              </nav>

              <div className="pt-4 border-t border-zinc-800">
                <button
                  onClick={onLogout}
                  className="w-full flex items-center gap-2 px-3 py-2 text-sm text-rose-400 hover:bg-rose-950/30 rounded-lg"
                >
                  <LogOut className="h-4 w-4" />
                  <span>Sign Out</span>
                </button>
              </div>
            </div>
          </div>
        )}

        {/* Main Content Area */}
        <main className="flex-1 overflow-y-auto bg-zinc-950 p-4 sm:p-6 lg:p-8">
          <div className="max-w-7xl mx-auto">{children}</div>
        </main>
      </div>
    </div>
  );
}
