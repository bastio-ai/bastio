import React from "react";

type Props = { children: React.ReactNode };
type State = { error: Error | null };

/**
 * Top-level error boundary. Catches rendering errors from the router tree and
 * shows a minimal recovery UI with reload + retry. Uses tokens only.
 */
export class ErrorBoundary extends React.Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: React.ErrorInfo) {
    // Surface to console in dev; swap for a real telemetry pipe in prod.
    console.error("Dashboard error boundary caught:", error, info);
  }

  handleRetry = () => this.setState({ error: null });
  handleReload = () => window.location.reload();

  render() {
    if (!this.state.error) return this.props.children;

    return (
      <div className="min-h-screen flex items-center justify-center bg-background p-6">
        <div className="surface-card max-w-[520px] w-full p-6">
          <p className="text-[10px] font-medium uppercase tracking-[0.1em] text-danger">
            Something broke
          </p>
          <h1 className="mt-2 text-[18px] font-semibold text-text-primary">
            The dashboard hit an unexpected error.
          </h1>
          <p className="mt-2 text-[12px] text-text-secondary">
            The page stopped rendering mid-flight. Try retrying — if the error repeats,
            reload the page. The message is logged to the browser console.
          </p>
          <pre className="mt-4 p-3 rounded-md bg-surface-2 border border-border-subtle font-mono text-[11px] text-text-secondary overflow-auto max-h-[160px]">
            {this.state.error.message}
          </pre>
          <div className="mt-4 flex gap-2">
            <button
              type="button"
              onClick={this.handleRetry}
              className="inline-flex items-center px-3 py-1.5 rounded-md bg-primary text-primary-foreground text-[12px] font-medium hover:opacity-90 transition-opacity"
            >
              Retry
            </button>
            <button
              type="button"
              onClick={this.handleReload}
              className="inline-flex items-center px-3 py-1.5 rounded-md border border-border-default text-text-secondary text-[12px] hover:text-text-primary transition-colors"
            >
              Reload page
            </button>
          </div>
        </div>
      </div>
    );
  }
}
