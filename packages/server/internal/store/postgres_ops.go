package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// postgres_ops.go backs the admin ops dashboard. Queries here hit
// River's own tables (river_job, river_client, river_client_queue,
// river_leader) plus our app tables (memories) and project them into
// product-framed shapes for the UI.
//
// Rules:
//   - Read-only. Destructive actions (retry / cancel) go through
//     River's client API, not raw SQL — see handler/admin_ops.go.
//   - Queries are bounded (always LIMIT, always time-windowed or
//     keyset-paginated). River tables can grow fast under load.
//   - We treat river_job.state as text at the Go layer even though
//     the column type is a River enum, so the ops code survives
//     River adding new states without a code deploy.

const (
	// opsWorkerHealthyWindow is how recently river_client.updated_at
	// must have been for the client to count as healthy. River's
	// default heartbeat is 5s; 60s = 12 missed heartbeats, which is
	// past any transient DB/network jitter but still faster than
	// River's own sweeper (~1 min to prune a dead row). An earlier
	// 30s window tripped false "processing is stalled" banners on
	// local dev where heartbeats land a bit later on cold start.
	opsWorkerHealthyWindow = 60 * time.Second

	// opsPulseWindowMinutes is the default rolling window for
	// throughput counts on the pulse card. Callers can override
	// via GetOpsPulse(ctx, windowMinutes).
	opsPulseWindowMinutes = 60

	// opsStuckMemoryThresholdSeconds is the age at which a memory
	// still in state='processing' becomes a red flag. 5 min
	// matches finalizeFailedProcessing's worst-case retry budget.
	opsStuckMemoryThresholdSeconds = 300

	// opsJobListHardLimit caps pagination to keep the list endpoint
	// cheap regardless of caller input.
	opsJobListHardLimit = 200
	opsJobListDefault   = 50

	// opsFailedIngestionsWindow is the rolling window for
	// "failed last hour" counters + the failed-list view.
	opsFailedIngestionsWindowMinutes = 60
)

// GetOpsPulse builds the landing-page summary in a single
// round-trip per sub-card. windowMinutes is clamped to a sane
// range.
func (s *PostgresStore) GetOpsPulse(ctx context.Context, windowMinutes int) (*model.OpsPulse, error) {
	if windowMinutes <= 0 {
		windowMinutes = opsPulseWindowMinutes
	}
	if windowMinutes > 24*60 {
		windowMinutes = 24 * 60
	}

	pulse := &model.OpsPulse{
		GeneratedAt: time.Now().UTC(),
	}

	workers, err := s.getOpsWorkers(ctx)
	if err != nil {
		return nil, fmt.Errorf("ops workers: %w", err)
	}
	pulse.Workers = workers

	queues, err := s.getOpsQueueDepths(ctx)
	if err != nil {
		return nil, fmt.Errorf("ops queue depths: %w", err)
	}
	pulse.Queues = queues

	throughput, err := s.getOpsThroughput(ctx, windowMinutes)
	if err != nil {
		return nil, fmt.Errorf("ops throughput: %w", err)
	}
	pulse.Throughput = throughput

	cards, err := s.getOpsIngestionCards(ctx)
	if err != nil {
		return nil, fmt.Errorf("ops ingestion cards: %w", err)
	}
	pulse.Ingestion = cards

	pulse.RedFlags = computeRedFlags(workers, queues, cards)

	// Normalize every slice on the wire to []. Go marshals nil
	// slices as JSON null, which crashes TS clients that write
	// `.length` or `.map` on the field. Doing it here (not at the
	// handler) so SSE pushes and REST responses share one contract.
	if pulse.Workers.Clients == nil {
		pulse.Workers.Clients = []model.OpsWorkerClient{}
	}
	if pulse.Queues == nil {
		pulse.Queues = []model.OpsQueueDepth{}
	}
	if pulse.RedFlags == nil {
		pulse.RedFlags = []model.OpsRedFlag{}
	}

	return pulse, nil
}

func (s *PostgresStore) getOpsWorkers(ctx context.Context) (model.OpsWorkers, error) {
	var out model.OpsWorkers
	now := time.Now().UTC()
	healthyCutoff := now.Add(-opsWorkerHealthyWindow)

	// Leader election is the AUTHORITATIVE "worker alive" signal.
	// River always writes river_leader when a client starts up;
	// river_client / river_client_queue are feature-gated and may
	// be empty even when a worker is running (observed on staging
	// with River v0.32, default config). If we relied on
	// river_client alone, we'd report "no workers registered"
	// while the worker is fine and has even acquired the lease.
	//
	// We read both tables and reconcile: if river_client has rows
	// we use them (richer queue info); if not but a valid leader
	// lease exists, we synthesize one client row from the leader.
	var leaderID string
	var leaderElectedAt, leaderExpiresAt time.Time
	if err := s.pool.QueryRow(ctx, `
		SELECT leader_id, elected_at, expires_at
		FROM river_leader WHERE name = 'default'
	`).Scan(&leaderID, &leaderElectedAt, &leaderExpiresAt); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return out, fmt.Errorf("read river_leader: %w", err)
	}
	out.Leader = leaderID

	// Per-client rows from river_client (when populated). LEFT
	// JOIN against river_client_queue for queue subscriptions.
	rows, err := s.pool.Query(ctx, `
		SELECT
			c.id,
			c.updated_at,
			COALESCE(
				jsonb_agg(jsonb_build_object(
					'queue', q.name,
					'max_workers', q.max_workers
				) ORDER BY q.name) FILTER (WHERE q.name IS NOT NULL),
				'[]'::jsonb
			) AS queues
		FROM river_client c
		LEFT JOIN river_client_queue q ON q.river_client_id = c.id
		GROUP BY c.id, c.updated_at
		ORDER BY c.updated_at DESC
	`)
	if err != nil {
		return out, fmt.Errorf("list river_client: %w", err)
	}
	defer rows.Close()

	sawAnyClient := false
	for rows.Next() {
		var cli model.OpsWorkerClient
		var queuesJSON []byte
		if err := rows.Scan(&cli.ClientID, &cli.HeartbeatAt, &queuesJSON); err != nil {
			return out, fmt.Errorf("scan river_client: %w", err)
		}
		if err := json.Unmarshal(queuesJSON, &cli.Queues); err != nil {
			return out, fmt.Errorf("decode queues: %w", err)
		}
		cli.AgeSeconds = int(now.Sub(cli.HeartbeatAt).Seconds())
		cli.Healthy = cli.HeartbeatAt.After(healthyCutoff)
		// River v0.32's leader_id format is `<machine_prefix>_<timestamp>`,
		// while our own heartbeat writer (workerapp/heartbeat.go) uses the
		// bare `<machine_prefix>` (FLY_MACHINE_ID) as the client_id. Match
		// by prefix so the heartbeat row is labeled as the leader rather
		// than showing up as a non-leader client AND spawning a synthesized
		// leader row alongside it.
		cli.IsLeader = isLeaderForClient(cli.ClientID, leaderID)
		if cli.Healthy {
			out.Healthy++
		} else {
			out.Stale++
		}
		out.Clients = append(out.Clients, cli)
		sawAnyClient = true
	}
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("iter river_client: %w", err)
	}

	// Synthesize from leader ONLY when river_client is entirely empty.
	// Any heartbeat row is authoritative evidence a worker is alive, so
	// we shouldn't invent a second synthetic row on top. This matters
	// after our own heartbeat writer landed (workerapp/heartbeat.go):
	// before that, river_client was always empty and the synthesis was
	// the only way to report the leader; now the synthesis is a
	// fallback for older workers that predate the heartbeat writer.
	if !sawAnyClient && leaderID != "" && !leaderExpiresAt.IsZero() && now.Before(leaderExpiresAt) {
		cli := model.OpsWorkerClient{
			ClientID:    leaderID,
			Queues:      []model.OpsWorkerClientQueue{},
			HeartbeatAt: leaderElectedAt,
			AgeSeconds:  int(now.Sub(leaderElectedAt).Seconds()),
			Healthy:     true,
			IsLeader:    true,
		}
		out.Clients = append(out.Clients, cli)
		out.Healthy++
	}

	return out, nil
}

// isLeaderForClient reports whether the given client_id owns the
// current leader lease. River stores leader_id as
// `<client_prefix>_<timestamp>`; our heartbeat writer stores the bare
// `<client_prefix>` as the client_id. Exact equality fails on the
// timestamp suffix, so we match on `leader_id == client_id` OR
// `leader_id` starts with `<client_id>_`. The underscore anchor
// prevents accidental matches between machine IDs that share a
// prefix (e.g. `abc` would otherwise match a leader named `abcdef_…`).
func isLeaderForClient(clientID, leaderID string) bool {
	if clientID == "" || leaderID == "" {
		return false
	}
	if clientID == leaderID {
		return true
	}
	return len(leaderID) > len(clientID) &&
		leaderID[:len(clientID)] == clientID &&
		leaderID[len(clientID)] == '_'
}

func (s *PostgresStore) getOpsQueueDepths(ctx context.Context) ([]model.OpsQueueDepth, error) {
	// Note: we only count non-terminal and recently-finalized states
	// here. Completed/cancelled are excluded from the dashboard's
	// queue-depth view — they're terminal and uninteresting at a
	// glance.
	rows, err := s.pool.Query(ctx, `
		SELECT
			queue,
			state::text AS state,
			COUNT(*) AS n
		FROM river_job
		WHERE state IN ('available', 'scheduled', 'running', 'retryable', 'pending', 'discarded')
		GROUP BY queue, state
		ORDER BY queue
	`)
	if err != nil {
		return nil, fmt.Errorf("queue depth query: %w", err)
	}
	defer rows.Close()

	byQueue := map[string]*model.OpsQueueDepth{}
	for rows.Next() {
		var queue, state string
		var n int
		if err := rows.Scan(&queue, &state, &n); err != nil {
			return nil, fmt.Errorf("scan queue depth: %w", err)
		}
		bucket, ok := byQueue[queue]
		if !ok {
			bucket = &model.OpsQueueDepth{Queue: queue}
			byQueue[queue] = bucket
		}
		switch state {
		case model.JobStateAvailable:
			bucket.Available = n
		case model.JobStateScheduled:
			bucket.Scheduled = n
		case model.JobStateRunning:
			bucket.Running = n
		case model.JobStateRetryable:
			bucket.Retryable = n
		case model.JobStatePending:
			bucket.Pending = n
		case model.JobStateDiscarded:
			bucket.Discarded = n
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iter queue depth: %w", err)
	}

	// Emit in a stable order (queue name ASC).
	out := make([]model.OpsQueueDepth, 0, len(byQueue))
	names := make([]string, 0, len(byQueue))
	for name := range byQueue {
		names = append(names, name)
	}
	// sort by name without importing sort for one call site — the
	// map is tiny.
	for i := range names {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	for _, name := range names {
		out = append(out, *byQueue[name])
	}
	return out, nil
}

func (s *PostgresStore) getOpsThroughput(ctx context.Context, windowMinutes int) (model.OpsThroughput, error) {
	t := model.OpsThroughput{WindowMinutes: windowMinutes}
	cutoff := time.Now().UTC().Add(-time.Duration(windowMinutes) * time.Minute)

	// Memories processed / failed split on kind + terminal state.
	if err := s.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE kind = 'memory_process' AND state = 'completed')  AS memories_processed,
			COUNT(*) FILTER (WHERE kind = 'memory_process' AND state = 'discarded')  AS memories_failed,
			COUNT(*) FILTER (WHERE kind = 'dream_cycle'    AND state = 'completed')  AS dreams_completed,
			COUNT(*) FILTER (WHERE kind IN ('send_email', 'send_campaign_email') AND state = 'completed') AS emails_sent
		FROM river_job
		WHERE finalized_at >= $1
	`, cutoff).Scan(&t.MemoriesProcessed, &t.MemoriesFailed, &t.DreamsCompleted, &t.EmailsSent); err != nil {
		return t, fmt.Errorf("throughput query: %w", err)
	}
	return t, nil
}

func (s *PostgresStore) getOpsIngestionCards(ctx context.Context) (model.OpsIngestionCards, error) {
	var out model.OpsIngestionCards
	now := time.Now().UTC()
	stuckCutoff := now.Add(-opsStuckMemoryThresholdSeconds * time.Second)
	failureCutoff := now.Add(-opsFailedIngestionsWindowMinutes * time.Minute)

	err := s.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE state = 'processing')                                AS in_processing,
			COUNT(*) FILTER (WHERE state = 'processing' AND created_at < $1)            AS stuck_over_five_min,
			COUNT(*) FILTER (WHERE state = 'active' AND created_at >= $2 AND summary LIKE 'Processing failed after%') AS failed_last_hour
		FROM public.memories
	`, stuckCutoff, failureCutoff).Scan(&out.InProcessing, &out.StuckOverFiveMin, &out.FailedLastHour)
	if err != nil {
		return out, fmt.Errorf("ingestion cards query: %w", err)
	}
	return out, nil
}

func computeRedFlags(workers model.OpsWorkers, queues []model.OpsQueueDepth, cards model.OpsIngestionCards) []model.OpsRedFlag {
	var flags []model.OpsRedFlag

	// Three distinct worker states — each gets a different banner
	// because the operator's action differs:
	//   - 0 healthy + 0 stale: no worker process is known to the DB
	//     at all. Either the worker never connected, or it connected
	//     to a different database than the API (the most common
	//     local-dev foot-gun).
	//   - 0 healthy + some stale: a worker was connected recently
	//     but hasn't heartbeated within the window. Likely a
	//     transient pause, network hiccup, or the worker is
	//     mid-restart. Not "stalled" — just stale.
	//   - healthy>0 + some stale: mostly fine, some clients are
	//     slow. Informational warn.
	switch {
	case workers.Healthy == 0 && workers.Stale == 0:
		flags = append(flags, model.OpsRedFlag{
			Kind:     "worker_offline",
			Severity: "error",
			Message:  "No worker clients registered in this database. Verify the worker process is running and pointed at the same database as the API.",
		})
	case workers.Healthy == 0 && workers.Stale > 0:
		flags = append(flags, model.OpsRedFlag{
			Kind:     "worker_offline",
			Severity: "warn",
			Message:  fmt.Sprintf("%d worker client(s) present but heartbeats are stale — new jobs may not be picked up until a client recovers.", workers.Stale),
		})
	case workers.Stale > 0:
		flags = append(flags, model.OpsRedFlag{
			Kind:     "worker_offline",
			Severity: "info",
			Message:  fmt.Sprintf("%d worker client(s) have a stale heartbeat.", workers.Stale),
		})
	}

	if cards.StuckOverFiveMin > 0 {
		flags = append(flags, model.OpsRedFlag{
			Kind:     "stuck_memories",
			Severity: "warn",
			Message:  fmt.Sprintf("%d memory(ies) stuck in processing > 5 minutes.", cards.StuckOverFiveMin),
		})
	}

	if cards.FailedLastHour >= 5 {
		flags = append(flags, model.OpsRedFlag{
			Kind:     "ingestion_failures",
			Severity: "error",
			Message:  fmt.Sprintf("%d memory ingestion(s) failed in the last hour.", cards.FailedLastHour),
		})
	} else if cards.FailedLastHour > 0 {
		flags = append(flags, model.OpsRedFlag{
			Kind:     "ingestion_failures",
			Severity: "warn",
			Message:  fmt.Sprintf("%d memory ingestion(s) failed in the last hour.", cards.FailedLastHour),
		})
	}

	for _, q := range queues {
		if q.Retryable >= 10 {
			flags = append(flags, model.OpsRedFlag{
				Kind:     "queue_backlog",
				Severity: "warn",
				Message:  fmt.Sprintf("Queue %q has %d retryable job(s).", q.Queue, q.Retryable),
			})
		}
	}

	return flags
}

// ListOpsJobs returns a paginated, filtered job list.
func (s *PostgresStore) ListOpsJobs(ctx context.Context, opts model.JobListOpts) (*model.JobListResponse, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = opsJobListDefault
	}
	if limit > opsJobListHardLimit {
		limit = opsJobListHardLimit
	}

	// Build filter clauses incrementally. We use prepared-style
	// numbered placeholders and keep a parallel args slice.
	var conds []string
	var args []any
	add := func(cond string, vals ...any) {
		conds = append(conds, cond)
		args = append(args, vals...)
	}

	if len(opts.States) > 0 {
		ph := placeholders(len(args), len(opts.States))
		conds = append(conds, fmt.Sprintf("state::text IN (%s)", ph))
		for _, s := range opts.States {
			args = append(args, s)
		}
	}
	if len(opts.Kinds) > 0 {
		ph := placeholders(len(args), len(opts.Kinds))
		conds = append(conds, fmt.Sprintf("kind IN (%s)", ph))
		for _, k := range opts.Kinds {
			args = append(args, k)
		}
	}
	if s := strings.TrimSpace(opts.KindSearch); s != "" {
		add(fmt.Sprintf("kind ILIKE $%d", len(args)+1), "%"+s+"%")
	}
	if len(opts.Queues) > 0 {
		ph := placeholders(len(args), len(opts.Queues))
		conds = append(conds, fmt.Sprintf("queue IN (%s)", ph))
		for _, q := range opts.Queues {
			args = append(args, q)
		}
	}
	if s := strings.TrimSpace(opts.QueueSearch); s != "" {
		add(fmt.Sprintf("queue ILIKE $%d", len(args)+1), "%"+s+"%")
	}
	if opts.CreatedAfter != nil {
		add(fmt.Sprintf("created_at >= $%d", len(args)+1), *opts.CreatedAfter)
	}
	if opts.CreatedBefore != nil {
		add(fmt.Sprintf("created_at <= $%d", len(args)+1), *opts.CreatedBefore)
	}

	// Keyset pagination on (created_at DESC, id DESC). Cursor
	// encodes the last-row pair.
	if opts.Cursor != "" {
		curTime, curID, err := decodeJobCursor(opts.Cursor)
		if err != nil {
			// Sentinel → handler returns 400 instead of leaking an
			// internal 500 on malformed client-supplied input.
			return nil, ErrInvalidJobCursor
		}
		add(
			fmt.Sprintf("(created_at, id) < ($%d, $%d)", len(args)+1, len(args)+2),
			curTime, curID,
		)
	}

	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	// Ask for limit+1 rows so we can tell whether there's another
	// page without a separate COUNT query.
	args = append(args, limit+1)

	query := fmt.Sprintf(`
		SELECT
			id, kind, queue, state::text, attempt, max_attempts, priority,
			created_at, attempted_at, finalized_at, scheduled_at,
			errors, args
		FROM river_job
		%s
		ORDER BY created_at DESC, id DESC
		LIMIT $%d
	`, where, len(args))

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list ops jobs: %w", err)
	}
	defer rows.Close()

	var summaries []model.JobSummary
	for rows.Next() {
		var js model.JobSummary
		var errorsRaw [][]byte
		var argsRaw []byte
		if err := rows.Scan(
			&js.ID, &js.Kind, &js.Queue, &js.State,
			&js.Attempt, &js.MaxAttempts, &js.Priority,
			&js.CreatedAt, &js.AttemptedAt, &js.FinalizedAt, &js.ScheduledAt,
			&errorsRaw, &argsRaw,
		); err != nil {
			return nil, fmt.Errorf("scan ops job: %w", err)
		}
		if len(errorsRaw) > 0 {
			js.LastError = lastErrorMessage(errorsRaw)
		}
		js.ProductContext = productContextFor(js.Kind, argsRaw)
		summaries = append(summaries, js)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iter ops jobs: %w", err)
	}

	resp := &model.JobListResponse{}
	if len(summaries) > limit {
		resp.HasMore = true
		summaries = summaries[:limit]
		last := summaries[len(summaries)-1]
		resp.NextCursor = encodeJobCursor(last.CreatedAt, last.ID)
	}
	resp.Jobs = summaries

	// Enrich product context with resource lookups (memories, hubs).
	if err := s.enrichJobProductContext(ctx, summaries); err != nil {
		// Enrichment is best-effort: missing titles don't break the
		// list, and the logs will catch real failures.
		_ = err
	}

	return resp, nil
}

// GetOpsJob returns full detail for one job.
func (s *PostgresStore) GetOpsJob(ctx context.Context, id int64) (*model.JobDetail, error) {
	var detail model.JobDetail
	var errorsRaw [][]byte
	var argsRaw, metadataRaw []byte
	var tags []string

	err := s.pool.QueryRow(ctx, `
		SELECT
			id, kind, queue, state::text, attempt, max_attempts, priority,
			created_at, attempted_at, finalized_at, scheduled_at,
			args, metadata, errors, tags
		FROM river_job
		WHERE id = $1
	`, id).Scan(
		&detail.ID, &detail.Kind, &detail.Queue, &detail.State,
		&detail.Attempt, &detail.MaxAttempts, &detail.Priority,
		&detail.CreatedAt, &detail.AttemptedAt, &detail.FinalizedAt, &detail.ScheduledAt,
		&argsRaw, &metadataRaw, &errorsRaw, &tags,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrJobNotFound
		}
		return nil, fmt.Errorf("read ops job: %w", err)
	}

	detail.Args = argsRaw
	detail.Metadata = metadataRaw
	// Normalize slice fields so the wire form uses [] not null —
	// otherwise TS clients crash on `.length` / `.map`. Same
	// contract as GetOpsPulse.
	detail.Tags = tags
	if detail.Tags == nil {
		detail.Tags = []string{}
	}
	detail.Errors = decodeJobErrors(errorsRaw)
	if detail.Errors == nil {
		detail.Errors = []model.JobErrorRecord{}
	}
	if len(detail.Errors) > 0 {
		detail.LastError = detail.Errors[len(detail.Errors)-1].Error
	}
	detail.ProductContext = productContextFor(detail.Kind, argsRaw)
	if err := s.enrichJobProductContext(ctx, []model.JobSummary{detail.JobSummary}); err == nil {
		// Writing back requires re-reading; the slice copy above
		// doesn't propagate mutations. Do the one-off enrichment
		// directly on detail.
		detail.ProductContext = productContextFor(detail.Kind, argsRaw)
		s.enrichSingleJobProductContext(ctx, &detail.JobSummary)
	}

	return &detail, nil
}

// ListOpsStuckMemories returns memories that have been in
// state='processing' for at least minAgeSeconds.
func (s *PostgresStore) ListOpsStuckMemories(ctx context.Context, minAgeSeconds int, limit int) ([]model.StuckMemory, error) {
	if minAgeSeconds <= 0 {
		minAgeSeconds = opsStuckMemoryThresholdSeconds
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	cutoff := time.Now().UTC().Add(-time.Duration(minAgeSeconds) * time.Second)

	rows, err := s.pool.Query(ctx, `
		SELECT
			m.id, m.owner_id, m.hub_id, m.title, m.content_type,
			m.source, COALESCE(m.source_agent, ''), m.created_at,
			EXTRACT(EPOCH FROM (now() - m.created_at))::int AS age_seconds
		FROM public.memories m
		WHERE m.state = 'processing' AND m.created_at < $1
		ORDER BY m.created_at ASC
		LIMIT $2
	`, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("list stuck memories: %w", err)
	}
	defer rows.Close()

	var out []model.StuckMemory
	for rows.Next() {
		var m model.StuckMemory
		if err := rows.Scan(
			&m.ID, &m.OwnerID, &m.HubID, &m.Title, &m.ContentType,
			&m.Source, &m.SourceAgent, &m.CreatedAt, &m.AgeSeconds,
		); err != nil {
			return nil, fmt.Errorf("scan stuck memory: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iter stuck memories: %w", err)
	}

	// Attach the most recent memory_process job for each memory,
	// if one exists. Cheap indexed lookup by args->memory_id.
	for i := range out {
		job, err := s.findLatestMemoryProcessJob(ctx, out[i].ID)
		if err == nil {
			out[i].AssociatedJob = job
		}
	}

	return out, nil
}

// ListOpsFailedIngestions returns memories where processing fell
// back to finalizeFailedProcessing (state='active' with a summary
// marker) within the last sinceMinutes.
func (s *PostgresStore) ListOpsFailedIngestions(ctx context.Context, sinceMinutes int, limit int) ([]model.FailedIngestion, error) {
	if sinceMinutes <= 0 {
		sinceMinutes = opsFailedIngestionsWindowMinutes
	}
	if sinceMinutes > 7*24*60 {
		sinceMinutes = 7 * 24 * 60
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	cutoff := time.Now().UTC().Add(-time.Duration(sinceMinutes) * time.Minute)

	rows, err := s.pool.Query(ctx, `
		SELECT
			id, owner_id, hub_id, title, content_type,
			source, COALESCE(source_agent, ''), created_at, summary
		FROM public.memories
		WHERE state = 'active'
		  AND created_at >= $1
		  AND summary LIKE 'Processing failed after%'
		ORDER BY created_at DESC
		LIMIT $2
	`, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("list failed ingestions: %w", err)
	}
	defer rows.Close()

	var out []model.FailedIngestion
	for rows.Next() {
		var f model.FailedIngestion
		if err := rows.Scan(
			&f.ID, &f.OwnerID, &f.HubID, &f.Title, &f.ContentType,
			&f.Source, &f.SourceAgent, &f.CreatedAt, &f.Summary,
		); err != nil {
			return nil, fmt.Errorf("scan failed ingestion: %w", err)
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iter failed ingestions: %w", err)
	}
	return out, nil
}

// ListOpsIngestionBySource returns per-source ingestion counts
// for the last sinceMinutes.
func (s *PostgresStore) ListOpsIngestionBySource(ctx context.Context, sinceMinutes int) ([]model.IngestionBySource, error) {
	if sinceMinutes <= 0 {
		sinceMinutes = opsFailedIngestionsWindowMinutes
	}
	if sinceMinutes > 7*24*60 {
		sinceMinutes = 7 * 24 * 60
	}
	cutoff := time.Now().UTC().Add(-time.Duration(sinceMinutes) * time.Minute)

	rows, err := s.pool.Query(ctx, `
		SELECT
			COALESCE(NULLIF(source, ''), 'unknown') AS source,
			COUNT(*)                                                        AS memories_created,
			COUNT(*) FILTER (WHERE state = 'processing')                     AS still_processing,
			COUNT(*) FILTER (WHERE state = 'active'
			                  AND summary LIKE 'Processing failed after%') AS failed_silent
		FROM public.memories
		WHERE created_at >= $1
		GROUP BY COALESCE(NULLIF(source, ''), 'unknown')
		ORDER BY memories_created DESC
	`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("ingestion by source: %w", err)
	}
	defer rows.Close()

	var out []model.IngestionBySource
	for rows.Next() {
		row := model.IngestionBySource{WindowMinutes: sinceMinutes}
		if err := rows.Scan(&row.Source, &row.MemoriesCreated, &row.StillProcessing, &row.FailedSilent); err != nil {
			return nil, fmt.Errorf("scan ingestion by source: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iter ingestion by source: %w", err)
	}
	return out, nil
}

// ErrJobNotFound is returned by GetOpsJob when the id doesn't match.
var ErrJobNotFound = errors.New("job not found")

// ErrInvalidJobCursor is returned when ListOpsJobs can't decode the
// caller's cursor. The handler translates this to 400, not 500.
var ErrInvalidJobCursor = errors.New("invalid job list cursor")

// AppendAdminAudit inserts one row into admin_audit. Separate from
// comms_audit because ops audits reference bigint job IDs, which
// don't fit comms_audit.resource_id's uuid type.
func (s *PostgresStore) AppendAdminAudit(ctx context.Context, entry *model.AdminAuditEntry) error {
	metadata := []byte(entry.Metadata)
	if len(metadata) == 0 {
		metadata = []byte(`{}`)
	}
	var actor any
	if entry.ActorID != nil && *entry.ActorID != "" {
		actor = *entry.ActorID
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO admin_audit (
			id, resource_type, resource_id, action, actor_id, metadata, created_at
		) VALUES (
			$1::uuid, $2, $3, $4, $5, $6::jsonb, now()
		)
	`, entry.ID, entry.ResourceType, entry.ResourceID, entry.Action, actor, metadata)
	if err != nil {
		return fmt.Errorf("insert admin_audit: %w", err)
	}
	return nil
}

// --- internal helpers ---

// findLatestMemoryProcessJob returns the most recent memory_process
// row for a memory id. Nil result + nil error if none.
func (s *PostgresStore) findLatestMemoryProcessJob(ctx context.Context, memoryID string) (*model.JobSummary, error) {
	var js model.JobSummary
	var argsRaw []byte
	err := s.pool.QueryRow(ctx, `
		SELECT
			id, kind, queue, state::text, attempt, max_attempts, priority,
			created_at, attempted_at, finalized_at, scheduled_at, args
		FROM river_job
		WHERE kind = 'memory_process'
		  AND args->>'memory_id' = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, memoryID).Scan(
		&js.ID, &js.Kind, &js.Queue, &js.State,
		&js.Attempt, &js.MaxAttempts, &js.Priority,
		&js.CreatedAt, &js.AttemptedAt, &js.FinalizedAt, &js.ScheduledAt,
		&argsRaw,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	js.ProductContext = productContextFor(js.Kind, argsRaw)
	return &js, nil
}

// enrichJobProductContext fills Label/SubLabel on every summary
// whose kind is enriched and whose resource lookup succeeds. Best
// effort — returns the first error encountered but doesn't abort
// the slice.
func (s *PostgresStore) enrichJobProductContext(ctx context.Context, jobs []model.JobSummary) error {
	for i := range jobs {
		s.enrichSingleJobProductContext(ctx, &jobs[i])
	}
	return nil
}

func (s *PostgresStore) enrichSingleJobProductContext(ctx context.Context, js *model.JobSummary) {
	if js.ProductContext == nil {
		return
	}
	pc := js.ProductContext
	switch pc.Kind {
	case "memory_process":
		if pc.ResourceID == "" {
			return
		}
		var title string
		err := s.pool.QueryRow(ctx, `SELECT title FROM public.memories WHERE id = $1`, pc.ResourceID).Scan(&title)
		if err == nil && title != "" {
			pc.Label = "Memory: " + title
		}
	case "dream_cycle":
		if pc.ResourceID == "" {
			return
		}
		var name string
		err := s.pool.QueryRow(ctx, `SELECT name FROM public.hubs WHERE id = $1`, pc.ResourceID).Scan(&name)
		if err == nil && name != "" {
			pc.Label = "Dream: " + name
		}
	}
}

// productContextFor is pure — no DB reads. It extracts the IDs and
// produces a placeholder label; enrichJobProductContext fills in
// human-readable labels where we can.
func productContextFor(kind string, argsRaw []byte) *model.JobProductContext {
	if len(argsRaw) == 0 {
		return nil
	}
	switch kind {
	case "memory_process":
		var args struct {
			MemoryID string `json:"memory_id"`
			Title    string `json:"title"`
		}
		if err := json.Unmarshal(argsRaw, &args); err != nil || args.MemoryID == "" {
			return nil
		}
		label := args.Title
		if label == "" {
			label = "Memory " + args.MemoryID
		} else {
			label = "Memory: " + label
		}
		return &model.JobProductContext{
			Kind:       kind,
			Label:      label,
			ResourceID: args.MemoryID,
		}
	case "dream_cycle":
		var args struct {
			HubID       string `json:"hub_id"`
			TriggeredBy string `json:"triggered_by"`
		}
		if err := json.Unmarshal(argsRaw, &args); err != nil || args.HubID == "" {
			return nil
		}
		sub := "scheduled"
		if args.TriggeredBy != "" {
			sub = "manually triggered"
		}
		return &model.JobProductContext{
			Kind:       kind,
			Label:      "Dream " + args.HubID,
			SubLabel:   sub,
			ResourceID: args.HubID,
		}
	case "send_email":
		var args struct {
			To       string `json:"to"`
			Template string `json:"template"`
		}
		if err := json.Unmarshal(argsRaw, &args); err == nil && args.To != "" {
			return &model.JobProductContext{
				Kind:     kind,
				Label:    "Email → " + args.To,
				SubLabel: args.Template,
			}
		}
	case "send_campaign_email":
		var args struct {
			CampaignID string `json:"campaign_id"`
		}
		if err := json.Unmarshal(argsRaw, &args); err == nil && args.CampaignID != "" {
			return &model.JobProductContext{
				Kind:       kind,
				Label:      "Campaign delivery",
				ResourceID: args.CampaignID,
			}
		}
	}
	return nil
}

// lastErrorMessage parses the last entry of river_job.errors (jsonb[])
// and returns its message, truncated for list payloads.
func lastErrorMessage(raw [][]byte) string {
	if len(raw) == 0 {
		return ""
	}
	var last struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw[len(raw)-1], &last); err != nil {
		return ""
	}
	const maxLen = 240
	if len(last.Error) > maxLen {
		return last.Error[:maxLen] + "…"
	}
	return last.Error
}

func decodeJobErrors(raw [][]byte) []model.JobErrorRecord {
	if len(raw) == 0 {
		return nil
	}
	out := make([]model.JobErrorRecord, 0, len(raw))
	for _, entry := range raw {
		var r struct {
			At      time.Time `json:"at"`
			Attempt int       `json:"attempt"`
			Error   string    `json:"error"`
			Trace   string    `json:"trace"`
		}
		if err := json.Unmarshal(entry, &r); err != nil {
			continue
		}
		out = append(out, model.JobErrorRecord{
			At:      r.At,
			Attempt: r.Attempt,
			Error:   r.Error,
			Trace:   r.Trace,
		})
	}
	return out
}

// placeholders returns $n,$n+1,… for a SQL IN clause given the
// current args-list length and the number of values to add.
func placeholders(startArgs int, n int) string {
	parts := make([]string, n)
	for i := range n {
		parts[i] = "$" + strconv.Itoa(startArgs+i+1)
	}
	return strings.Join(parts, ",")
}

// encodeJobCursor serializes the (created_at, id) pair for keyset
// pagination. Format is opaque on the wire — base64(RFC3339Nano|id).
func encodeJobCursor(createdAt time.Time, id int64) string {
	raw := createdAt.UTC().Format(time.RFC3339Nano) + "|" + strconv.FormatInt(id, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeJobCursor(cursor string) (time.Time, int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("decode cursor: %w", err)
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, 0, fmt.Errorf("cursor has wrong shape")
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("cursor timestamp: %w", err)
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("cursor id: %w", err)
	}
	return t, id, nil
}
