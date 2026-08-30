import React from 'react';
import { cn } from '../../lib/utils';

export interface TabItem {
  id: string;
  label: React.ReactNode;
  icon?: React.ReactNode;
  count?: number;
}

export interface TabsProps {
  tabs: TabItem[];
  activeTab: string;
  onChange: (tabId: string) => void;
  className?: string;
  variant?: 'pills' | 'underline';
}

export function Tabs({
  tabs,
  activeTab,
  onChange,
  className,
  variant = 'underline',
}: TabsProps) {
  if (variant === 'pills') {
    return (
      <div
        className={cn(
          'inline-flex p-1 bg-zinc-900 border border-zinc-800 rounded-lg gap-1',
          className
        )}
      >
        {tabs.map((tab) => {
          const isActive = tab.id === activeTab;
          return (
            <button
              key={tab.id}
              onClick={() => onChange(tab.id)}
              className={cn(
                'flex items-center gap-2 px-3 py-1.5 rounded-md text-xs font-medium transition-all select-none',
                isActive
                  ? 'bg-zinc-800 text-zinc-100 shadow-sm'
                  : 'text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800/50'
              )}
            >
              {tab.icon}
              {tab.label}
              {tab.count !== undefined && (
                <span
                  className={cn(
                    'px-1.5 py-0.2 rounded text-[10px]',
                    isActive ? 'bg-zinc-700 text-zinc-200' : 'bg-zinc-800 text-zinc-400'
                  )}
                >
                  {tab.count}
                </span>
              )}
            </button>
          );
        })}
      </div>
    );
  }

  return (
    <div className={cn('border-b border-zinc-800/80 flex items-center gap-6', className)}>
      {tabs.map((tab) => {
        const isActive = tab.id === activeTab;
        return (
          <button
            key={tab.id}
            onClick={() => onChange(tab.id)}
            className={cn(
              'relative flex items-center gap-2 pb-3 pt-1 text-sm font-medium transition-colors select-none',
              isActive
                ? 'text-cyan-400 border-b-2 border-cyan-400 -mb-px'
                : 'text-zinc-400 hover:text-zinc-200'
            )}
          >
            {tab.icon}
            {tab.label}
            {tab.count !== undefined && (
              <span
                className={cn(
                  'px-1.5 py-0.5 rounded text-xs',
                  isActive ? 'bg-cyan-950/60 text-cyan-300' : 'bg-zinc-800 text-zinc-400'
                )}
              >
                {tab.count}
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
}
