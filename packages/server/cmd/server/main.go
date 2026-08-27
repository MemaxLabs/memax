package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/observability"
	"github.com/MemaxLabs/memax/packages/server/internal/serverapp"
)

func main() {
	logger, logShutdown, err := observability.NewLogger("memax-api")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	slog.SetDefault(logger)
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := logShutdown(shutdownCtx); err != nil {
			slog.Warn("log exporter shutdown failed", "error", err)
		}
	}()

	otelResult, err := observability.Setup(context.Background(), "memax-api")
	if err != nil {
		slog.Error("failed to initialize observability", "error", err)
		os.Exit(1)
	}
	if otelResult.Enabled {
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := otelResult.Shutdown(shutdownCtx); err != nil {
				slog.Warn("otel shutdown failed", "error", err)
			}
		}()
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Start listening on the port IMMEDIATELY so Fly.io's reachability probe
	// succeeds while we run migrations and initialize services. This prevents
	// deploy failures from Neon cold start + River migration taking >8 seconds.
	addr := fmt.Sprintf(":%s", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Error("failed to listen", "addr", addr, "error", err)
		os.Exit(1)
	}
	slog.Info("listening", "addr", addr)

	// Track readiness — health returns 503 until initialization completes
	var ready atomic.Bool

	mux := http.NewServeMux()

	// Health endpoint available immediately — always returns 200 so Fly's
	// health check passes during startup. The "ready" field tells callers
	// whether the server is fully initialized.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if ready.Load() {
			w.Write([]byte(`{"status":"ok","service":"memax-api","ready":true}`))
		} else {
			w.Write([]byte(`{"status":"starting","service":"memax-api","ready":false}`))
		}
	})

	// Serve immediately in background while we initialize.
	// Non-health requests return 503 until init completes, so clients see
	// "service unavailable" instead of a confusing 404.
	var innerHandler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ready.Load() && r.URL.Path != "/health" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error":{"code":"not_ready","message":"Server is starting up, please retry in a few seconds."}}`))
			return
		}
		mux.ServeHTTP(w, r)
	})

	// Middleware chain (outermost first):
	//
	//   OriginSharedSecretMiddleware  — reject direct-to-Fly traffic
	//                                   that bypasses Cloudflare
	//   corsMiddleware                 — CORS headers + OPTIONS short-circuit
	//   ready-check + mux              — the server body
	//
	// Origin lockdown MUST be outermost so it runs before CORS (else
	// CORS OPTIONS preflights from direct-to-Fly attackers return 200
	// without the secret check) and before the ready-check (else the
	// 503 startup body leaks through without validation). When
	// ORIGIN_SHARED_SECRET is unset, OriginSharedSecretMiddleware is
	// a pass-through — no behavioral change for local dev or
	// non-Cloudflare deployments.
	rootHandler := handler.OriginSharedSecretMiddleware()(corsMiddleware(innerHandler))

	// A real *http.Server (not bare http.Serve) for two reasons the
	// worker already had and this binary didn't:
	//   - timeouts: without ReadHeaderTimeout a slow-loris connection
	//     holds a goroutine forever; without IdleTimeout keep-alives
	//     pile up across deploys. WriteTimeout stays 0 on purpose —
	//     SSE streams (/v1/events) are long-lived writes and a global
	//     write deadline would sever them; per-request deadlines are
	//     the store-context workstream (F6).
	//   - graceful shutdown: `select {}` meant the deferred
	//     app.Shutdown NEVER ran and every deploy dropped in-flight
	//     requests at the TCP layer. Fly sends SIGTERM and waits —
	//     use the window.
	srv := &http.Server{
		Handler:           rootHandler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}
	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	// --- Initialization (can take time due to Neon cold start + migrations) ---
	app, err := serverapp.Configure(context.Background(), mux)
	if err != nil {
		slog.Error("failed to initialize API server", "error", err)
		os.Exit(1)
	}

	// Mark server as ready — health endpoint now returns 200
	ready.Store(true)
	slog.Info("memax API server ready", "addr", addr)

	// Block until SIGTERM/SIGINT (Fly's stop signal), then drain:
	// stop accepting, let in-flight requests finish (30s budget, same
	// as the worker), then release app resources. Mirrors
	// cmd/worker/main.go's NotifyContext pattern.
	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-sigCtx.Done()
	slog.Info("shutdown signal received, draining")
	ready.Store(false)
	drainCtx, cancelDrain := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelDrain()
	if err := srv.Shutdown(drainCtx); err != nil {
		slog.Warn("http drain incomplete", "error", err)
	}
	appCtx, cancelApp := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelApp()
	app.Shutdown(appCtx)
	slog.Info("memax API server stopped cleanly")
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Hub-ID, X-Timezone, Mcp-Session-Id")
		w.Header().Set("Access-Control-Expose-Headers", "Mcp-Session-Id, WWW-Authenticate")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
