import React, { useState, useEffect, useRef } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
  Search,
  LayoutDashboard,
  Folder,
  Box,
  Layers,
  Database as DbIcon,
  Server,
  Globe,
  Archive,
  HardDrive,
  Cpu,
  Key,
  Store,
  Plus,
  ArrowRight,
  Zap,
} from 'lucide-react';
import { api } from '../lib/api';
import { cn } from '../lib/utils';

export interface CommandPaletteProps {
  isOpen: boolean;
  onClose: () => void;
  onNavigate: (view: string) => void;
}

interface CommandItem {
  id: string;
  title: string;
  category: 'Navigation' | 'Workloads' | 'Infrastructure' | 'Actions';
  subtitle?: string;
  icon: React.ComponentType<{ className?: string }>;
  action: () => void;
}

export function CommandPalette({ isOpen, onClose, onNavigate }: CommandPaletteProps) {
  const [query, setQuery] = useState('');
  const [selectedIndex, setSelectedIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);

  // Fetch dynamic entities for live search
  const { data: projects } = useQuery({
    queryKey: ['projects'],
    queryFn: () => api.projects.list(),
    enabled: isOpen,
  });

  const { data: apps } = useQuery({
    queryKey: ['apps'],
    queryFn: () => api.apps.list(),
    enabled: isOpen,
  });

  const { data: databases } = useQuery({
    queryKey: ['databases'],
    queryFn: api.databases.list,
    enabled: isOpen,
  });

  const { data: nodes } = useQuery({
    queryKey: ['nodes'],
    queryFn: api.nodes.list,
    enabled: isOpen,
  });

  // Base navigation commands
  const baseCommands: CommandItem[] = [
    {
      id: 'nav-dashboard',
      title: 'Cluster Overview',
      category: 'Navigation',
      subtitle: 'Real-time telemetry, resource metrics & aggregate health',
      icon: LayoutDashboard,
      action: () => onNavigate('dashboard'),
    },
    {
      id: 'nav-projects',
      title: 'Project Workspaces',
      category: 'Navigation',
      subtitle: 'Group applications, databases, stacks, and domains by environment',
      icon: Folder,
      action: () => onNavigate('projects'),
    },
    {
      id: 'nav-marketplace',
      title: '1-Click Marketplace',
      category: 'Navigation',
      subtitle: 'Deploy pre-packaged open-source templates & recipes',
      icon: Store,
      action: () => onNavigate('marketplace'),
    },
    {
      id: 'nav-apps',
      title: 'Applications',
      category: 'Navigation',
      subtitle: 'Manage standalone container workloads & microservices',
      icon: Box,
      action: () => onNavigate('apps'),
    },
    {
      id: 'nav-stacks',
      title: 'Compose Stacks',
      category: 'Navigation',
      subtitle: 'Multi-service topologies & Docker Compose deployments',
      icon: Layers,
      action: () => onNavigate('stacks'),
    },
    {
      id: 'nav-databases',
      title: 'Managed Databases',
      category: 'Navigation',
      subtitle: 'Postgres, MySQL, Redis, and ClickHouse engines',
      icon: DbIcon,
      action: () => onNavigate('databases'),
    },
    {
      id: 'nav-ingress',
      title: 'Networking & Ingress',
      category: 'Navigation',
      subtitle: 'Dynamic Caddy reverse proxy, Auto-TLS & custom domains',
      icon: Globe,
      action: () => onNavigate('ingress'),
    },
    {
      id: 'nav-nodes',
      title: 'Machine Fleet & Swarm Nodes',
      category: 'Navigation',
      subtitle: 'Host machines, daemon sockets & agent telemetry',
      icon: Server,
      action: () => onNavigate('nodes'),
    },
    {
      id: 'nav-registry',
      title: 'Container Registry',
      category: 'Navigation',
      subtitle: 'Internal OCI registry & image repositories',
      icon: HardDrive,
      action: () => onNavigate('registry'),
    },
    {
      id: 'nav-backups',
      title: 'Backups & Disaster Recovery',
      category: 'Navigation',
      subtitle: 'S3/R2 snapshot schedules & streaming restores',
      icon: Archive,
      action: () => onNavigate('backups'),
    },
    {
      id: 'nav-system',
      title: 'System Health & Disk Usage',
      category: 'Navigation',
      subtitle: 'Host specs, Docker cache pruning & engine logs',
      icon: Cpu,
      action: () => onNavigate('system'),
    },
    {
      id: 'nav-settings',
      title: 'Access Tokens & Auth Security',
      category: 'Navigation',
      subtitle: 'API keys, user sessions & environment variable vault',
      icon: Key,
      action: () => onNavigate('settings'),
    },
    // Quick Actions
    {
      id: 'action-new-app',
      title: 'Deploy New Application',
      category: 'Actions',
      subtitle: 'Launch a container from Docker Hub, GitHub, or raw image',
      icon: Plus,
      action: () => onNavigate('apps'),
    },
    {
      id: 'action-new-db',
      title: 'Provision Managed Database',
      category: 'Actions',
      subtitle: 'Spin up Postgres, Redis, MySQL, or ClickHouse',
      icon: Plus,
      action: () => onNavigate('databases'),
    },
  ];

  // Dynamic entity commands
  const dynamicCommands: CommandItem[] = [];

  if (projects && projects.length > 0) {
    projects.forEach((proj) => {
      dynamicCommands.push({
        id: `project-${proj.id}`,
        title: proj.name,
        category: 'Workloads',
        subtitle: `Project Workspace · ${proj.slug || proj.id}`,
        icon: Folder,
        action: () => onNavigate('projects'),
      });
    });
  }

  if (apps && apps.length > 0) {
    apps.forEach((app) => {
      dynamicCommands.push({
        id: `app-${app.id}`,
        title: app.name,
        category: 'Workloads',
        subtitle: `App · ${app.image} · Status: ${app.status}`,
        icon: Box,
        action: () => onNavigate('apps'),
      });
    });
  }

  if (databases && databases.length > 0) {
    databases.forEach((db) => {
      dynamicCommands.push({
        id: `db-${db.id}`,
        title: db.name,
        category: 'Workloads',
        subtitle: `Database · ${db.engine} · Status: ${db.status}`,
        icon: DbIcon,
        action: () => onNavigate('databases'),
      });
    });
  }

  if (nodes && nodes.length > 0) {
    nodes.forEach((node) => {
      dynamicCommands.push({
        id: `node-${node.id}`,
        title: node.hostname || node.id,
        category: 'Infrastructure',
        subtitle: `Node · ${node.role} · Status: ${node.status}`,
        icon: Server,
        action: () => onNavigate('nodes'),
      });
    });
  }

  const allCommands = [...baseCommands, ...dynamicCommands];

  const filteredCommands = allCommands.filter((cmd) => {
    if (!query.trim()) return true;
    const lowerQuery = query.toLowerCase();
    return (
      cmd.title.toLowerCase().includes(lowerQuery) ||
      cmd.category.toLowerCase().includes(lowerQuery) ||
      (cmd.subtitle && cmd.subtitle.toLowerCase().includes(lowerQuery))
    );
  });

  // Focus input when opened
  useEffect(() => {
    if (isOpen) {
      setQuery('');
      setSelectedIndex(0);
      setTimeout(() => inputRef.current?.focus(), 50);
    }
  }, [isOpen]);

  // Keyboard navigation
  useEffect(() => {
    if (!isOpen) return;

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        onClose();
      } else if (e.key === 'ArrowDown') {
        e.preventDefault();
        setSelectedIndex((prev) => (prev < filteredCommands.length - 1 ? prev + 1 : 0));
      } else if (e.key === 'ArrowUp') {
        e.preventDefault();
        setSelectedIndex((prev) => (prev > 0 ? prev - 1 : filteredCommands.length - 1));
      } else if (e.key === 'Enter') {
        e.preventDefault();
        const selected = filteredCommands[selectedIndex];
        if (selected) {
          selected.action();
          onClose();
        }
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, filteredCommands, selectedIndex, onClose]);

  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center pt-20 px-4">
      {/* Backdrop */}
      <div
        className="fixed inset-0 bg-black/75 backdrop-blur-sm transition-opacity"
        onClick={onClose}
      />

      {/* Modal Dialog */}
      <div className="relative w-full max-w-2xl bg-zinc-900 border border-zinc-700/70 rounded-xl shadow-2xl overflow-hidden z-10 animate-in fade-in zoom-in-95 duration-150">
        {/* Search Header */}
        <div className="flex items-center gap-3 px-4 py-3.5 border-b border-zinc-800 bg-zinc-950/60">
          <Search className="h-4 w-4 text-cyan-400 shrink-0" />
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setSelectedIndex(0);
            }}
            placeholder="Type a command, service name, or jump to view..."
            className="w-full bg-transparent text-sm text-zinc-100 placeholder-zinc-500 focus:outline-none font-sans"
          />
          <kbd className="px-1.5 py-0.5 text-[10px] font-mono text-zinc-400 bg-zinc-800 border border-zinc-700 rounded select-none">
            ESC
          </kbd>
        </div>

        {/* Results List */}
        <div className="max-h-96 overflow-y-auto p-2 divide-y divide-zinc-800/40">
          {filteredCommands.length === 0 ? (
            <div className="py-10 text-center text-xs text-zinc-500">
              No matching resources or commands found for &ldquo;<span className="text-zinc-300">{query}</span>&rdquo;
            </div>
          ) : (
            filteredCommands.map((cmd, idx) => {
              const Icon = cmd.icon;
              const isSelected = idx === selectedIndex;
              return (
                <div
                  key={cmd.id}
                  onClick={() => {
                    cmd.action();
                    onClose();
                  }}
                  onMouseEnter={() => setSelectedIndex(idx)}
                  className={cn(
                    'flex items-center justify-between px-3 py-2.5 rounded-lg cursor-pointer transition-colors text-xs select-none',
                    isSelected
                      ? 'bg-zinc-800/90 text-cyan-300 shadow-sm border border-zinc-700/60'
                      : 'text-zinc-300 hover:bg-zinc-800/50'
                  )}
                >
                  <div className="flex items-center gap-3 min-w-0">
                    <div
                      className={cn(
                        'p-1.5 rounded-md shrink-0',
                        isSelected ? 'bg-cyan-950/80 text-cyan-400 border border-cyan-800/40' : 'bg-zinc-800 text-zinc-400'
                      )}
                    >
                      <Icon className="h-4 w-4" />
                    </div>
                    <div className="min-w-0">
                      <div className="font-medium text-zinc-100 flex items-center gap-2 truncate">
                        <span>{cmd.title}</span>
                        <span className="text-[10px] font-normal px-1.5 py-0.2 bg-zinc-800 text-zinc-400 rounded border border-zinc-700/50">
                          {cmd.category}
                        </span>
                      </div>
                      {cmd.subtitle && (
                        <div className="text-[11px] text-zinc-400 truncate mt-0.5">
                          {cmd.subtitle}
                        </div>
                      )}
                    </div>
                  </div>

                  <div className="flex items-center gap-1.5 shrink-0 ml-3">
                    {isSelected && (
                      <span className="flex items-center gap-1 text-[10px] font-mono text-cyan-400">
                        <span>Select</span>
                        <ArrowRight className="h-3 w-3" />
                      </span>
                    )}
                  </div>
                </div>
              );
            })
          )}
        </div>

        {/* Footer Shortcut Bar */}
        <div className="px-4 py-2 border-t border-zinc-800/80 bg-zinc-950/80 flex items-center justify-between text-[11px] text-zinc-400 font-mono">
          <div className="flex items-center gap-3">
            <span className="flex items-center gap-1">
              <kbd className="px-1 py-0.2 bg-zinc-800 rounded border border-zinc-700 text-zinc-300">↑</kbd>
              <kbd className="px-1 py-0.2 bg-zinc-800 rounded border border-zinc-700 text-zinc-300">↓</kbd>
              <span>Navigate</span>
            </span>
            <span className="flex items-center gap-1">
              <kbd className="px-1.5 py-0.2 bg-zinc-800 rounded border border-zinc-700 text-zinc-300">↵</kbd>
              <span>Execute</span>
            </span>
          </div>
          <div className="flex items-center gap-1 text-cyan-400">
            <Zap className="h-3 w-3" />
            <span>pikpik fast-jump</span>
          </div>
        </div>
      </div>
    </div>
  );
}
