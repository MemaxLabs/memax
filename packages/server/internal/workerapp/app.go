package workerapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"

	sdkmodel "github.com/MemaxLabs/memax-go-agent-sdk/model"

	"github.com/MemaxLabs/memax/packages/server/internal/agent"
	"github.com/MemaxLabs/memax/packages/server/internal/agent/approver"
	"github.com/MemaxLabs/memax/packages/server/internal/agent/providers"
	"github.com/MemaxLabs/memax/packages/server/internal/agent/tools"
	"github.com/MemaxLabs/memax/packages/server/internal/analytics"
	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	"github.com/MemaxLabs/memax/packages/server/internal/cache"
	"github.com/MemaxLabs/memax/packages/server/internal/chatstream"
	"github.com/MemaxLabs/memax/packages/server/internal/dreams"
	"github.com/MemaxLabs/memax/packages/server/internal/email"
	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/categorize"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/extract"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/fileproc"
	ingestformat "github.com/MemaxLabs/memax/packages/server/internal/ingest/format"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/link"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/summarize"
	ingesttitle "github.com/MemaxLabs/memax/packages/server/internal/ingest/title"
	"github.com/MemaxLabs/memax/packages/server/internal/meter"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/objectstore"
	"github.com/MemaxLabs/memax/packages/server/internal/planresolver"
	"github.com/MemaxLabs/memax/packages/server/internal/plans"
	"github.com/MemaxLabs/memax/packages/server/internal/queue"
	"github.com/MemaxLabs/memax/packages/server/internal/quota"
	"github.com/MemaxLabs/memax/packages/server/internal/safefetch"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// App owns the worker process dependencies and River client.
type App struct {
	pool        *pgxpool.Pool
	riverClient *river.Client[pgx.Tx]
	stats       *Stats
	cleanups    []func(context.Context) error
}

// New initializes the worker dependency graph.
func New(ctx context.Context) (*App, error) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required for the worker")
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	slog.Info("connected to PostgreSQL")

	app := &App{
		pool:  pool,
		stats: NewStats(time.Now()),
	}
	app.addClose(pool.Close)

	posthog := analytics.New()
	if posthog != nil {
		app.addClose(posthog.Close)
	}

	s := store.NewPostgresStore(pool)
	embedder := embed.NewEmbedder()
	llm := anthropic.NewFromEnv()
	if llm != nil {
		llm.SetAnalytics(posthog)
	}
	sum := summarize.New(llm)
	ext := extract.New(llm)
	cat := categorize.New(llm)
	lp := link.New(llm)
	fp := fileproc.New(llm)
	formatter := ingestformat.New(llm)
	titleResolver := ingesttitle.New(llm)
	blobStore := objectstore.NewFromEnv()
	eventsPublisher := events.NewPublisherFromEnv()
	if eventsPublisher != nil {
		app.addCleanup(func(context.Context) error {
			return eventsPublisher.Close()
		})
	}

	// Phase 3.6b3a — Broker for the worker process so the
	// SDK-host approver in chat runs receives approval.decided
	// fanouts published by API servers (the decide endpoint
	// runs there). Without this, the worker would only learn
	// about decisions via the 250ms poll fallback, adding
	// 0–250ms of latency on every approve/deny click. Same
	// shape as serverapp/app.go's Broker construction (Redis
	// pub/sub on `memax:events`); shared channel, distinct
	// subscribers per process.
	eventsBroker := events.NewBrokerFromEnv(slog.Default())
	if eventsBroker != nil {
		eventsBroker.Start(ctx)
		app.addCleanup(func(context.Context) error {
			return eventsBroker.Close()
		})
		slog.Info("realtime events broker enabled (worker process)")
	}
	dreamEngine := dreams.New(s, embedder, llm, eventsPublisher)
	if dreamEngine != nil {
		// Agentic dream runtime (plan 24/25): powers the routed
		// contradiction/organize paths and the Lane B board-synthesis
		// phase. Nil-safe at two layers — no ANTHROPIC_API_KEY means
		// an unconfigured runtime and every agentic path falls back;
		// hubs additionally opt in via dreams_use_agent_runtime.
		if agentModel := providers.NewAnthropicFromEnv(anthropic.ModelFromEnvWithDefault("DREAMS_AGENT_MODEL", anthropic.SonnetModel)); agentModel != nil {
			if dreamRecall, err := tools.NewAgentRecallService(chatRecallStoreAdapter{s: s}, embedder); err != nil {
				slog.Warn("dream agent runtime: recall service init failed, agentic phases disabled", "error", err)
			} else {
				dreamEngine.SetAgentRuntime(&dreams.AgentRuntime{
					Model:         agentModel,
					RecallService: dreamRecall,
					Resolver:      agent.NewScopeResolver(agent.NewStoreMembership(s)),
				})
				slog.Info("dream agent runtime configured (Lane B board synthesis available)")
			}
		}
	}

	// Plan 18 §4.6 — the first_dream checklist item was previously
	// auto-ticked on dream completion via a hook wired here. Per
	// user direction 2026-05-19 the auto-tick was dropped — the
	// checklist now requires an explicit user click on the "Trigger
	// now" CTA, which optimistically completes the item.

	// Plan registry for dreams-enabled enforcement
	planRegistry := plans.NewRegistry(s)
	if err := planRegistry.Load(ctx); err != nil {
		slog.Warn("worker: plan registry load failed, dreams will skip plan checks", "error", err)
	} else {
		cancel := planRegistry.Start(ctx, 60*time.Second)
		app.addCleanup(func(context.Context) error { cancel(); return nil })
		dreamEngine.SetPlanResolver(planRegistry)
		slog.Info("worker: plan registry loaded", "count", len(planRegistry.AllPlans()))
	}

	// Email sender — provider selected by env var (first match wins)
	var emailSender email.Sender
	if resendKey := os.Getenv("RESEND_API_KEY"); resendKey != "" {
		emailSender = email.NewResendSender(resendKey)
		slog.Info("email: using Resend")
	} else if smtpHost := os.Getenv("SMTP_HOST"); smtpHost != "" {
		emailSender = email.NewSMTPSender(
			smtpHost,
			os.Getenv("SMTP_USERNAME"),
			os.Getenv("SMTP_PASSWORD"),
		)
		slog.Info("email: using SMTP", "host", smtpHost)
	} else {
		emailSender = &email.LogSender{}
		slog.Info("email: using dev logger (no RESEND_API_KEY or SMTP_HOST set)")
	}

	templateManager, err := email.NewTemplateManager(s, s)
	if err != nil {
		app.Shutdown(context.Background())
		return nil, fmt.Errorf("load email template manager: %w", err)
	}
	// Worker is the process that actually renders queued emails (the API
	// server enqueues, the worker dequeues + sends). Both base URLs must
	// be configured here OR the auto-injected {{ .AppURL }} / {{ .DocsURL }}
	// template vars fall through to hardcoded prod defaults, and
	// staging/dev waitlist emails link to prod — defeating the per-env
	// config intent.
	templateManager.SetAppBaseURL(os.Getenv("APP_BASE_URL"))
	templateManager.SetDocsBaseURL(os.Getenv("DOCS_BASE_URL"))
	emailFrom := os.Getenv("EMAIL_FROM")
	if emailFrom == "" {
		// Brand rule: memax is always lowercase, even at the start of
		// a sentence. Staging/prod override via Doppler-managed Fly
		// secret — verify that value matches this casing.
		emailFrom = "memax <noreply@memax.app>"
	}

	logEnabled("embeddings", embedder != nil)
	logEnabled("summarization", sum != nil)
	logEnabled("fact extraction", ext != nil)
	logEnabled("auto-categorization", cat != nil)
	logEnabled("link processing", lp != nil)
	logEnabled("file processing", fp != nil)
	logEnabled("content formatting", formatter != nil)
	logEnabled("memory dreams", dreamEngine != nil)
	logEnabled("object storage", blobStore != nil)

	insertClient, err := queue.InsertClient(pool)
	if err != nil {
		app.Shutdown(context.Background())
		return nil, fmt.Errorf("create insert client: %w", err)
	}

	workers := river.NewWorkers()
	river.AddWorker(workers, &queue.MemoryProcessWorker{
		Store:         s,
		Events:        eventsPublisher,
		Embedder:      embedder,
		Summarizer:    sum,
		Extractor:     ext,
		Categorizer:   cat,
		LinkProcessor: lp,
		FileProcessor: fp,
		Formatter:     formatter,
		TitleResolver: titleResolver,
		ObjectStore:   blobStore,
	})
	river.AddWorker(workers, &queue.DreamCycleWorker{
		Engine:      dreamEngine,
		RiverClient: insertClient,
	})
	river.AddWorker(workers, &queue.NightlyDreamSweepWorker{
		Store:       s,
		RiverClient: insertClient,
	})
	river.AddWorker(workers, &queue.BoardRefreshWorker{Store: s})
	river.AddWorker(workers, &queue.BoardSweepWorker{
		Store:       s,
		RiverClient: insertClient,
	})
	river.AddWorker(workers, &queue.ConfigExtractWorker{
		Store:       s,
		Categorizer: cat,
		Embedder:    embedder,
	})
	river.AddWorker(workers, &queue.SendEmailWorker{
		Sender:    emailSender,
		Templates: templateManager,
		FromAddr:  emailFrom,
	})
	river.AddWorker(workers, &queue.SendRenderedEmailWorker{
		Sender:   emailSender,
		FromAddr: emailFrom,
	})
	river.AddWorker(workers, &queue.SendInviteRemindersWorker{
		Store:       s,
		RiverClient: insertClient,
	})
	river.AddWorker(workers, &queue.ExpireWaitlistInvitesWorker{
		Store: s,
	})
	river.AddWorker(workers, &queue.ExpireNotificationsWorker{
		Store:  s,
		Events: eventsPublisher,
	})
	river.AddWorker(workers, &queue.SendBroadcastNotificationWorker{
		Store:  s,
		Events: eventsPublisher,
	})
	river.AddWorker(workers, &queue.CampaignSendWorker{
		Store:     s,
		Templates: templateManager,
		EnqueueRenderedEmail: func(ctx context.Context, to, subject, html, text string, tags map[string]string, headers map[string]string) error {
			return insertClient.Insert(ctx, queue.SendRenderedEmailArgs{
				To:      to,
				Subject: subject,
				HTML:    html,
				Text:    text,
				Tags:    tags,
				Headers: headers,
			}, nil)
		},
		// APP_BASE_URL already gates transactional links (hub invites,
		// waitlist invites). Reuse it as the unsubscribe URL base so
		// campaign emails get one-click unsubscribe footers when the
		// server knows its public origin.
		UnsubscribeURLBase: os.Getenv("APP_BASE_URL"),
		Events:             eventsPublisher,
	})
	river.AddWorker(workers, &queue.HubFreezeSweepWorker{
		Store:  s,
		Events: eventsPublisher,
	})

	// Phase 3.4b1 — chat message run worker with the production
	// agent runtime wired. ChatAgentRuntime requires three
	// pieces:
	//
	//   - Anthropic SDK client (provider.NewAnthropicFromEnv).
	//     nil when ANTHROPIC_API_KEY is unset.
	//   - Strict-hub RecallService (tools.NewAgentRecallService).
	//     Wraps the store's SearchChunksInHubs + GetMemoryInHubs
	//     under a strict `m.hub_id = ANY($hubs)` predicate so
	//     non-target hubs never leak into agent results.
	//   - ScopeResolver — derives per-call SessionScope from
	//     live hub membership. Same primitive dreams uses for
	//     its agentic path.
	//
	// If any piece is nil (no API key, no embedder), the runtime
	// stays nil and the worker preserves Phase 3.4a's
	// fail-closed shape: chat sends fail with
	// `runtime_not_configured` so users see a clear error
	// rather than a stuck "thinking..." state.
	// Critical Redis client is built early because the chat
	// stream signaler (Phase 3.5b) needs it for the workers
	// registered just below. The meter (further down) shares
	// the same client. Nil-safe everywhere — no Redis means
	// degraded mode for metering and poll-only delivery for
	// chat streams.
	criticalRedis := cache.CriticalFromEnv()
	var redisClient *redis.Client
	if criticalRedis != nil {
		redisClient = criticalRedis.Client()
		app.addCleanup(func(context.Context) error { return criticalRedis.Close() })
	}
	chatSignaler := chatstream.NewSignaler(redisClient)
	logEnabled("chat stream signaler", chatSignaler != nil)

	chatRuntime := buildChatAgentRuntime(s, embedder, eventsBroker, nil)
	logEnabled("chat agent runtime", chatRuntime.IsConfigured())
	river.AddWorker(workers, &queue.ChatMessageRunWorker{
		Store:    s,
		Runtime:  chatRuntime,
		Signaler: chatSignaler,
	})

	// Phase 3.5a — chat lease sweeper + 24h replay-buffer purge.
	// Both run on the default queue (cheap, low-frequency); see
	// configurePeriodicJobs for cadence.
	river.AddWorker(workers, &queue.ChatLeaseSweepWorker{Store: s, Signaler: chatSignaler})
	river.AddWorker(workers, &queue.ChatEventPurgeWorker{Store: s})
	river.AddWorker(workers, &queue.ChatApprovalSweepWorker{Store: s, Publisher: eventsPublisher})

	// Usage sync worker — syncs committed Redis counters to Postgres every 5 min.
	//
	// Meter construction: always call meter.New, even without
	// critical Redis. The meter's degraded-mode contract
	// (meter.go:37) is that usage_events still write to Postgres;
	// only the per-user Redis quota counters are skipped. The API
	// server does the same thing so push/recall/ask usage_events
	// persist in dev environments without Redis — the worker needs
	// to match that so dream.* rows follow the same rule.
	usageMeter := meter.New(redisClient, s, planRegistry)
	river.AddWorker(workers, &queue.UsageSyncWorker{
		Store: s,
		Meter: usageMeter,
	})

	// Phase 3.6b3e v2/v3 codex follow-up — late-bind the meter,
	// events publisher, object store, and hub-quota resolver
	// into chatForgetAdapter so chat-driven deletes match the
	// REST DELETE path's full side-effect set: counter adjusts,
	// hub.memories.changed event, R2 blob purge, and clearing
	// the over_limit_since freeze marker. The adapter was
	// constructed in buildChatAgentRuntime before any of these
	// existed; this patch installs them now.
	if forgetAdapter, ok := chatRuntime.ForgetService.(*chatForgetAdapter); ok && forgetAdapter != nil {
		forgetAdapter.meter = usageMeter
		forgetAdapter.events = eventsPublisher
		if blobStore != nil {
			forgetAdapter.objectStore = blobStore
		}
		if planRegistry != nil {
			forgetAdapter.hubQuota = planresolver.New(redisClient, s, planRegistry)
		}
	}

	// Phase 3.6b3f — late-bind the events publisher into
	// chatProposeTopicMergeAdapter so the realtime
	// `notification.created` event reaches the user's inbox UI
	// the moment a chat-proposed merge is upserted. Constructed
	// in buildChatAgentRuntime before eventsPublisher existed;
	// installs it now.
	if proposeAdapter, ok := chatRuntime.ProposeTopicMergeService.(*chatProposeTopicMergeAdapter); ok && proposeAdapter != nil {
		proposeAdapter.events = eventsPublisher
	}

	// Plan 23 §5.3 — onboarding seed-memory copy worker. Receives the
	// same insertClient so each successful copy can chain a follow-up
	// MemoryProcessArgs job (chunk + embed + classify the copy so
	// recall surfaces it).
	river.AddWorker(workers, &queue.CopySeedMemoriesWorker{
		Store:       s,
		RiverClient: insertClient,
	})

	// Wire the meter into the dream engine so each phase emits a
	// usage_events row. Now unconditional — the meter writes to
	// Postgres even in degraded mode, so dreams are visible to
	// billing regardless of Redis availability.
	if dreamEngine != nil {
		dreamEngine.SetMeter(usageMeter)
	}

	// Dream-tier quota wiring (docs/plans/23-dream-tiers.md). The
	// engine captures a quota snapshot at run start and writes the
	// completion ledger + counter increment + soft-mode signal in
	// finalizeDreamRun. Best-effort: a quota failure logs but does
	// not undo a successfully-completed run.
	if dreamEngine != nil && planRegistry != nil {
		dreamQuotaResolver := planresolver.New(redisClient, s, planRegistry)
		dreamEngine.SetQuota(dreamQuotaResolver, s)
		if mode := strings.TrimSpace(os.Getenv("DREAM_QUOTA_MODE")); mode != "" {
			switch quota.Mode(mode) {
			case quota.ModeHard:
				dreamEngine.SetQuotaMode(quota.ModeHard)
				slog.Info("worker dream quota: hard enforcement enabled via DREAM_QUOTA_MODE")
			case quota.ModeSoft:
				dreamEngine.SetQuotaMode(quota.ModeSoft)
			default:
				slog.Warn("worker dream quota: invalid DREAM_QUOTA_MODE; falling back to soft",
					"value", mode)
			}
		}
	}

	periodicJobs := configurePeriodicJobs(dreamEngine != nil)
	riverClient, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Logger: slog.Default(),
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 20},
			"dreams":           {MaxWorkers: 3},
			// Phase 3.4a — chat queue. Higher concurrency than
			// dreams because a chat turn is interactive (user is
			// waiting); we want the worker pool deep enough to
			// absorb a burst of concurrent sends without
			// queueing latency. 8 is a starting point — tune
			// alongside the per-process model client's rate
			// limits.
			"chat": {MaxWorkers: 8},
		},
		Workers: workers,
		// Global worker middleware: every job's Work() runs inside a
		// ctx pre-loaded with job_id/kind/queue/attempt as slog attrs.
		// Paired with observability.NewCtxAttrsHandler on the default
		// logger, any slog.InfoContext/ErrorContext call inside a job
		// handler (or any code it transitively calls with the ctx)
		// lands in Loki tagged with the job id — which is what the
		// admin ops logs panel queries against.
		Middleware:   []rivertype.Middleware{queue.NewLoggerMiddleware()},
		PeriodicJobs: periodicJobs,
	})
	if err != nil {
		app.Shutdown(context.Background())
		return nil, fmt.Errorf("create river client: %w", err)
	}
	app.riverClient = riverClient
	subscribeJobMetrics(riverClient, app.stats, posthog)

	// Phase 3.6b3d — late-bind the river client into the chat
	// push adapter. The adapter was constructed earlier (before
	// riverClient existed) with a nil river field; now that
	// the client is real, install it so push_memory can enqueue
	// MemoryProcessArgs for background chunk/embed/categorize.
	if pushAdapter, ok := chatRuntime.PushService.(*chatPushAdapter); ok && pushAdapter != nil {
		pushAdapter.river = riverClient
	}

	return app, nil
}

// buildChatAgentRuntime composes the production chat-agent
// runtime from the worker's existing dependencies. Returns
// `*queue.ChatAgentRuntime` whose IsConfigured() reports the
// composite readiness — any missing piece (no Anthropic key,
// no embedder, etc.) leaves the returned runtime in a
// non-configured state and the worker falls back to
// `runtime_not_configured` failures (Phase 3.4a's contract).
//
// Why this lives in workerapp rather than queue: workerapp owns
// the lifecycle of the SDK provider client, the embedder, and
// the store. queue.ChatAgentRuntime is the consumer-side
// bundle. Composing here keeps queue/ free of knowledge about
// Anthropic key resolution, environment variables, etc.
// chatModelAllowlist is the closed set of session-level model
// names that the chat handler accepts (matched against
// handler.chatModelAllowlist; Phase 3.8 wires plan-tier gating).
// Pre-building one provider client per name at startup is the
// pattern the providers package recommends — keeps the hot path
// allocation-free and gives codex's "session model must be
// honored" property a deterministic foundation. The constant
// duplication is intentional: each layer owns the contract
// boundary it sees, and a handler change must consciously
// touch this list too.
var chatModelAllowlist = []string{
	"claude-sonnet-4-6",
	"claude-haiku-4-5-20251001",
}

func buildChatAgentRuntime(s *store.PostgresStore, embedder embed.Embedder, broker *events.Broker, riverClient *river.Client[pgx.Tx]) *queue.ChatAgentRuntime {
	if s == nil || embedder == nil {
		return &queue.ChatAgentRuntime{}
	}
	// Pre-build one SDK client per allowlisted model. The
	// session's `model` field (set at create-time, validated
	// against the handler's allowlist) selects which client a
	// given turn uses via ChatAgentRuntime.ModelFor. The
	// providers package documents this as the production
	// pattern: per-(provider, model) clients constructed once
	// at startup so the per-turn hot path does no allocation.
	models := make(map[string]sdkmodel.Client, len(chatModelAllowlist))
	for _, name := range chatModelAllowlist {
		// Thinking-enabled: chat renders readable reasoning as a stream layer.
		c := providers.NewAnthropicChatFromEnv(name)
		if c == nil {
			// No ANTHROPIC_API_KEY — every entry will be nil,
			// runtime stays unconfigured. Drop out clean.
			return &queue.ChatAgentRuntime{}
		}
		models[name] = c
	}
	// DefaultModel is the chat default (sonnet 4.6) — used when
	// a session row carries a name we haven't pre-built (rare,
	// only happens during a model rollout / rollback).
	defaultModel := models["claude-sonnet-4-6"]
	recallService, err := tools.NewAgentRecallService(chatRecallStoreAdapter{s: s}, embedder)
	if err != nil {
		slog.Error("chat agent runtime: NewAgentRecallService failed", "err", err)
		return &queue.ChatAgentRuntime{}
	}
	browseService, err := tools.NewAgentBrowseService(chatBrowseStoreAdapter{s: s})
	if err != nil {
		slog.Error("chat agent runtime: NewAgentBrowseService failed", "err", err)
		return &queue.ChatAgentRuntime{}
	}
	resolver := agent.NewScopeResolver(agent.NewStoreMembership(s))

	// Phase 3.6b3a — SDK-host approver for chat tool approvals.
	// Built once at startup; per-turn ForTurn calls bind a fresh
	// approver to (ownerID, messageID) inside the chat worker's
	// Work() method. The Subscriber is the events.Broker (for
	// cross-process approve/deny wakeups via Redis pub/sub);
	// nil broker degrades to poll-only mode.
	approverSvc := approver.New(approver.Config{
		Store:      s,
		Subscriber: approver.BrokerSubscriber(broker),
	})

	// Phase 3.6b3d — production push_memory service. Wraps
	// store.CreateMemory + a worker-local River insert of
	// MemoryProcessArgs (so the new memory gets chunked +
	// embedded by the same pipeline that picks up REST/MCP
	// pushes). Nil riverClient means push_memory is
	// build-eligible but its background-process enqueue is
	// skipped (the memory is created but won't be searchable
	// until a manual reprocess). That degraded mode lets
	// dev/staging without a fully-running River loop still
	// exercise the approval flow.
	pushSvc := &chatPushAdapter{store: s, river: riverClient}

	// Phase 3.6b3e — production forget_memory service. Wraps
	// store.GetMemoryInHubs (scope-checked) + DeleteMemory /
	// DeleteHubMemory (hard delete; audit lives in
	// chat_tool_approvals). The store layer's strict-hub read
	// enforces the SECURITY boundary: the memory must be in the
	// agent's allowed hub set. Per-call role gate via
	// GetHubMemberRole + canDeleteMemoryRoleNonOwner catches
	// the case where the session scope contains a hub the user
	// has read but not delete access on (the chat session's
	// scope.HubIDs is read-based; the per-call gate catches
	// role downgrades). Meter, events publisher, object store,
	// and hub-quota resolver are late-bound in NewApp after
	// they're constructed.
	forgetSvc := &chatForgetAdapter{store: s}

	// Phase 3.6b3f — production propose_topic_merge service.
	// Notification-native: writes a pending review_topic_merge
	// notification, never touches the topic graph (the user
	// applies the merge from their inbox via the existing
	// notifications-resolve API). The store reference is the
	// only required dependency at construction; events
	// publisher is late-bound from NewApp.
	proposeMergeSvc := &chatProposeTopicMergeAdapter{store: s}

	// Phase 3.6b3g — production fetch_url service. Wraps a
	// safefetch.Client (SSRF-safe HTTP transport: dial-time IP
	// block + URL-level validation + redirect re-validation).
	// The safefetch.Client is constructed once and reused
	// across every fetch — it owns a connection pool and
	// configured transport. Plan-24-pinned defaults: 5MB body
	// cap, 10s timeout, ≤3 redirects.
	fetchURLClient := safefetch.New(safefetch.Options{})
	fetchURLSvc := &chatFetchURLAdapter{client: fetchURLClient}

	return &queue.ChatAgentRuntime{
		Models:                   models,
		DefaultModel:             defaultModel,
		RecallService:            recallService,
		BrowseService:            browseService,
		Resolver:                 resolver,
		Approver:                 approverSvc,
		PushService:              pushSvc,
		ForgetService:            forgetSvc,
		ProposeTopicMergeService: proposeMergeSvc,
		FetchURLService:          fetchURLSvc,
	}
}

// chatPushAdapter implements tools.PushService for the chat
// worker. Sequence:
//
//  1. Verify the owner has WRITE access to the target hub
//     (Phase 3.6b3d v2 codex review caught the gap: the
//     chat session's scope was built from READ-accessible
//     hubs, so a viewer-role chat principal could push to
//     any hub in their session's scope after approval —
//     approval is not a permission substitute).
//  2. CreateMemory (owner-scoped via the store contract).
//  3. Enqueue MemoryProcessArgs for background chunk/embed/
//     categorize.
//
// Returns:
//   - PushOutcome{Status: "saved"} — fully successful.
//   - PushOutcome{Status: "saved_unprocessed"} — memory
//     committed but background enqueue failed; row exists,
//     but recall won't find it until manual reprocess.
//   - error — write-permission rejected, or store.CreateMemory
//     failed. No partial state.
//
// chatPushStore is the small store surface chatPushAdapter
// uses. Phase 3.6b3d v2 codex flagged that the adapter's
// role gate had no direct test; defining the interface here
// (rather than embedding the full *store.PostgresStore) makes
// adapter tests trivial — drop in a fake implementing the
// two methods.
type chatPushStore interface {
	GetHubMemberRole(hubID, userID string) (string, error)
	CreateMemory(memory *model.Memory) error
}

type chatPushAdapter struct {
	store chatPushStore
	river *river.Client[pgx.Tx]
}

// canWriteMemoriesRole mirrors handler.canWriteMemories. Roles
// allowed to push: owner, admin, contributor. Viewer is read-
// only.
//
// Local copy (not a shared helper) because handler can't be
// imported from queue. The role-gate is enforced at the
// store boundary in handler too; the chat path's gate is
// belt-and-suspenders, scoped to the per-call hub.
func canWriteMemoriesRole(role string) bool {
	switch role {
	case "owner", "admin", "contributor":
		return true
	}
	return false
}

func (a *chatPushAdapter) CreateAndProcess(ctx context.Context, m *model.Memory, req model.PushRequest) (tools.PushOutcome, error) {
	if a == nil || a.store == nil {
		return tools.PushOutcome{}, fmt.Errorf("chatPushAdapter: store is required")
	}

	// Phase 3.6b3d v2 — write-permission gate. The chat
	// session's scope.HubIDs were built from READ-accessible
	// hubs (handler.GetAccessibleHubIDs); membership may have
	// changed since session-create AND the role may not
	// permit writes. GetHubMemberRole returns the LIVE role,
	// which is the one we enforce against.
	role, err := a.store.GetHubMemberRole(m.HubID, m.OwnerID)
	if err != nil {
		return tools.PushOutcome{}, fmt.Errorf("get hub role: %w", err)
	}
	if role == "" {
		return tools.PushOutcome{}, fmt.Errorf("no live membership in hub %q", m.HubID)
	}
	if !canWriteMemoriesRole(role) {
		return tools.PushOutcome{}, fmt.Errorf("hub role %q does not permit writes", role)
	}

	if err := a.store.CreateMemory(m); err != nil {
		return tools.PushOutcome{}, fmt.Errorf("create memory: %w", err)
	}
	if a.river == nil {
		slog.WarnContext(ctx, "chat push: river client unset; memory saved without background processing",
			"memory_id", m.ID)
		return tools.PushOutcome{Status: tools.PushStatusSavedUnprocessed}, nil
	}
	if _, err := a.river.Insert(ctx, queue.MemoryProcessArgs{
		MemoryID:       m.ID,
		OwnerID:        m.OwnerID,
		Content:        req.Content,
		Title:          req.Title,
		Hint:           req.Hint,
		MemoryKind:     req.Kind,
		Stability:      req.Stability,
		Tags:           req.Tags,
		ContentType:    req.ContentType,
		Source:         req.Source,
		SourceAgent:    req.SourceAgent,
		HubReason:      req.HubReason,
		ProjectContext: req.ProjectContext,
	}, nil); err != nil {
		slog.WarnContext(ctx, "chat push: enqueue MemoryProcessArgs failed; memory saved without background processing",
			"memory_id", m.ID, "err", err)
		return tools.PushOutcome{Status: tools.PushStatusSavedUnprocessed}, nil
	}
	return tools.PushOutcome{Status: tools.PushStatusSaved}, nil
}

// chatForgetStore is the small store surface chatForgetAdapter
// uses. Phase 3.6b3e — defining it inline (rather than
// embedding the full *store.PostgresStore) keeps adapter tests
// decoupled from Postgres setup, same pattern chatPushAdapter
// follows.
type chatForgetStore interface {
	GetMemoryInHubs(ctx context.Context, id string, hubIDs []string) (*model.Memory, error)
	GetHub(id string) (*model.Hub, error)
	GetHubMemberRole(hubID, userID string) (string, error)
	DeleteMemory(id, ownerID string) error
	DeleteHubMemory(id, hubID string) error
	ListMemoryAttachments(memoryID, ownerID string) ([]model.MemoryAttachment, error)
	// CountMemoriesByHub + ClearHubOverLimit power the
	// over-limit-clear path. Without them a chat-forget that
	// brings a frozen hub back under cap leaves it frozen
	// forever — push-path short-circuits frozen hubs BEFORE the
	// recheck (memories.go:632), so subsequent pushes can't be
	// the trigger. Codex Phase 3.6b3e v2 [Medium] flagged this.
	CountMemoriesByHub(ctx context.Context, hubID string) (int, error)
	ClearHubOverLimit(ctx context.Context, hubID string) (bool, error)
}

// chatForgetMeter is the meter surface chatForgetAdapter
// uses to keep quota counters in sync with REST/MCP delete
// paths. Phase 3.6b3e v2 codex review caught that v1
// bypassed these adjustments — chat deletes would leave
// counters stale, blocking the user from pushing more
// memories until the counters were manually reset.
//
// Nil meter means "skip counter adjustments" (dev/staging
// without Redis); the row delete still happens, but the
// counters drift. Production wiring must pass a real meter.
type chatForgetMeter interface {
	AdjustMemoryCount(ctx context.Context, ownerID string, delta int)
	AdjustHubMemoryCount(ctx context.Context, hubID string, delta int)
	AdjustStorageBytes(ctx context.Context, ownerID string, delta int64)
}

// chatForgetEvents is the event-publisher surface used to
// invalidate UIs on a forget. Mirrors the REST handler's
// publishMemoryChanged behavior so a chat-forget triggers
// the same `hub.memories.changed` event the REST DELETE
// path emits — without it, list views on other tabs/devices
// stay stale until the next manual refresh.
type chatForgetEvents interface {
	Publish(ctx context.Context, evt events.Event) error
}

// chatForgetObjectStore is the object-storage surface needed to
// delete attachment blobs after a memory's row is removed.
// REST handler.Delete calls deleteAttachmentObjects on the same
// snapshot we use for byte-counting; Phase 3.6b3e v2 codex
// flagged that v1 of this adapter skipped that step, leaving
// orphaned R2 objects after every chat-approved delete.
//
// Nil object store means "skip blob delete" (dev without R2);
// the row + counters still update.
type chatForgetObjectStore interface {
	Delete(ctx context.Context, key string) error
}

// chatForgetHubQuota is the plan-resolver surface used to read
// a hub's memory limit when deciding whether the post-delete
// count clears the over_limit_since marker. Same shape REST's
// MemoriesHandler.hubQuota uses.
//
// Nil hub quota means "skip the over-limit clear" (the marker
// stays set; admin/billing-side intervention required to
// unfreeze). Production wiring must pass a real resolver.
type chatForgetHubQuota interface {
	GetHubMemoryLimit(ctx context.Context, hubID string) int
}

// chatForgetAdapter implements tools.ForgetService for the
// chat worker. Sequence:
//
//  1. Resolve the memory by (id, scope.HubIDs) — strict-hub
//     read. Out-of-scope or missing → ErrForgetMemoryNotFound
//     (same surface either way; the security contract is "the
//     agent can't probe existence across the scope boundary").
//  2. Authorize: if the caller IS the memory owner, allow. If
//     not (team-hub case), check role + hub policy via
//     canDeleteMemory mirror.
//  3. Hard delete: DeleteMemory for the owner case;
//     DeleteHubMemory for the team-hub non-owner case. Both
//     issue `DELETE FROM memories` — the row is gone after
//     this returns. Audit lives in chat_tool_approvals
//     (args + decided_at + decided_by persist independently).
//
// **Concurrent-delete race.** A second forget for the same
// memory in flight: the second store call returns NotFound
// (the row is already gone). The tool surface returns
// ErrForgetMemoryNotFound to the agent. This is the right
// shape — the user's intent (forget M) is satisfied; surfacing
// "M doesn't exist (anymore)" is an honest answer.
type chatForgetAdapter struct {
	store       chatForgetStore
	meter       chatForgetMeter       // optional; nil = skip counter adjustments
	events      chatForgetEvents      // optional; nil = no UI invalidation event
	objectStore chatForgetObjectStore // optional; nil = skip blob delete (dev without R2)
	hubQuota    chatForgetHubQuota    // optional; nil = skip over-limit-clear (frozen hub stays frozen)
}

// isStoreNotFound returns true when err looks like a "memory
// not found" outcome from the store layer. The store's
// scanMemoryWithAuthor wraps pgx.ErrNoRows as
// "memory not found: <pgx err>"; the InMemoryStore returns a
// similar error. We do a string-suffix check rather than
// errors.Is(err, pgx.ErrNoRows) because:
//
//   - Workerapp doesn't otherwise import pgx for error
//     sentinels (it uses the *river.Client[pgx.Tx] generic
//     parameter, but the pgx package itself isn't direct).
//   - Both Postgres and InMemory paths produce a "memory not
//     found" prefix; substring matching matches both without
//     needing to import either's specific sentinel.
//
// Codex's Phase 3.6b3e v1 review caught that v1 expected
// (nil, nil) on miss, which neither implementation produces.
// This helper bridges the contract.
func isStoreNotFound(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "memory not found")
}

// canDeleteMemoryRoleNonOwner mirrors handler.canDeleteMemory's
// non-owner branch. Caller is NOT the memory's owner; this
// covers the team-hub-non-owner-deletes-someone-else's-memory
// case. Owner/admin always; viewer never; contributor ONLY
// when hub.ContributorDeletePolicy == "any".
//
// **The "own" policy explicitly rejects in this branch.**
// "own" means a contributor can delete only memories they
// themselves authored — but this code path is the non-owner
// branch (memory.OwnerID != caller). So "own" must reject.
// Codex's Phase 3.6b3e v1 review caught the drift: v1 treated
// every policy except "none" as permissive, which silently
// widened "own" to "any".
//
// Local copy because handler can't be imported from workerapp;
// the parity is enforced via the chat_forget_adapter_test
// covering each policy value explicitly.
func canDeleteMemoryRoleNonOwner(role string, hub *model.Hub) bool {
	switch role {
	case "owner", "admin":
		return true
	case "contributor":
		// "any"  → unrestricted contributor delete on the hub.
		// "own"  → contributor can delete only their own
		//          memories. We're in the non-owner branch,
		//          so reject.
		// "none" / anything else → reject.
		return hub != nil && hub.ContributorDeletePolicy == "any"
	}
	return false
}

func (a *chatForgetAdapter) ForgetMemory(ctx context.Context, in tools.ForgetMemoryRequest) (tools.ForgetOutcome, error) {
	if a == nil || a.store == nil {
		return tools.ForgetOutcome{}, fmt.Errorf("chatForgetAdapter: store is required")
	}
	if in.OwnerID == "" || in.MemoryID == "" {
		return tools.ForgetOutcome{}, fmt.Errorf("chatForgetAdapter: empty OwnerID or MemoryID")
	}
	if len(in.AllowedHubIDs) == 0 {
		// Defensive — the tool wrapper rejects empty scopes,
		// but a future caller bypassing it shouldn't get the
		// store layer's owner-only fallback.
		return tools.ForgetOutcome{}, tools.ErrForgetMemoryNotFound
	}

	memory, err := a.store.GetMemoryInHubs(ctx, in.MemoryID, in.AllowedHubIDs)
	if err != nil {
		// Postgres scanMemoryWithAuthor wraps pgx.ErrNoRows
		// as "memory not found: <pgx err>" — unwrap and map
		// to our sentinel. InMemoryStore returns a similar
		// not-found error. Codex's Phase 3.6b3e v1 review
		// caught that v1 expected (nil, nil) on miss, which
		// neither implementation actually produces — so
		// missing/out-of-scope memories surfaced as a
		// generic tool failure instead of a clean
		// ErrForgetMemoryNotFound.
		if isStoreNotFound(err) {
			return tools.ForgetOutcome{}, tools.ErrForgetMemoryNotFound
		}
		return tools.ForgetOutcome{}, fmt.Errorf("get memory: %w", err)
	}
	if memory == nil {
		return tools.ForgetOutcome{}, tools.ErrForgetMemoryNotFound
	}

	// Authorization. Owner can always delete; non-owner needs
	// role + hub-policy clearance.
	isOwner := memory.OwnerID == in.OwnerID
	if !isOwner {
		role, err := a.store.GetHubMemberRole(memory.HubID, in.OwnerID)
		if err != nil {
			return tools.ForgetOutcome{}, fmt.Errorf("get hub role: %w", err)
		}
		if role == "" {
			return tools.ForgetOutcome{}, tools.ErrForgetMemoryNoPermission
		}
		hub, err := a.store.GetHub(memory.HubID)
		if err != nil {
			return tools.ForgetOutcome{}, fmt.Errorf("get hub: %w", err)
		}
		if !canDeleteMemoryRoleNonOwner(role, hub) {
			return tools.ForgetOutcome{}, tools.ErrForgetMemoryNoPermission
		}
	}

	// Snapshot attachments BEFORE the delete cascade removes
	// the rows. We need (a) byte totals for AdjustStorageBytes,
	// and (b) StorageKey list for the post-delete blob purge.
	var (
		freedBytes  int64
		attachments []model.MemoryAttachment
	)
	if list, attErr := a.store.ListMemoryAttachments(in.MemoryID, memory.OwnerID); attErr == nil {
		attachments = list
		for _, att := range attachments {
			freedBytes += att.SizeBytes
		}
	}

	// Hard delete (the store does `DELETE FROM memories`, not
	// state='deleted' — codex's Phase 3.6b3e v1 review caught
	// that v1's comments claimed tombstone semantics that
	// don't actually exist at the store layer; the audit
	// trail lives in chat_tool_approvals, which persists
	// args + decided_at + decided_by even after the row is
	// gone).
	if isOwner {
		if err := a.store.DeleteMemory(in.MemoryID, in.OwnerID); err != nil {
			if isStoreNotFound(err) {
				return tools.ForgetOutcome{}, tools.ErrForgetMemoryNotFound
			}
			return tools.ForgetOutcome{}, fmt.Errorf("delete memory: %w", err)
		}
	} else {
		if err := a.store.DeleteHubMemory(in.MemoryID, memory.HubID); err != nil {
			if isStoreNotFound(err) {
				return tools.ForgetOutcome{}, tools.ErrForgetMemoryNotFound
			}
			return tools.ForgetOutcome{}, fmt.Errorf("delete hub memory: %w", err)
		}
	}

	// Side effects matching the REST DELETE path. Codex Phase
	// 3.6b3e v1+v2 walked us through every divergence: meter
	// counters, hub.memories.changed event, R2 blob purge, and
	// the over_limit_since clear. Each is wired below; nil
	// dependencies degrade gracefully (dev environments without
	// Redis/R2 still complete the delete, just without the
	// corresponding side effect).
	//
	// The MEMORY's owner — not the acting user — receives the
	// quota refund: a team-hub delete by a contributor frees
	// the original author's storage budget, which is the
	// invariant REST handler.Delete also enforces.
	if a.meter != nil {
		a.meter.AdjustMemoryCount(ctx, memory.OwnerID, -1)
		a.meter.AdjustHubMemoryCount(ctx, memory.HubID, -1)
		if freedBytes > 0 {
			a.meter.AdjustStorageBytes(ctx, memory.OwnerID, -freedBytes)
		}
		// Re-evaluate hub freeze. REST does this via
		// maybeClearHubOverLimit; we inline the check here
		// because workerapp can't call into handler. Without
		// it, a chat-forget that brings a frozen hub back
		// under cap leaves the freeze marker set — and the
		// push path short-circuits frozen hubs BEFORE the
		// recheck (memories.go:632), so subsequent pushes
		// can't be the trigger to clear it. Codex Phase
		// 3.6b3e v2 [Medium] flagged this. The hub_restored
		// notification dispatch (REST path's tail end) is
		// intentionally NOT replicated here — it's a UX
		// nicety, not a correctness gate; the REST owner-flow
		// or a billing-side restore covers it. The freeze
		// flag clear is what unblocks pushes; that's the fix.
		a.maybeClearHubOverLimitInline(ctx, memory.HubID)
	}
	a.deleteAttachmentBlobs(ctx, attachments)
	if a.events != nil {
		// Same envelope shape the REST DELETE path uses —
		// hub.memories.changed broadcasts to every member of
		// the hub so list views invalidate.
		_ = a.events.Publish(ctx, events.Event{
			Type:        events.EventTypeHubMemoriesChanged,
			HubID:       memory.HubID,
			ActorID:     in.OwnerID,
			EntityID:    in.MemoryID,
			State:       "deleted",
			PrivateOnly: memory.Boundary == "private",
		})
	}

	slog.InfoContext(ctx, "chat forget: memory deleted",
		"memory_id", in.MemoryID,
		"hub_id", memory.HubID,
		"owner_id", memory.OwnerID,
		"acting_user_id", in.OwnerID,
		"reason", in.Reason,
		"freed_bytes", freedBytes,
	)

	return tools.ForgetOutcome{
		Status: tools.ForgetStatusForgotten,
		HubID:  memory.HubID,
		Title:  memory.Title,
	}, nil
}

// deleteAttachmentBlobs removes object-store blobs for the
// memory's attachment snapshot. Mirrors REST handler.Delete →
// deleteAttachmentObjects (memories.go:2763). Best-effort: a
// failed delete logs and continues — the row is already gone,
// so there's no recovery path; the orphan is at worst a
// cents-per-month storage leak that the periodic blob-GC
// sweeper will catch.
func (a *chatForgetAdapter) deleteAttachmentBlobs(ctx context.Context, attachments []model.MemoryAttachment) {
	if a.objectStore == nil || len(attachments) == 0 {
		return
	}
	for _, att := range attachments {
		key := strings.TrimSpace(att.StorageKey)
		if key == "" {
			continue
		}
		if err := a.objectStore.Delete(ctx, key); err != nil {
			slog.WarnContext(ctx, "chat forget: delete attachment blob failed",
				"attachment_id", att.ID,
				"memory_id", att.MemoryID,
				"storage_key", key,
				"error", err)
		}
	}
}

// maybeClearHubOverLimitInline mirrors handler.MemoriesHandler
// .maybeClearHubOverLimit (memories.go:2688) for the chat path.
// Inlined because workerapp can't import handler. Skips the
// hub_restored notification dispatch — that's a UX nicety, not
// a correctness gate (REST owner-flow + billing-side restore
// still cover it). The marker clear is what unblocks pushes.
//
// Best-effort throughout: count failures and clear failures
// log + return rather than fail the delete itself, since the
// memory row is already gone.
func (a *chatForgetAdapter) maybeClearHubOverLimitInline(ctx context.Context, hubID string) {
	if hubID == "" || a.hubQuota == nil {
		return
	}
	limit := a.hubQuota.GetHubMemoryLimit(ctx, hubID)
	if !model.IsUnlimited(limit) {
		count, err := a.store.CountMemoriesByHub(ctx, hubID)
		if err != nil {
			slog.WarnContext(ctx, "chat forget: count hub memories failed; leaving over_limit_since in place",
				"hub_id", hubID, "error", err)
			return
		}
		if count > limit {
			return
		}
	}
	if _, err := a.store.ClearHubOverLimit(ctx, hubID); err != nil {
		slog.WarnContext(ctx, "chat forget: clear hub over_limit_since failed",
			"hub_id", hubID, "error", err)
	}
}

// ─────────────────────────────────────────────────────────────
// Phase 3.6b3f — propose_topic_merge production adapter.
// ─────────────────────────────────────────────────────────────

// chatProposeTopicMergeStore is the small store surface the
// chat propose_topic_merge adapter calls into. Defining it
// inline (vs embedding *store.PostgresStore directly) keeps
// adapter tests decoupled from Postgres setup, matching the
// pattern chatPushAdapter and chatForgetAdapter follow.
type chatProposeTopicMergeStore interface {
	GetHub(id string) (*model.Hub, error)
	GetHubMemberRole(hubID, userID string) (string, error)
	GetTopic(id string, hubID string) (*model.Topic, error)
	// CountMemoriesByTopic returns per-topic memory counts in
	// the hub, scoped to the visibility a team-member sees
	// (own + shared). The map is keyed by topic_id; absent
	// keys mean zero memories. Same shape the dream engine
	// reads via topicCounts.
	CountMemoriesByTopic(scope store.VisibilityScope, hubID string) (map[string]int, error)
	CreateNotificationIfAbsent(n *model.Notification) (bool, error)
	// GetNotificationBySourceKey lets the adapter read back the
	// EXISTING notification row when CreateNotificationIfAbsent
	// returns inserted=false. Codex Phase 3.6b3f v1 caught two
	// related bugs that this fixes:
	//   (a) the wire result was returning the locally-generated
	//       UUID, not the actual existing row's id;
	//   (b) every (source_kind, source_id) conflict was being
	//       reported as "already_pending" even when the prior
	//       row was resolved or dismissed.
	GetNotificationBySourceKey(ctx context.Context, sourceKind, sourceID string) (*model.Notification, error)
}

// chatProposeTopicMergeEvents is the event-publisher surface
// used to fan out the realtime `notification.created` event so
// the user's inbox UI updates immediately. Mirrors the dream
// engine's posture (events.PublishNotificationCreated). Nil
// publisher means "skip the event" — the row write still
// succeeds; the UI catches up on the next poll.
//
// Modeled as the existing events.Publisher interface
// (Publish(ctx, Event) error) directly, matching
// chatForgetAdapter's `events` field. Same late-bind pattern
// in NewApp.
type chatProposeTopicMergeEvents = events.Publisher

// chatProposeTopicMergeAdapter implements
// tools.ProposeTopicMergeService. Sequence:
//
//  1. Verify the caller's role on the hub (owner/admin always;
//     contributor only when AllowContributorTopics; viewer
//     never; non-member never).
//  2. Verify each (target, sources...) topic exists IN THIS
//     HUB. The (id, hub_id) lookup naturally rejects
//     cross-hub topics — same fail-closed shape forget_memory
//     uses for cross-scope memory probes (no existence-probe
//     leak across the scope boundary).
//  3. Build the canonical source_id from
//     "topic_merge:<targetID>:<sortedSourceIDs>:<recipient>"
//     so repeated proposals — by chat OR by the dream engine
//     — dedupe to the same notification row via the
//     (source_kind, source_id) partial unique index.
//  4. CreateNotificationIfAbsent — true ⇒ fresh row written
//     (publish notification.created); false ⇒ existing
//     pending row matched (suppress the event so the UI
//     doesn't double-ping the user for a re-suggestion).
//
// The adapter never calls store.MergeTopics. The actual merge
// is the user's job via the notifications-resolve API.
type chatProposeTopicMergeAdapter struct {
	store  chatProposeTopicMergeStore
	events chatProposeTopicMergeEvents // optional; nil = skip notification.created event
}

// mapExistingNotificationToProposeStatus translates an existing
// review_topic_merge notification's (Status, Resolution) tuple
// into the propose_topic_merge tool's wire status enum. Returns
// "" for an unhandled combination — the caller must treat that
// as an explicit error (don't fall back to a generic
// "already_pending" that would mislead the user about pending
// review work).
//
// The two-axis mapping captures what topic-merge actually does:
//   - status=pending → user hasn't decided yet → already_pending.
//   - status=resolved + resolution=merged → user applied the
//     merge → already_merged.
//   - status=resolved + resolution=kept_separate → user
//     rejected the merge → already_kept_separate.
//   - status=resolved + resolution=dismissed → user dismissed
//     the proposal → already_dismissed.
//   - status=resolved + resolution={anything else} → defensive
//     catch-all → already_resolved.
//   - status=dismissed → rare receipt-style dismiss path →
//     already_dismissed.
//   - status=expired → grace window expired → already_expired.
func mapExistingNotificationToProposeStatus(n *model.Notification) string {
	if n == nil {
		return ""
	}
	switch n.Status {
	case model.NotificationStatusPending:
		return tools.ProposeStatusAlreadyPending
	case model.NotificationStatusResolved:
		switch n.Resolution {
		case model.ResolutionMerged:
			return tools.ProposeStatusAlreadyMerged
		case model.ResolutionKeptSeparate:
			return tools.ProposeStatusAlreadyKeptSeparate
		case model.ResolutionDismissed:
			return tools.ProposeStatusAlreadyDismissed
		case "":
			// Resolved without a recorded resolution. Treat as
			// the catch-all rather than guessing.
			return tools.ProposeStatusAlreadyResolved
		default:
			// Future resolution value — surface as the
			// catch-all so the agent gets a meaningful
			// answer; the warn log in the caller flags the
			// gap for follow-up.
			return tools.ProposeStatusAlreadyResolved
		}
	case model.NotificationStatusDismissed:
		return tools.ProposeStatusAlreadyDismissed
	case model.NotificationStatusExpired:
		return tools.ProposeStatusAlreadyExpired
	}
	return ""
}

// canProposeTopicMergeRole mirrors the REST topics-handler
// role gate. Owner / admin always; contributor only when the
// hub's AllowContributorTopics flag is set; viewer never.
// Non-member is the role == "" case (rejected naturally).
func canProposeTopicMergeRole(role string, hub *model.Hub) bool {
	switch role {
	case "owner", "admin":
		return true
	case "contributor":
		return hub != nil && hub.AllowContributorTopics
	}
	return false
}

func (a *chatProposeTopicMergeAdapter) ProposeTopicMerge(ctx context.Context, in tools.ProposeTopicMergeRequest) (tools.ProposeTopicMergeOutcome, error) {
	if a == nil || a.store == nil {
		return tools.ProposeTopicMergeOutcome{}, fmt.Errorf("chatProposeTopicMergeAdapter: store is required")
	}
	if in.OwnerID == "" || in.HubID == "" || in.TargetTopicID == "" {
		return tools.ProposeTopicMergeOutcome{}, fmt.Errorf("chatProposeTopicMergeAdapter: empty OwnerID, HubID, or TargetTopicID")
	}
	if len(in.SourceTopicIDs) == 0 {
		return tools.ProposeTopicMergeOutcome{}, tools.ErrProposeTopicMergeInvalidArgs
	}
	// Defensive: the wrapper canonicalizes already, but a
	// future caller bypassing it shouldn't slip through with
	// target appearing in sources.
	for _, s := range in.SourceTopicIDs {
		if s == in.TargetTopicID {
			return tools.ProposeTopicMergeOutcome{}, tools.ErrProposeTopicMergeInvalidArgs
		}
	}

	// Hub + role gate.
	hub, err := a.store.GetHub(in.HubID)
	if err != nil || hub == nil {
		// Hub disappeared / not visible. Same fail-closed
		// surface — don't leak existence.
		return tools.ProposeTopicMergeOutcome{}, tools.ErrProposeTopicMergeNotFound
	}
	role, err := a.store.GetHubMemberRole(in.HubID, in.OwnerID)
	if err != nil {
		return tools.ProposeTopicMergeOutcome{}, fmt.Errorf("get hub role: %w", err)
	}
	if role == "" {
		// Non-member. The chat session's scope said this hub
		// is reachable but membership has changed; deny.
		return tools.ProposeTopicMergeOutcome{}, tools.ErrProposeTopicMergeNoPermission
	}
	if !canProposeTopicMergeRole(role, hub) {
		return tools.ProposeTopicMergeOutcome{}, tools.ErrProposeTopicMergeNoPermission
	}

	// Target lookup. (id, hub_id) — the hub bind is what
	// rejects cross-hub probes. Folded into NotFound to
	// avoid existence leak.
	targetTopic, err := a.store.GetTopic(in.TargetTopicID, in.HubID)
	if err != nil || targetTopic == nil {
		return tools.ProposeTopicMergeOutcome{}, tools.ErrProposeTopicMergeNotFound
	}
	// Source lookups. Each must exist in the same hub.
	// Postgres UUID columns are case-insensitive on read but
	// preserve canonical form on output, so building the
	// source_id from the RESOLVED Topic.ID (rather than the
	// caller's raw arg) normalizes uppercase/lowercase variants
	// onto a single dedup key. Codex Phase 3.6b3f v1 [Medium]
	// flagged this — without it, the same merge proposed with
	// different casing produced different source_ids and
	// surfaced as two notification rows.
	sourceTopics := make([]*model.Topic, 0, len(in.SourceTopicIDs))
	sourceTitles := make([]string, 0, len(in.SourceTopicIDs))
	resolvedSourceIDs := make([]string, 0, len(in.SourceTopicIDs))
	for _, sid := range in.SourceTopicIDs {
		src, err := a.store.GetTopic(sid, in.HubID)
		if err != nil || src == nil {
			return tools.ProposeTopicMergeOutcome{}, tools.ErrProposeTopicMergeNotFound
		}
		sourceTopics = append(sourceTopics, src)
		sourceTitles = append(sourceTitles, src.Name)
		resolvedSourceIDs = append(resolvedSourceIDs, src.ID)
	}
	// Re-check target-in-sources after topic resolution (defense
	// in depth — codex's [Medium] note: case-folded UUID input
	// could bypass the wrapper's pre-resolution check). Same
	// invariant as the wrapper, applied to canonical IDs.
	for _, rid := range resolvedSourceIDs {
		if rid == targetTopic.ID {
			return tools.ProposeTopicMergeOutcome{}, tools.ErrProposeTopicMergeInvalidArgs
		}
	}
	// Sort the resolved IDs so the source_id is stable
	// regardless of caller order. The wrapper already sorts the
	// raw input; this re-sort is the canonical-form sort that
	// matters for dedup.
	sort.Strings(resolvedSourceIDs)

	// Build the payload. Topic memory counts are populated
	// best-effort — the inbox surface renders them as a hint
	// ("merging 3 topics holding 12 memories total"). A
	// failed count returns an empty map; per-topic lookups
	// fall through to zero.
	//
	// Scope is the acting user's view of the hub: own +
	// shared. This is the same lens the REST topics-list
	// endpoint uses, so the count the notification displays
	// matches what the user would see on /topics for the
	// same hub.
	countScope := store.VisibilityScope{
		OwnerID: in.OwnerID,
		HubIDs:  []string{in.HubID},
	}
	topicCounts, _ := a.store.CountMemoriesByTopic(countScope, in.HubID)
	sourceRefs := make([]model.ReviewTopicRef, 0, len(sourceTopics))
	for _, src := range sourceTopics {
		sourceRefs = append(sourceRefs, model.ReviewTopicRef{
			ID:          src.ID,
			Name:        src.Name,
			MemoryCount: topicCounts[src.ID],
		})
	}
	targetCount := topicCounts[targetTopic.ID]

	payloadBytes, err := json.Marshal(model.ReviewTopicMergePayload{
		TargetTopic: model.ReviewTopicRef{
			ID:          targetTopic.ID,
			Name:        targetTopic.Name,
			MemoryCount: targetCount,
		},
		SourceTopics: sourceRefs,
		Reason:       in.Reason,
	})
	if err != nil {
		return tools.ProposeTopicMergeOutcome{}, fmt.Errorf("marshal topic_merge payload: %w", err)
	}

	// Stable source_id via the shared helper in model — same
	// shape the dream engine uses, so a chat-proposed merge and
	// a dream-proposed merge with the same (target, sources,
	// recipient) collapse onto a single inbox row via the
	// notifications-source-unique constraint. Codex Phase
	// 3.6b3f v2 [Medium] caught that pre-share dreams included
	// the target in its source list while chat filtered it out,
	// producing two distinct rows for the same merge — the
	// shared helper filters target out either way.
	//
	// Built from RESOLVED Topic.IDs (canonical UUID form) so
	// casing variants in the agent's input dedupe onto a
	// single key.
	srcID := model.BuildTopicMergeNotificationSourceID(targetTopic.ID, resolvedSourceIDs, in.OwnerID)

	notif := &model.Notification{
		ID:              uuid.NewString(),
		Audience:        model.AudienceUser,
		RecipientUserID: in.OwnerID,
		HubID:           in.HubID,
		Kind:            model.NotificationKindReviewTopicMerge,
		Status:          model.NotificationStatusPending,
		Priority:        0,
		SourceKind:      model.NotificationSourceTopicReview,
		SourceID:        srcID,
		Payload:         payloadBytes,
		CreatedAt:       time.Now().UTC(),
	}
	inserted, err := a.store.CreateNotificationIfAbsent(notif)
	if err != nil {
		return tools.ProposeTopicMergeOutcome{}, fmt.Errorf("create topic_merge notification: %w", err)
	}

	// Resolve the wire result. Codex Phase 3.6b3f v1 caught two
	// related defects this block fixes:
	//
	//   (a) [High] On insert=false, returning notif.ID was
	//       returning the LOCALLY-generated UUID — not the
	//       existing row's id. The user clicking the inbox row
	//       would 404. Fix: read the existing row by source key
	//       and surface its actual id.
	//
	//   (b) [High] The DB unique constraint is global on
	//       (source_kind, source_id), not pending-scoped. A
	//       resolved or dismissed prior proposal would block
	//       insert AND surface as "already_pending" — the agent
	//       would tell the user they have pending review work
	//       when actually they already handled it. Fix: read
	//       the existing row's actual status and map to the
	//       matching ProposeStatusAlready* constant.
	//
	// On insert=true, neither concern applies — we own the row
	// we just wrote.
	resultID := notif.ID
	resultStatus := tools.ProposeStatusProposed
	if !inserted {
		existing, lookupErr := a.store.GetNotificationBySourceKey(ctx, model.NotificationSourceTopicReview, srcID)
		// Inconsistent state guard. Codex Phase 3.6b3f v2 [Low]
		// flagged that the prior single %w branch produced an
		// ugly nil-wrap message for a (nil, nil) bad-store
		// return. Split: wrap only when there's an actual
		// underlying error.
		if lookupErr != nil {
			return tools.ProposeTopicMergeOutcome{}, fmt.Errorf("propose_topic_merge: lookup after insert conflict failed for source_id %q: %w", srcID, lookupErr)
		}
		if existing == nil {
			return tools.ProposeTopicMergeOutcome{}, fmt.Errorf("propose_topic_merge: insert reported conflict but lookup found no row for source_id %q (store invariant violation)", srcID)
		}
		// Defense in depth: GetNotificationBySourceKey doesn't
		// scope by recipient/hub — the caller (here) owns that
		// policy. Codex v2 noted that since the method is on
		// the broad Store interface, we should assert. The
		// source_id encodes the recipient suffix already, so a
		// mismatch here means the global key collided across
		// users — never expected, but worth the cheap check.
		if existing.RecipientUserID != in.OwnerID {
			slog.ErrorContext(ctx, "chat propose_topic_merge: source_id collision across recipients",
				"source_id", srcID, "expected_recipient", in.OwnerID, "got_recipient", existing.RecipientUserID)
			return tools.ProposeTopicMergeOutcome{}, fmt.Errorf("propose_topic_merge: source_id collision (expected recipient %q, got %q)", in.OwnerID, existing.RecipientUserID)
		}
		if existing.HubID != in.HubID {
			slog.ErrorContext(ctx, "chat propose_topic_merge: source_id collision across hubs",
				"source_id", srcID, "expected_hub", in.HubID, "got_hub", existing.HubID)
			return tools.ProposeTopicMergeOutcome{}, fmt.Errorf("propose_topic_merge: source_id collision (expected hub %q, got %q)", in.HubID, existing.HubID)
		}

		resultID = existing.ID
		// Resolution-aware status mapping. Codex Phase 3.6b3f
		// v2 [Medium] flagged that topic-merge reviews have
		// THREE meaningful resolved-state values
		// (merged / kept_separate / dismissed) and the agent
		// needs to phrase its response differently for each.
		// The store's status=dismissed path is rare for review
		// rows (dismissals normally land via /resolve with
		// resolution=dismissed, leaving status=resolved); we
		// cover both code paths here.
		resultStatus = mapExistingNotificationToProposeStatus(existing)
		if resultStatus == "" {
			// Unknown status × resolution combo. Codex v2 said
			// don't fall back to "already_pending" — that lies.
			// Surface as an explicit error so the agent doesn't
			// claim review work that may not actually be
			// pending.
			slog.WarnContext(ctx, "chat propose_topic_merge: unknown existing-notification status/resolution combo",
				"status", existing.Status, "resolution", existing.Resolution, "notification_id", existing.ID, "source_id", srcID)
			return tools.ProposeTopicMergeOutcome{}, fmt.Errorf("propose_topic_merge: existing notification has unhandled status %q resolution %q (id=%s)", existing.Status, existing.Resolution, existing.ID)
		}
	} else if a.events != nil {
		// Only publish the realtime event on a fresh insert.
		// On dedup-retries, the user already saw the notification;
		// re-emitting would re-ping their inbox UI for stale work.
		events.PublishNotificationCreated(ctx, a.events, notif)
	}

	slog.InfoContext(ctx, "chat propose_topic_merge",
		"status", resultStatus,
		"notification_id", resultID,
		"hub_id", in.HubID,
		"target_topic_id", in.TargetTopicID,
		"source_topic_count", len(in.SourceTopicIDs),
		"acting_user_id", in.OwnerID,
	)

	return tools.ProposeTopicMergeOutcome{
		Status:         resultStatus,
		NotificationID: resultID,
		TargetTitle:    targetTopic.Name,
		SourceTitles:   sourceTitles,
	}, nil
}

// ─────────────────────────────────────────────────────────────
// Phase 3.6b3g — fetch_url production adapter.
// ─────────────────────────────────────────────────────────────

// chatFetchURLClient is the safefetch surface the chat fetch
// adapter uses. Defining it as an interface (vs embedding a
// concrete *safefetch.Client) keeps the adapter testable
// without spinning up a real HTTP client — same pattern push /
// forget / propose adapters follow.
type chatFetchURLClient interface {
	Fetch(ctx context.Context, rawURL string) (*safefetch.FetchResult, error)
}

// chatFetchURLAdapter implements tools.FetchURLService for the
// chat worker. Sequence:
//
//  1. Hand the URL to safefetch.Client.Fetch — that does
//     URL-level validation, dial-time IP block, redirect
//     re-validation, body cap, timeout. We don't second-
//     guess any of it.
//  2. Map URL-level rejections to ErrFetchURLBlocked so the
//     tool wrapper can give the agent a generic "blocked"
//     error rather than leaking the specific reason. Network
//     failures (DNS, dial, timeout) map to
//     ErrFetchURLNetworkFailed so the agent can distinguish
//     terminal-by-design from retryable.
//  3. Best-effort extract <title> from HTML responses so the
//     agent gets a useful one-liner alongside the raw body.
//     A failed title extract returns empty title, not an
//     error.
//  4. Surface the truncated body as a UTF-8 string. Bytes
//     that aren't valid UTF-8 are passed through with a
//     fallback note in a future improvement; for v1 we trust
//     the safefetch byte cap.
//
// The adapter does NOT cache, NOT auth, NOT rate-limit. Those
// are higher-level concerns — caching would be a per-session
// memo (chat layer), auth doesn't apply to public web fetch,
// and rate-limiting per session is a future addition.
type chatFetchURLAdapter struct {
	client chatFetchURLClient
}

func (a *chatFetchURLAdapter) FetchURL(ctx context.Context, in tools.FetchURLRequest) (tools.FetchURLOutcome, error) {
	if a == nil || a.client == nil {
		return tools.FetchURLOutcome{}, fmt.Errorf("chatFetchURLAdapter: client is required")
	}
	if in.URL == "" {
		return tools.FetchURLOutcome{}, fmt.Errorf("chatFetchURLAdapter: empty URL")
	}

	res, err := a.client.Fetch(ctx, in.URL)
	if err != nil {
		// URL-level rejection (scheme, userinfo, blocked
		// host, blocked literal IP) — terminal for this URL.
		// Each safefetch sentinel maps to ErrFetchURLBlocked.
		// Codex Phase 3.6b3g v1 [Medium] flagged that
		// err.Error() leaks query strings (presigned S3 URLs,
		// tokenized public links). Log the safefetch sentinel
		// only — the wrapped err.Error() includes the URL.
		switch {
		case errors.Is(err, safefetch.ErrEmptyURL),
			errors.Is(err, safefetch.ErrParseURL),
			errors.Is(err, safefetch.ErrURLTooLong),
			errors.Is(err, safefetch.ErrUnsupportedScheme),
			errors.Is(err, safefetch.ErrUserinfoNotAllowed),
			errors.Is(err, safefetch.ErrEmptyHost),
			errors.Is(err, safefetch.ErrBlockedHost),
			errors.Is(err, safefetch.ErrBlockedIP):
			slog.InfoContext(ctx, "chat fetch_url: blocked",
				"acting_user_id", in.OwnerID,
				"reason", safefetchSentinelName(err),
				"url_redacted", redactURLForLog(in.URL),
			)
			return tools.FetchURLOutcome{}, tools.ErrFetchURLBlocked
		}
		// Anything else is a network-layer failure (dial,
		// timeout, server disconnect). Log the underlying
		// error class — but redact the URL because url.Error
		// wraps the FULL request URL (with query) into its
		// .Error() string and that string leaks into the log
		// otherwise.
		slog.WarnContext(ctx, "chat fetch_url: network failed",
			"acting_user_id", in.OwnerID,
			"error_class", networkErrorClass(err),
			"url_redacted", redactURLForLog(in.URL),
		)
		return tools.FetchURLOutcome{}, tools.ErrFetchURLNetworkFailed
	}

	// Title extraction runs on the FULL safefetch body (up to
	// 5MB) — codex Phase 3.6b3g v2 noted that bloated heads
	// can carry the <title> past any adapter-level body cap,
	// and the scan is cheap enough to run on the full slice.
	// This must happen BEFORE the body cap below so we don't
	// miss the title.
	title := ""
	if isHTMLContentType(res.ContentType) {
		title = extractHTMLTitle(res.Body)
	}

	// Adapter-level body cap. The wrapper's
	// marshalFetchURLResult does the FINAL JSON-aware cap
	// (codex Phase 3.6b3g v2 [High] caught that a fixed-byte
	// cap here doesn't account for JSON escape expansion of
	// `<`/`>`/`&`/control chars). This cap is a coarse first
	// pass — keeps the JSON marshal call cheap when bodies
	// are very large; the wrapper handles the precise cap.
	body := res.Body
	adapterTruncated := res.Truncated
	if int64(len(body)) > chatFetchURLBodyCap {
		body = body[:chatFetchURLBodyCap]
		adapterTruncated = true
	}

	slog.InfoContext(ctx, "chat fetch_url: fetched",
		"acting_user_id", in.OwnerID,
		"final_url_redacted", redactURLForLog(res.FinalURL),
		"status_code", res.StatusCode,
		"content_type", res.ContentType,
		"body_bytes", len(body),
		"truncated", adapterTruncated,
	)

	return tools.FetchURLOutcome{
		FinalURL:    res.FinalURL,
		StatusCode:  res.StatusCode,
		ContentType: res.ContentType,
		Title:       title,
		Body:        string(body),
		Truncated:   adapterTruncated,
	}, nil
}

// chatFetchURLBodyCap is the maximum body bytes the adapter
// returns to the tool wrapper. Sized to fit comfortably
// inside the SDK's 64KB MaxResultBytes — the JSON envelope
// adds ~256 bytes of structure + the metadata fields + JSON
// escaping (worst-case 6x for control chars, but real-world
// text is closer to 1.05x). A 48KB body cap leaves ~16KB of
// headroom which covers JSON escaping plus all metadata.
const chatFetchURLBodyCap = 48 * 1024

// redactURLForLog returns a log-safe form of a URL: scheme,
// host (with port), and path. Query and fragment are
// stripped because they commonly carry presigned-URL
// credentials, OAuth tokens, or session ids that we don't
// want in the log stream. Codex Phase 3.6b3g v1 [Medium]
// flagged that url.Error's .Error() string includes the
// full URL, so this helper is also used to replace the
// raw err.Error() in the log fields.
func redactURLForLog(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		// Can't parse — log the scheme prefix only, drop
		// the rest. Better than logging an unparseable
		// string that might itself be a token.
		if idx := strings.Index(rawURL, ":"); idx > 0 && idx < 16 {
			return rawURL[:idx] + "://[unparseable]"
		}
		return "[unparseable]"
	}
	host := u.Host
	if host == "" {
		host = u.Hostname()
	}
	scheme := u.Scheme
	if scheme == "" {
		scheme = "?"
	}
	path := u.Path
	if path == "" {
		path = "/"
	}
	return scheme + "://" + host + path
}

// safefetchSentinelName returns a static log-safe label for
// each safefetch sentinel. Avoids logging the wrapped err
// .Error() which can include user-supplied URL fragments.
func safefetchSentinelName(err error) string {
	switch {
	case errors.Is(err, safefetch.ErrEmptyURL):
		return "empty_url"
	case errors.Is(err, safefetch.ErrParseURL):
		return "parse_failed"
	case errors.Is(err, safefetch.ErrURLTooLong):
		return "url_too_long"
	case errors.Is(err, safefetch.ErrUnsupportedScheme):
		return "unsupported_scheme"
	case errors.Is(err, safefetch.ErrUserinfoNotAllowed):
		return "userinfo_in_url"
	case errors.Is(err, safefetch.ErrEmptyHost):
		return "empty_host"
	case errors.Is(err, safefetch.ErrBlockedHost):
		return "blocked_host"
	case errors.Is(err, safefetch.ErrBlockedIP):
		return "blocked_ip"
	}
	return "unknown"
}

// networkErrorClass returns a coarse classification for a
// network-layer failure that's safe to log. Avoids logging
// the wrapped err.Error() (which contains the full URL via
// url.Error). Distinguishes timeout / canceled from
// general "network failed" — useful enough for ops without
// leaking specifics.
func networkErrorClass(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	return "network_failed"
}

// isHTMLContentType reports whether the (already-lowercased,
// parameter-stripped) content-type indicates an HTML or XHTML
// document. Title extraction only runs on HTML to avoid the
// regex hitting a `<title>` substring in JSON or plain text.
func isHTMLContentType(ct string) bool {
	return ct == "text/html" || ct == "application/xhtml+xml"
}

// extractHTMLTitle is a tiny, allocation-light <title>
// extractor. It deliberately doesn't use a full HTML parser
// (golang.org/x/net/html or similar): the title is read from
// the first ~64KB of body bytes (which is what we cap at the
// safefetch layer), and a malformed HTML page shouldn't drag
// in a parser dependency.
//
// Rules:
//   - Case-insensitive search for "<title" (start tag).
//   - Skip past attributes to the first ">" closing the tag.
//   - Read until "</title" (case-insensitive).
//   - Trim whitespace, collapse internal whitespace runs.
//   - Cap at 256 chars (the agent's response surface doesn't
//     need a longer one).
//
// Returns empty string when no title is found or the title is
// empty after trim. Never returns an error.
func extractHTMLTitle(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	// Find the first <title (case-insensitive).
	start := indexFold(body, []byte("<title"))
	if start < 0 {
		return ""
	}
	// Skip past the opening tag's attributes to the first ">".
	openEnd := -1
	for i := start; i < len(body); i++ {
		if body[i] == '>' {
			openEnd = i
			break
		}
	}
	if openEnd < 0 {
		return ""
	}
	contentStart := openEnd + 1
	// Find the closing </title (case-insensitive) after
	// contentStart.
	rel := indexFold(body[contentStart:], []byte("</title"))
	if rel < 0 {
		return ""
	}
	raw := body[contentStart : contentStart+rel]

	// Trim + collapse whitespace runs.
	var b []byte
	prevSpace := false
	for _, c := range raw {
		isSpace := c == ' ' || c == '\t' || c == '\n' || c == '\r'
		if isSpace {
			if !prevSpace && len(b) > 0 {
				b = append(b, ' ')
			}
			prevSpace = true
			continue
		}
		prevSpace = false
		b = append(b, c)
	}
	title := string(b)
	title = strings.TrimSpace(title)
	if len(title) > 256 {
		title = title[:256]
	}
	return title
}

// indexFold returns the index of needle in haystack, ASCII
// case-insensitive. -1 when not found. Cheap byte-by-byte
// scan; the haystack here is at most 64KB and the needle is
// 6-8 bytes. No regex, no allocations.
func indexFold(haystack, needle []byte) int {
	if len(needle) == 0 || len(haystack) < len(needle) {
		return -1
	}
	first := needle[0]
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if !asciiEqualFold(haystack[i], first) {
			continue
		}
		match := true
		for j := 1; j < len(needle); j++ {
			if !asciiEqualFold(haystack[i+j], needle[j]) {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// asciiEqualFold compares two bytes, treating ASCII
// uppercase and lowercase as equal. Non-ASCII bytes match
// only exactly.
func asciiEqualFold(a, b byte) bool {
	if a == b {
		return true
	}
	if 'A' <= a && a <= 'Z' {
		a += 'a' - 'A'
	}
	if 'A' <= b && b <= 'Z' {
		b += 'a' - 'A'
	}
	return a == b
}

// chatRecallStoreAdapter narrows *store.PostgresStore to the
// `tools.agentRecallStore` interface that the production
// RecallService consumes. The adapter exists because the tools
// interface deliberately omits filters/options — the agent path
// uses default scoring lanes only.
type chatRecallStoreAdapter struct {
	s *store.PostgresStore
}

func (a chatRecallStoreAdapter) SearchChunksAgent(ctx context.Context, query string, queryEmbedding []float64, hubIDs []string, limit int) ([]model.Chunk, error) {
	return a.s.SearchChunksInHubs(ctx, query, queryEmbedding, hubIDs, limit, nil, nil)
}

func (a chatRecallStoreAdapter) GetMemoryInHubs(ctx context.Context, id string, hubIDs []string) (*model.Memory, error) {
	return a.s.GetMemoryInHubs(ctx, id, hubIDs)
}

type chatBrowseStoreAdapter struct {
	s *store.PostgresStore
}

func (a chatBrowseStoreAdapter) ListMemoriesInHubs(ctx context.Context, opts store.StrictHubListOptions) ([]model.Memory, string, int, error) {
	return a.s.ListMemoriesInHubs(ctx, opts)
}

func (a chatBrowseStoreAdapter) GetMemoryInHubs(ctx context.Context, id string, hubIDs []string) (*model.Memory, error) {
	return a.s.GetMemoryInHubs(ctx, id, hubIDs)
}

func (a chatBrowseStoreAdapter) ListUserHubs(userID string) ([]model.HubWithRole, error) {
	return a.s.ListUserHubs(userID)
}

func (a chatBrowseStoreAdapter) GetHub(id string) (*model.Hub, error) {
	return a.s.GetHub(id)
}

func (a chatBrowseStoreAdapter) ListTopics(hubID string) ([]model.Topic, error) {
	return a.s.ListTopics(hubID)
}

func (a chatBrowseStoreAdapter) CountMemoriesByTopic(scope store.VisibilityScope, hubID string) (map[string]int, error) {
	return a.s.CountMemoriesByTopic(scope, hubID)
}

func (a chatBrowseStoreAdapter) CountUnassignedMemories(scope store.VisibilityScope, hubID string) (int, error) {
	return a.s.CountUnassignedMemories(scope, hubID)
}

// Start begins River processing and the health server.
func (a *App) Start(ctx context.Context) error {
	if err := a.riverClient.Start(ctx); err != nil {
		return fmt.Errorf("start river client: %w", err)
	}

	slog.Info("memax worker started",
		"queues", []string{"default", "dreams"},
		"max_workers_default", 20,
		"max_workers_dreams", 3,
	)

	// River v0.32 doesn't populate river_client, so the admin ops
	// pulse has no way to count real worker machines. We write our
	// own row per process; see workerapp/heartbeat.go for why.
	heartbeatStop := startClientHeartbeat(ctx, a.pool)
	a.addCleanup(heartbeatStop)

	go startHealthServer(a.pool, a.stats)
	return nil
}

// Shutdown gracefully stops River and releases dependencies.
func (a *App) Shutdown(ctx context.Context) {
	if a == nil {
		return
	}
	if a.riverClient != nil {
		if err := a.riverClient.Stop(ctx); err != nil {
			slog.Error("river shutdown error", "error", err)
		}
	}
	for i := len(a.cleanups) - 1; i >= 0; i-- {
		if err := a.cleanups[i](ctx); err != nil {
			slog.Warn("worker cleanup failed", "error", err)
		}
	}
}

func (a *App) addCleanup(fn func(context.Context) error) {
	a.cleanups = append(a.cleanups, fn)
}

func (a *App) addClose(closeFn func()) {
	a.addCleanup(func(context.Context) error {
		closeFn()
		return nil
	})
}

func configurePeriodicJobs(dreamsEnabled bool) []*river.PeriodicJob {
	var periodicJobs []*river.PeriodicJob
	if dreamsEnabled {
		periodicJobs = append(periodicJobs, river.NewPeriodicJob(
			river.PeriodicInterval(24*time.Hour),
			func() (river.JobArgs, *river.InsertOpts) {
				return queue.NightlyDreamSweepArgs{}, nil
			},
			&river.PeriodicJobOpts{RunOnStart: false},
		))
		slog.Info("nightly dream sweep scheduled (every 24h)")
	}

	// Lane A board refresh (plan 25) — deterministic, no LLM, so it
	// runs for every hub regardless of the dreams engine being up.
	periodicJobs = append(periodicJobs, river.NewPeriodicJob(
		river.PeriodicInterval(24*time.Hour),
		func() (river.JobArgs, *river.InsertOpts) {
			return queue.BoardSweepArgs{}, nil
		},
		&river.PeriodicJobOpts{RunOnStart: true},
	))
	slog.Info("board sweep scheduled (every 24h, run on start)")

	// Send invite reminders for invites expiring within 48h (every 12 hours)
	periodicJobs = append(periodicJobs, river.NewPeriodicJob(
		river.PeriodicInterval(12*time.Hour),
		func() (river.JobArgs, *river.InsertOpts) {
			return queue.SendInviteRemindersArgs{}, nil
		},
		&river.PeriodicJobOpts{RunOnStart: false},
	))
	slog.Info("invite reminder scheduled (every 12h)")

	// Expire past-due waitlist invites every hour
	periodicJobs = append(periodicJobs, river.NewPeriodicJob(
		river.PeriodicInterval(1*time.Hour),
		func() (river.JobArgs, *river.InsertOpts) {
			return queue.ExpireWaitlistInvitesArgs{}, nil
		},
		&river.PeriodicJobOpts{RunOnStart: true}, // clean up on startup
	))
	slog.Info("waitlist invite expiry scheduled (every 1h)")

	// Expire past-due notification receipts every hour + on startup.
	periodicJobs = append(periodicJobs, river.NewPeriodicJob(
		river.PeriodicInterval(1*time.Hour),
		func() (river.JobArgs, *river.InsertOpts) {
			return queue.ExpireNotificationsArgs{}, nil
		},
		&river.PeriodicJobOpts{RunOnStart: true},
	))
	slog.Info("notification expiry scheduled (every 1h)")

	// Sync committed Redis usage counters to Postgres every 5 minutes
	periodicJobs = append(periodicJobs, river.NewPeriodicJob(
		river.PeriodicInterval(5*time.Minute),
		func() (river.JobArgs, *river.InsertOpts) {
			return queue.UsageSyncArgs{}, nil
		},
		&river.PeriodicJobOpts{RunOnStart: false},
	))
	slog.Info("usage sync scheduled (every 5m)")

	// Sweep hubs whose grace window has expired and fire hub_frozen
	// notifications. Hourly is cheap (one indexed SELECT + bounded
	// fan-out); enforcement is already live at every push/recall so
	// sweep latency only affects the notification side.
	periodicJobs = append(periodicJobs, river.NewPeriodicJob(
		river.PeriodicInterval(1*time.Hour),
		func() (river.JobArgs, *river.InsertOpts) {
			return queue.HubFreezeSweepArgs{}, nil
		},
		&river.PeriodicJobOpts{RunOnStart: true},
	))
	slog.Info("hub freeze sweep scheduled (every 1h)")

	// Phase 3.5a — chat lease sweep finalizes any session whose
	// worker dropped the lease > 5min ago. 1m cadence balances
	// responsiveness (a stuck session resolves within 1–2 min)
	// against DB load (one indexed SELECT bounded by the batch
	// size). RunOnStart cleans up post-deploy.
	periodicJobs = append(periodicJobs, river.NewPeriodicJob(
		river.PeriodicInterval(1*time.Minute),
		func() (river.JobArgs, *river.InsertOpts) {
			return queue.ChatLeaseSweepArgs{}, nil
		},
		&river.PeriodicJobOpts{RunOnStart: true},
	))
	slog.Info("chat lease sweep scheduled (every 1m)")

	// Phase 3.5a — purge chat_message_events older than 24h.
	// Plan 24's replay window. Hourly is plenty; the deletion
	// is a single indexed DELETE bounded by the per-row TTL.
	periodicJobs = append(periodicJobs, river.NewPeriodicJob(
		river.PeriodicInterval(1*time.Hour),
		func() (river.JobArgs, *river.InsertOpts) {
			return queue.ChatEventPurgeArgs{}, nil
		},
		&river.PeriodicJobOpts{RunOnStart: false},
	))
	slog.Info("chat event purge scheduled (every 1h)")

	// Phase 3.6b1 — chat tool approval timeout sweep. Catches
	// any chat_tool_approvals row whose `expires_at` has passed
	// while still pending (approver crashed / disconnected /
	// host-process killed before its own deadline timer fired).
	// Pending rows get UPDATE'd to 'timed_out' and an
	// approval.decided{state=timed_out} event is published so
	// any still-alive SDK-host approver unblocks via its poll
	// fallback. 1m cadence matches the lease sweep — same
	// tradeoff (responsiveness vs DB load), bounded by a single
	// indexed UPDATE. The healthy-approver path handles its own
	// expiry via the local timer in internal/agent/approver
	// and doesn't depend on this sweep.
	periodicJobs = append(periodicJobs, river.NewPeriodicJob(
		river.PeriodicInterval(1*time.Minute),
		func() (river.JobArgs, *river.InsertOpts) {
			return queue.ChatApprovalSweepArgs{}, nil
		},
		&river.PeriodicJobOpts{RunOnStart: false},
	))
	slog.Info("chat approval sweep scheduled (every 1m)")

	return periodicJobs
}

func subscribeJobMetrics(client *river.Client[pgx.Tx], stats *Stats, posthog *analytics.Client) {
	completedCh, _ := client.Subscribe(river.EventKindJobCompleted)
	failedCh, _ := client.Subscribe(river.EventKindJobFailed)

	go func() {
		for evt := range completedCh {
			stats.RecordCompleted()
			if posthog != nil {
				posthog.Track("worker", "worker.job.completed", map[string]any{
					"kind":    evt.Job.Kind,
					"attempt": evt.Job.Attempt,
					"queue":   evt.Job.Queue,
				})
			}
		}
	}()
	go func() {
		for evt := range failedCh {
			stats.RecordFailed()
			slog.Error("job failed",
				"kind", evt.Job.Kind,
				"id", evt.Job.ID,
				"attempt", evt.Job.Attempt,
				"errors", evt.Job.Errors,
			)
			if posthog != nil {
				posthog.Track("worker", "worker.job.failed", map[string]any{
					"kind":    evt.Job.Kind,
					"attempt": evt.Job.Attempt,
					"queue":   evt.Job.Queue,
				})
			}
		}
	}()
}

func logEnabled(name string, enabled bool) {
	if enabled {
		slog.Info(fmt.Sprintf("%s enabled", name))
	}
}
