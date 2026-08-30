import React from 'react';
import { cn } from '../../lib/utils';

export interface BadgeProps extends React.HTMLAttributes<HTMLSpanElement> {
  variant?: 'default' | 'success' | 'warning' | 'error' | 'info' | 'purple' | 'outline';
  dot?: boolean;
  pulse?: boolean;
}

export function Badge({
  className,
  variant = 'default',
  dot = false,
  pulse = false,
  children,
  ...props
}: BadgeProps) {
  const variants = {
    default: 'bg-zinc-800 text-zinc-300 border-zinc-700/60',
    success: 'bg-emerald-950/40 text-emerald-400 border-emerald-800/40',
    warning: 'bg-amber-950/40 text-amber-400 border-amber-800/40',
    error: 'bg-rose-950/40 text-rose-400 border-rose-800/40',
    info: 'bg-cyan-950/40 text-cyan-300 border-cyan-800/40',
    purple: 'bg-purple-950/40 text-purple-300 border-purple-800/40',
    outline: 'bg-transparent text-zinc-400 border-zinc-700',
  };

  const dotColors = {
    default: 'bg-zinc-400',
    success: 'bg-emerald-400',
    warning: 'bg-amber-400',
    error: 'bg-rose-400',
    info: 'bg-cyan-400',
    purple: 'bg-purple-400',
    outline: 'bg-zinc-400',
  };

  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 px-2 py-0.5 rounded-md text-xs font-medium border tracking-wide transition-colors',
        variants[variant],
        className
      )}
      {...props}
    >
      {dot && (
        <span className="relative flex h-1.5 w-1.5">
          {pulse && (
            <span
              className={cn(
                'animate-ping absolute inline-flex h-full w-full rounded-full opacity-75',
                dotColors[variant]
              )}
            />
          )}
          <span
            className={cn('relative inline-flex rounded-full h-1.5 w-1.5', dotColors[variant])}
          />
        </span>
      )}
      {children}
    </span>
  );
}
