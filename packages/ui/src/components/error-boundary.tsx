"use client";

import React from "react";
import { ContentError } from "./content-error";

interface Props {
  children: React.ReactNode;
  fallback?: React.ReactNode;
}

interface State {
  hasError: boolean;
}

/**
 * Global error boundary — catches unhandled render errors.
 * Shows a full-page centered error state matching MemaxLoader's
 * visual weight. Never a floating card in empty space.
 */
export class ErrorBoundary extends React.Component<Props, State> {
  constructor(props: Props) {
    super(props);
    this.state = { hasError: false };
  }

  static getDerivedStateFromError(): State {
    return { hasError: true };
  }

  componentDidCatch(error: Error, info: React.ErrorInfo) {
    console.error("[memax] render error:", error, info.componentStack);
  }

  render() {
    if (this.state.hasError) {
      if (this.props.fallback) {
        return this.props.fallback;
      }

      return (
        <div className="fixed inset-0 z-takeover flex items-center justify-center bg-background px-4">
          <div className="w-full max-w-md">
            <ContentError
              plain
              message="Something went wrong"
              retryLabel="Try again"
              onRetry={() => {
                this.setState({ hasError: false });
                window.location.reload();
              }}
            />
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}
