import { Component, type ErrorInfo, type ReactNode } from 'react';
import { Button } from './ui/Button';
import { Logo } from './Logo';

type Props = {
  children: ReactNode;
};

type State = {
  error: Error | null;
};

export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // Keep this noisy in the console: when the UI goes blank, this is the fastest signal.
    console.error('RailPush UI crashed', error, info);
  }

  render() {
    const { error } = this.state;
    if (!error) return this.props.children;

    return (
      <div className="min-h-screen bg-surface-primary text-content-primary flex items-center justify-center px-4 py-10">
        <div className="w-full max-w-[720px] bg-surface-secondary border border-border-default rounded-lg p-6">
          <div className="flex items-center justify-between gap-4">
            <Logo size={32} />
            <div className="flex items-center gap-2">
              <Button variant="secondary" onClick={() => window.location.assign('/')}>
                Home
              </Button>
              <Button onClick={() => window.location.reload()}>Reload</Button>
            </div>
          </div>

          <div className="mt-6">
            <div className="text-sm font-semibold">Something went wrong</div>
            <div className="text-sm text-content-secondary mt-1">
              The dashboard hit an unexpected error. Reload usually fixes it. If it keeps happening, this page contains the error
              message for debugging.
            </div>
          </div>

          <pre className="mt-4 text-xs overflow-auto max-h-[280px] bg-surface-tertiary border border-border-subtle rounded-md p-3 font-mono text-content-secondary">
            {String(error?.message || error)}
          </pre>
        </div>
      </div>
    );
  }
}
