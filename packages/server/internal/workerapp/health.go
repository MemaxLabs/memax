package workerapp

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// startHealthServer runs a lightweight HTTP server for Fly.io health checks
// and observability. Reports worker status, job metrics, and queue depth.
func startHealthServer(pool *pgxpool.Pool, stats *Stats) {
	port := os.Getenv("HEALTH_PORT")
	if port == "" {
		port = "9090"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		dbOK := pool.Ping(r.Context()) == nil

		var pending, running, available int
		pool.QueryRow(r.Context(),
			`SELECT
				COUNT(*) FILTER (WHERE state = 'available'),
				COUNT(*) FILTER (WHERE state = 'running'),
				COUNT(*) FILTER (WHERE state = 'available' OR state = 'scheduled')
			FROM river_job
			WHERE state IN ('available', 'running', 'scheduled')`).
			Scan(&available, &running, &pending)

		lastJob := stats.LastJobAt()

		status := "healthy"
		httpStatus := http.StatusOK
		if !dbOK {
			status = "unhealthy"
			httpStatus = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(httpStatus)
		json.NewEncoder(w).Encode(map[string]any{
			"status":         status,
			"service":        "memax-worker",
			"uptime_seconds": int(time.Since(stats.StartedAt()).Seconds()),
			"database":       dbOK,
			"jobs": map[string]any{
				"completed": stats.JobsCompleted(),
				"failed":    stats.JobsFailed(),
				"last_at":   lastJob,
			},
			"queue": map[string]any{
				"pending":   pending,
				"running":   running,
				"available": available,
			},
		})
	})

	addr := fmt.Sprintf(":%s", port)
	slog.Info("worker health server started", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("health server failed", "error", err)
	}
}
