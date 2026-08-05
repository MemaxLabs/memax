package workerapp

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// River v0.32 ships the `river_client` table in its migrations but
// contains no Go code that ever writes to it. The `dbsqlc` package
// generates a `ClientCreateOrSetUpdatedAt` helper but nothing in the
// river library calls it. That leaves our admin ops pulse reading an
// always-empty table and falling through to the synthesize-from-
// leader branch, which always reports exactly 1 worker regardless of
// how many Fly machines are actually running.
//
// We populate river_client ourselves. The schema is a PK string id,
// a metadata JSONB, and updated_at — perfect for a simple per-machine
// heartbeat. The Pulse query already compares updated_at to a 60-
// second healthy window, so a 15-second UPSERT interval leaves three
// full cycles of headroom. On graceful shutdown we DELETE the row so
// a clean deploy doesn't leave a stale "2 workers" reading for up to
// a minute after scaling down.

const (
	heartbeatInterval = 15 * time.Second
	// writeTimeout is a per-upsert cap. The heartbeat query is a
	// tiny UPSERT against a PK so even under load it should be
	// sub-second. The cap prevents a stuck DB from wedging the
	// heartbeat goroutine.
	heartbeatWriteTimeout = 5 * time.Second
)

// workerClientID is the stable identity this machine uses in
// river_client. Every fallback appends the PID so two worker
// processes on the same host (common in dev: `pnpm dev` + a hand-
// started `go run ./cmd/worker`) don't fight over the same PK and
// silently mask each other in the admin pulse.
//
//  1. FLY_MACHINE_ID — production/staging; already unique per VM.
//  2. MEMAX_WORKER_CLIENT_ID — explicit override for testing /
//     unusual deploys where the operator wants a specific name.
//  3. hostname + "-" + pid — dev and CI. PID disambiguates
//     multiple local processes.
//  4. A timestamped sentinel with the PID — last resort when even
//     Hostname() fails (unusual container configs).
func workerClientID() string {
	if fly := os.Getenv("FLY_MACHINE_ID"); fly != "" {
		return fly
	}
	if override := os.Getenv("MEMAX_WORKER_CLIENT_ID"); override != "" {
		return override
	}
	pid := strconv.Itoa(os.Getpid())
	if host, err := os.Hostname(); err == nil && host != "" {
		return host + "-" + pid
	}
	return "memax-worker-" + time.Now().UTC().Format("20060102T150405") + "-" + pid
}

// workerClientMetadata builds the JSON stored on the river_client
// row. The admin pulse UI doesn't currently render these fields, but
// they're available for ad-hoc ops queries (psql inspection of
// `SELECT id, metadata FROM river_client`) and are easy to surface
// later by extending getOpsWorkers + OpsWorkerClient. Keep small:
// fly_region + fly_machine + fly_app + memax_env are the useful
// diagnostics for "which VM is this?".
func workerClientMetadata() []byte {
	m := map[string]string{
		"service":     "memax-worker",
		"fly_region":  os.Getenv("FLY_REGION"),
		"fly_machine": os.Getenv("FLY_MACHINE_ID"),
		"fly_app":     os.Getenv("FLY_APP_NAME"),
		"memax_env":   os.Getenv("MEMAX_ENV"),
	}
	// Strip empty values so the rendered metadata isn't cluttered
	// with blanks in dev environments.
	for k, v := range m {
		if v == "" {
			delete(m, k)
		}
	}
	buf, err := json.Marshal(m)
	if err != nil {
		// json.Marshal on a string map can't realistically fail;
		// the fallback keeps the heartbeat writer resilient.
		return []byte(`{"service":"memax-worker"}`)
	}
	return buf
}

// startClientHeartbeat spawns a goroutine that UPSERTs this worker's
// row into river_client every heartbeatInterval. Returns a cleanup
// func that stops the ticker and deletes the row — call it from
// App.Shutdown so a graceful stop removes the worker from the pulse
// immediately instead of waiting out the 60s healthy window.
//
// Cancellation is owned locally (hbCtx below), NOT derived solely
// from the caller's ctx. That matters because App.Shutdown may be
// invoked without the parent ctx being cancelled first — if the
// goroutine were parked on <-parentCtx.Done() in that case, the
// cleanup's <-done wait would hang forever. The local cancel gives
// Shutdown a guaranteed wake-up path regardless of what the parent
// ctx is doing.
func startClientHeartbeat(parentCtx context.Context, pool *pgxpool.Pool) func(context.Context) error {
	clientID := workerClientID()
	metadata := workerClientMetadata()

	// UPSERT semantics: insert if missing, bump updated_at if
	// present. The pulse uses updated_at as the heartbeat
	// timestamp, so we refresh it every tick even when nothing
	// else changed.
	upsertSQL := `
		INSERT INTO river_client (id, metadata, updated_at)
		VALUES ($1, $2::jsonb, now())
		ON CONFLICT (id) DO UPDATE
		SET updated_at = now(),
		    metadata = EXCLUDED.metadata
	`
	deleteSQL := `DELETE FROM river_client WHERE id = $1`

	// hbCtx is derived from parentCtx so a parent cancel still
	// flows through, but also has its own cancel() so the cleanup
	// func can wake the goroutine deterministically.
	hbCtx, hbCancel := context.WithCancel(parentCtx)

	write := func() {
		writeCtx, cancel := context.WithTimeout(hbCtx, heartbeatWriteTimeout)
		defer cancel()
		if _, err := pool.Exec(writeCtx, upsertSQL, clientID, metadata); err != nil {
			// Heartbeat failures are logged but not fatal — we'd
			// rather the worker keep processing jobs than crash
			// because the admin pulse can't see it briefly.
			slog.WarnContext(writeCtx, "worker heartbeat upsert failed", "error", err, "client_id", clientID)
		}
	}

	// Write once immediately so the pulse picks up the new worker
	// before the first 15s tick — avoids a dead-reckoning window
	// right after deploy.
	write()

	ticker := time.NewTicker(heartbeatInterval)
	done := make(chan struct{})

	go func() {
		defer close(done)
		defer ticker.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-ticker.C:
				write()
			}
		}
	}()

	return func(shutdownCtx context.Context) error {
		// Wake the goroutine via our local cancel — safe to call
		// even if parentCtx was already cancelled. Then wait for
		// the goroutine to return so we don't race a final UPSERT.
		hbCancel()
		<-done

		// Best-effort delete. Use a fresh ctx (not hbCtx, which
		// we just cancelled) so this call actually runs. The pool
		// is still open at this point because cleanups run in
		// reverse registration order and heartbeat was registered
		// in Start() after pool.Close in New().
		delCtx, cancel := context.WithTimeout(context.Background(), heartbeatWriteTimeout)
		defer cancel()
		if _, err := pool.Exec(delCtx, deleteSQL, clientID); err != nil {
			slog.Warn("worker heartbeat delete on shutdown failed", "error", err, "client_id", clientID)
			return err
		}
		return nil
	}
}
