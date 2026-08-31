import { Component, ErrorInfo, ReactNode } from 'react';
import { AlertTriangle, RefreshCw, Home } from 'lucide-react';
import { Button } from './ui/Button';

export interface ErrorBoundaryProps {
  children: ReactNode;
  fallback?: ReactNode | ((error: Error, reset: () => void) => ReactNode);
  onError?: (error: Error, errorInfo: ErrorInfo) => void;
}

interface ErrorBoundaryState {
  hasError: boolean;
  error: Error | null;
  errorInfo: ErrorInfo | null;
  showDetails: boolean;
}

export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  constructor(props: ErrorBoundaryProps) {
    super(props);
    this.state = {
      hasError: false,
      error: null,
      errorInfo: null,
      showDetails: false,
    };
  }

  static getDerivedStateFromError(error: Error): Partial<ErrorBoundaryState> {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, errorInfo: ErrorInfo): void {
    this.setState({ errorInfo });
    if (this.props.onError) {
      this.props.onError(error, errorInfo);
    }
    console.error('[ErrorBoundary] Caught render exception:', error, errorInfo);
  }

  handleReset = (): void => {
    this.setState({
      hasError: false,
      error: null,
      errorInfo: null,
      showDetails: false,
    });
  };

  handleReload = (): void => {
    window.location.reload();
  };

  handleHome = (): void => {
    window.location.hash = '#/dashboard';
    this.handleReset();
  };

  render(): ReactNode {
    if (this.state.hasError) {
      const { fallback } = this.props;
      if (fallback) {
        if (typeof fallback === 'function') {
          return fallback(this.state.error || new Error('Unknown error'), this.handleReset);
        }
        return fallback;
      }

      const errorMessage = this.state.error?.message || 'An unexpected rendering error occurred.';
      const errorStack = this.state.error?.stack;
      const componentStack = this.state.errorInfo?.componentStack;

      return (
        <div className="min-h-[400px] w-full text-zinc-100 flex items-center justify-center p-6 select-text">
          <div className="max-w-xl w-full bg-zinc-950/80 border border-zinc-800 rounded-xl p-6 shadow-2xl backdrop-blur-md space-y-6">
            <div className="flex items-start gap-4">
              <div className="p-3 bg-rose-950/50 border border-rose-800/60 rounded-lg text-rose-400 shrink-0">
                <AlertTriangle className="h-6 w-6" />
              </div>
              <div className="space-y-1 flex-1">
                <h2 className="text-lg font-bold text-zinc-100 tracking-tight">
                  Application Rendering Error
                </h2>
                <p className="text-xs text-zinc-400 leading-relaxed">
                  A component crashed while rendering the UI. The control plane caught this exception gracefully.
                </p>
              </div>
            </div>

            <div className="p-3.5 bg-zinc-900/90 rounded-lg border border-zinc-800/80 font-mono text-xs text-rose-300 break-all leading-relaxed">
              {errorMessage}
            </div>

            {(errorStack || componentStack) && (
              <div className="space-y-2">
                <button
                  type="button"
                  onClick={() => this.setState((prev) => ({ showDetails: !prev.showDetails }))}
                  className="text-[11px] font-mono text-cyan-400 hover:text-cyan-300 underline underline-offset-2 transition-colors cursor-pointer"
                >
                  {this.state.showDetails ? 'Hide technical trace' : 'Show technical trace'}
                </button>

                {this.state.showDetails && (
                  <div className="p-3 bg-black/70 rounded-lg border border-zinc-800/80 max-h-60 overflow-y-auto font-mono text-[11px] text-zinc-400 leading-normal whitespace-pre-wrap">
                    {errorStack && (
                      <div className="mb-2">
                        <span className="text-zinc-500 font-semibold block">Stack Trace:</span>
                        {errorStack}
                      </div>
                    )}
                    {componentStack && (
                      <div>
                        <span className="text-zinc-500 font-semibold block">Component Stack:</span>
                        {componentStack}
                      </div>
                    )}
                  </div>
                )}
              </div>
            )}

            <div className="flex flex-wrap items-center justify-end gap-2.5 pt-4 border-t border-zinc-800/80">
              <Button
                variant="outline"
                size="sm"
                onClick={this.handleHome}
                leftIcon={<Home className="h-3.5 w-3.5" />}
              >
                Dashboard
              </Button>
              <Button
                variant="secondary"
                size="sm"
                onClick={this.handleReset}
                leftIcon={<RefreshCw className="h-3.5 w-3.5" />}
              >
                Try Again
              </Button>
              <Button
                variant="primary"
                size="sm"
                onClick={this.handleReload}
                leftIcon={<RefreshCw className="h-3.5 w-3.5" />}
              >
                Reload Page
              </Button>
            </div>
          </div>
        </div>
      );
    }

    return this.props.children;
  }
}

export default ErrorBoundary;
