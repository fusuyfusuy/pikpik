import React from 'react';
import { AlertCircle, RefreshCw } from 'lucide-react';
import { Button } from './Button';
import { cn } from '../../lib/utils';

export interface QueryErrorAlertProps extends React.HTMLAttributes<HTMLDivElement> {
  title?: string;
  error?: Error | unknown;
  onRetry?: () => void;
  inline?: boolean;
}

export function QueryErrorAlert({
  title = 'Failed to load data',
  error,
  onRetry,
  inline = false,
  className,
  ...props
}: QueryErrorAlertProps) {
  const message =
    error instanceof Error
      ? error.message
      : typeof error === 'string'
      ? error
      : 'An unexpected error occurred while communicating with the control plane.';

  return (
    <div
      className={cn(
        'rounded-xl border border-rose-800/60 bg-rose-950/40 text-rose-200 shadow-lg',
        inline ? 'p-3 flex items-center justify-between gap-3 text-xs' : 'p-4 flex flex-col sm:flex-row sm:items-center justify-between gap-3',
        className
      )}
      {...props}
    >
      <div className="flex items-start gap-3 min-w-0">
        <AlertCircle className={cn('text-rose-400 shrink-0 mt-0.5', inline ? 'h-4 w-4' : 'h-5 w-5')} />
        <div className="min-w-0 flex-1">
          <h4 className={cn('font-semibold text-rose-100', inline ? 'text-xs' : 'text-sm')}>
            {title}
          </h4>
          <p className={cn('text-rose-300/80 font-mono break-words mt-0.5', inline ? 'text-[11px]' : 'text-xs')}>
            {message}
          </p>
        </div>
      </div>
      {onRetry && (
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={onRetry}
          leftIcon={<RefreshCw className="h-3.5 w-3.5 text-rose-300" />}
          className="border-rose-700/50 hover:bg-rose-900/40 text-rose-200 shrink-0 self-start sm:self-center cursor-pointer"
        >
          Retry
        </Button>
      )}
    </div>
  );
}

export default QueryErrorAlert;
