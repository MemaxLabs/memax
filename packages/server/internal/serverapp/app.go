package serverapp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/redis/go-redis/v9"

	"github.com/MemaxLabs/memax/packages/server/internal/analytics"
	"github.com/MemaxLabs/memax/packages/server/internal/anthropic"
	"github.com/MemaxLabs/memax/packages/server/internal/attachments"
	"github.com/MemaxLabs/memax/packages/server/internal/billing"
	"github.com/MemaxLabs/memax/packages/server/internal/cache"
	"github.com/MemaxLabs/memax/packages/server/internal/chatstream"
	"github.com/MemaxLabs/memax/packages/server/internal/email"
	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/handler"
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
	"github.com/MemaxLabs/memax/packages/server/internal/observability"
	"github.com/MemaxLabs/memax/packages/server/internal/onboarding"
	"github.com/MemaxLabs/memax/packages/server/internal/planresolver"
	"github.com/MemaxLabs/memax/packages/server/internal/plans"
	"github.com/MemaxLabs/memax/packages/server/internal/queue"
	"github.com/MemaxLabs/memax/packages/server/internal/quota"
	"github.com/MemaxLabs/memax/packages/server/internal/ratelimit"
	"github.com/MemaxLabs/memax/packages/server/internal/retrieval/distill"
	"github.com/MemaxLabs/memax/packages/server/internal/retrieval/rerank"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// App owns the long-lived dependencies for the API process.
type App struct {
	cleanups []func(context.Context) error
}

// Shutdown releases dependencies in reverse construction order.
func (a *App) Shutdown(ctx context.Context) {
	for i := len(a.cleanups) - 1; i >= 0; i-- {
		if err := a.cleanups[i](ctx); err != nil {
			slog.Warn("server app cleanup failed", "error", err)
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

// Configure initializes API dependencies and registers routes on mux.
func Configure(ctx context.Context, mux *http.ServeMux) (*App, error) {
	app := &App{}
	configured := false
	defer func() {
		if !configured {
			app.Shutdown(ctx)
		}
	}()

	s, pool, err := configureStore(ctx, app)
	if err != nil {
		return nil, err
	}

	redisCache := cache.NewFromEnv()
	if redisCache != nil {
		app.addCleanup(func(context.Context) error {
			return redisCache.Close()
		})
		slog.Info("redis cache enabled")
	} else {
		slog.Info("no REDIS_URL set, redis cache disabled")
	}

	eventsBroker := events.NewBrokerFromEnv(slog.Default())
	if eventsBroker != nil {
		eventsBroker.Start(ctx)
		app.addCleanup(func(context.Context) error {
			return eventsBroker.Close()
		})
		slog.Info("realtime events enabled")
	} else {
		slog.Info("no REDIS_URL set, realtime events disabled")
	}

	posthog := analytics.New()
	if posthog != nil {
		app.addClose(posthog.Close)
		handler.SetAnalytics(posthog)
	}

	llm := anthropic.NewFromEnv()
	if llm != nil {
		llm.SetAnalytics(posthog)
		slog.Info("anthropic LLM client enabled")
	} else {
		slog.Info("no ANTHROPIC_API_KEY set, LLM features disabled")
	}

	embedder := embed.NewEmbedder()
	if embedder != nil {
		slog.Info("voyage AI embeddings enabled", "dimensions", embedder.Dimensions())
	} else {
		slog.Info("no VOYAGE_API_KEY set, using keyword search")
	}

	distiller := distill.New(llm)
	reranker := rerank.NewCohereFromEnv(nil, redisCache)
	if reranker != nil {
		slog.Info("cohere reranker enabled", "top_n", reranker.TopN())
	} else {
		slog.Info("no COHERE_API_KEY set, selective reranking disabled")
	}

	sum := summarize.New(llm)
	ext := extract.New(llm)
	cat := categorize.New(llm)
	lp := link.New(llm)
	fp := fileproc.New(llm)
	formatter := ingestformat.New(llm)
	titleResolver := ingesttitle.New(llm)
	blobStore := objectstore.NewFromEnv()
	if blobStore != nil {
		slog.Info("object storage enabled")
	} else {
		slog.Info("no object storage configured, original-file preservation disabled")
	}

	memories := handler.NewMemoriesHandler(s, eventsBroker, embedder, sum, ext, cat, lp, fp, formatter, titleResolver, blobStore)
	if redisCache != nil {
		// Wire the dedup cache for plan 21 §4.4 access tracking. Nil
		// is acceptable (handler falls open) but the active path needs
		// SetNX to dedup near-simultaneous /access POSTs.
		memories.SetCache(redisCache)
	}
	if signer := attachments.NewSigner(os.Getenv("ATTACHMENT_VIEW_SIGNING_KEY")); signer != nil {
		baseURL := strings.TrimRight(os.Getenv("API_BASE_URL"), "/")
		if baseURL == "" {
			baseURL = "http://localhost:8080"
		}
		memories.SetAttachmentSigner(signer, baseURL)
		slog.Info("attachment view signing enabled", "base_url", baseURL)
	} else {
		slog.Info("ATTACHMENT_VIEW_SIGNING_KEY not set, inline attachment previews disabled (download path still works)")
	}
	uploadsH := handler.NewUploadsHandler(blobStore)
	topicsH := handler.NewTopicsHandler(s, eventsBroker)
	recall := handler.NewRecallHandler(s, embedder, distiller, reranker, redisCache).WithEvents(eventsBroker)
	ask := handler.NewAskHandler(recall, s, llm, redisCache).WithEvents(eventsBroker)
	if ask != nil {
		slog.Info("answer synthesis enabled (/v1/ask)")
	}

	queueClient, err := configureQueue(pool, memories)
	if err != nil {
		return nil, err
	}

	dreamsH := handler.NewDreamsHandler(s, eventsBroker)
	if queueClient != nil {
		dreamsH.SetEnqueue(func(ctx context.Context, hubID string, triggeredBy string) error {
			return queueClient.Insert(ctx, queue.DreamCycleArgs{HubID: hubID, TriggeredBy: triggeredBy}, nil)
		})
	}

	chatH := handler.NewChatHandler(s)

	notificationsH := handler.NewNotificationsHandler(s, eventsBroker)
	settingsH := handler.NewSettingsHandler(s)
	hubsH := handler.NewHubsHandler(s).WithEvents(eventsBroker)
	boardsH := handler.NewBoardsHandler(s).WithEvents(eventsBroker)
	if queueClient != nil {
		// Decision write-backs must be recallable — route them through
		// the same ingest pipeline as every other memory.
		boardsH.SetEnqueue(func(memoryID, ownerID string, req model.PushRequest) {
			if err := queueClient.Insert(context.Background(), queue.MemoryProcessArgs{
				MemoryID:    memoryID,
				OwnerID:     ownerID,
				Content:     req.Content,
				Title:       req.Title,
				MemoryKind:  req.Kind,
				ContentType: req.ContentType,
				Source:      req.Source,
				SourceAgent: req.SourceAgent,
			}, nil); err != nil {
				slog.Warn("failed to enqueue decision memory processing", "error", err, "memory_id", memoryID)
			}
		})
	}
	configsH := handler.NewConfigsHandler(s, llm).WithEvents(eventsBroker)
	agentsH := handler.NewAgentsHandler(s, eventsBroker)
	eventsH := handler.NewEventsHandler(s, eventsBroker)

	if queueClient != nil {
		configsH.SetEnqueue(func(configID, ownerID string) {
			if err := queueClient.Insert(context.Background(), queue.ConfigExtractArgs{
				ConfigID: configID,
				OwnerID:  ownerID,
			}, nil); err != nil {
				slog.Warn("failed to enqueue config extraction", "error", err, "config_id", configID)
			}
		})
		// Phase 3.4a — chat send hands off to the queue. The
		// worker (cmd/worker via workerapp) drives the agent
		// runtime; this enqueue is the API process's only role
		// in the chat-execution path beyond persisting the
		// queued assistant row.
		//
		// **Detached context with a bounded timeout.** Codex's
		// Phase 3.4a review caught the gap: the original code
		// passed the request context, so a client disconnect
		// between BeginChatMessageTurn's commit and the Insert
		// would cancel the enqueue and leave the assistant row
		// queued forever (no chat lease sweeper exists yet).
		// context.WithoutCancel preserves any logger / trace
		// values from the request context but detaches
		// cancellation; a 5s deadline keeps a hung enqueue from
		// blocking indefinitely without losing it to a benign
		// disconnect.
		chatH.SetEnqueueRun(func(ctx context.Context, sessionID, ownerID, assistantMessageID string) error {
			detached, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			return queueClient.Insert(detached, queue.ChatMessageRunArgs{
				AssistantMessageID: assistantMessageID,
				OwnerID:            ownerID,
				SessionID:          sessionID,
			}, nil)
		})
	}

	authH, authMiddleware, err := configureAuth(pool, s)
	if err != nil {
		return nil, err
	}
	// Wire the user-axis SSE publisher after auth handler is built;
	// eventsBroker is in Configure's scope, not configureAuth's.
	if authH != nil {
		authH.SetEventsPublisher(eventsBroker)
		// Plan 23 §5.2 — wire the queue client so signup can enqueue
		// the seed-memory copy job. queueClient is nil in dev/test
		// pool-less paths; SetJobInserter accepts nil and the signup
		// handler skips the enqueue without erroring.
		if queueClient != nil {
			authH.SetJobInserter(queueClient)
		}
		// Plan 18 §6.1 — wire the super-notif emitter so signup
		// inserts the founder welcome + first-week checklist rows.
		// onboarding.New returns nil when store is nil (pool-less
		// dev/test), and SetOnboardingEmitter handles nil safely.
		authH.SetOnboardingEmitter(onboarding.New(s))
	}
	// Plan 18 §3.3 — restart handler. Single emitter instance is
	// reused for both signup and restart paths.
	onboardingEmitter := onboarding.New(s)
	onboardingH := handler.NewOnboardingHandler(s, onboardingEmitter, eventsBroker)
	if raw := strings.TrimSpace(os.Getenv("ONBOARDING_RESTART_RATE_MAX")); raw != "" {
		if n, perr := strconv.Atoi(raw); perr == nil {
			onboardingH.SetRateMax(n)
		}
	}
	handler.SetOnboardingVersionParser(onboarding.ParseChecklistVersion)

	// Plan 18 §4.6 — the onboarding Recorder used to cover five hot
	// paths (memories, ask, configs, hubs, dreams). Four of those
	// (memory_count, configs_sync = agent connected, hub_event,
	// dream click) are now served by the read-time materializer at
	// internal/onboarding/materialize.go — the materializer computes
	// completion from the underlying tables on every notifications
	// read, so the per-event recorder hooks are redundant.
	//
	// Only `first_ask` still needs an event-driven tick because /ask
	// is stateless (no `asks` row exists to count from). That one
	// item is the entire surface area of the recorder now.
	if ask != nil {
		ask.SetOnboardingHook(onboarding.NewRecorder(s, eventsBroker))
	}
	hubMiddleware := handler.HubContext(s)

	// Plan registry — load plans from Postgres, start background reload.
	// Fail-fast in Postgres mode: running with an empty registry masks
	// broken migrations and causes /v1/plans to return [].
	// In-memory mode (no DB): skip plan loading entirely.
	var planRegistry *plans.Registry
	if pool != nil {
		planRegistry = plans.NewRegistry(s)
		if err := planRegistry.Load(ctx); err != nil {
			return nil, fmt.Errorf("plan registry initial load: %w", err)
		}
		cancel := planRegistry.Start(ctx, 60*time.Second)
		app.addCleanup(func(context.Context) error { cancel(); return nil })
		slog.Info("plan registry loaded", "count", len(planRegistry.AllPlans()))
	}

	// Billing service — single mutation path for all plan changes.
	// Works in admin-managed mode (no external payment provider).
	var billingService *billing.Service
	if planRegistry != nil {
		billingService = billing.NewService(s, planRegistry)
		// Wire billing into auth for early access plan assignment on invite consumption
		if authH != nil {
			authH.SetPlanChanger(billingService)
		}
	}

	// Meter — usage metering with Redis dual counters.
	// Uses the critical Redis instance (noeviction) for counter accuracy.
	criticalRedis := cache.CriticalFromEnv()
	var meterService *meter.Meter
	var rateLimiter *ratelimit.Limiter
	var hubResolver *planresolver.Resolver
	if planRegistry != nil {
		var redisClient *redis.Client
		if criticalRedis != nil {
			redisClient = criticalRedis.Client()
			app.addCleanup(func(context.Context) error { return criticalRedis.Close() })
			slog.Info("critical redis enabled for metering")
		} else {
			slog.Info("no critical redis, metering runs in degraded mode (no quota enforcement)")
		}
		meterService = meter.New(redisClient, s, planRegistry)
		rateLimiter = ratelimit.New(redisClient) // shares critical Redis with meter

		// Plan resolver for per-hub elevation
		hubResolver = planresolver.New(redisClient, s, planRegistry)
		meterService.SetResolver(hubResolver)

		// Phase 3.5b — chat stream signaler. Reuses the same
		// Redis client as meter/ratelimit/planresolver so we
		// don't add a second connection. Nil-safe: when Redis
		// is unset (dev), Signaler stays nil and the SSE handler
		// falls back to the 250ms poll cadence with no
		// behavioral change.
		chatSignaler := chatstream.NewSignaler(redisClient)
		chatH.SetSignaler(chatSignaler)
		slog.Info("chat stream signaler enabled", "redis", redisClient != nil)

		// Phase 3.6b1 — approval-decided fan-out via the
		// existing events.Broker. The chat decide handler
		// publishes; the SDK-host approver (Phase 3.6b2)
		// subscribes per-approval.
		chatH.SetEventsPublisher(eventsBroker)

		// Wire meter's reset callback into billing service
		if billingService != nil {
			billingService.SetResetCounters(meterService.ResetCounters)
			billingService.SetInvalidateUserPlan(hubResolver.InvalidateUser)
			billingService.SetInvalidateHubMembers(hubResolver.InvalidateHubMembers)
			// Fire hub_restored when a plan upgrade clears
			// over_limit_since (the non-delete path).
			billingService.SetNotifyHubRestored(memories.NotifyHubRestored)
		}

		// Wire meter into handlers that need memory count enforcement / live usage
		memories.SetMeter(meterService)
		// Hub-level memory cap — the resolver reads the target hub's
		// subscription plan independent of any user's entitlements.
		memories.SetHubQuotaResolver(hubResolver)
		settingsH.SetPlanResolver(planRegistry)
		settingsH.SetEffectiveResolver(hubResolver)
		settingsH.SetUsageReader(meterService)
		hubsH.SetInvalidateUserPlan(hubResolver.InvalidateUser)

		// Dream-tier quota gate. Wires the resolver so manual triggers
		// hit CheckDreamQuota before enqueueing. Mode defaults to soft
		// (DreamsHandler's NewDreamsHandler default); operators flip
		// to hard via DREAM_QUOTA_MODE=hard in env once Phase 3 lands.
		// See docs/plans/23-dream-tiers.md.
		dreamsH.SetQuotaResolver(hubResolver)
		if mode := strings.TrimSpace(os.Getenv("DREAM_QUOTA_MODE")); mode != "" {
			switch quota.Mode(mode) {
			case quota.ModeHard:
				dreamsH.SetQuotaMode(quota.ModeHard)
				slog.Info("dream quota: hard enforcement enabled via DREAM_QUOTA_MODE")
			case quota.ModeSoft:
				dreamsH.SetQuotaMode(quota.ModeSoft)
			default:
				slog.Warn("dream quota: invalid DREAM_QUOTA_MODE; falling back to soft",
					"value", mode)
			}
		}
		hubsH.SetBilling(billingService)
		hubsH.SetOwnershipResolver(hubResolver)
		// Uploads handler uses the plan resolver to enforce the per-plan
		// memory_attachment size cap.
		uploadsH.SetLimitsResolver(hubResolver)
		notificationsH.SetInvalidateUserPlan(hubResolver.InvalidateUser)
		notificationsH.SetBilling(billingService)
		notificationsH.SetOwnershipResolver(hubResolver)
	}

	adminH := handler.NewAdminHandler(s)
	adminUsersH := handler.NewAdminUsersHandler(s, billingService, planRegistry)
	// AdminUsersHandler's usage endpoint reads Redis counters; wire
	// the meter if available so admin views reflect live quota state.
	// Degrades gracefully to 0s when meterService is nil (same
	// contract as every other quota-read path).
	if meterService != nil {
		adminUsersH.SetUsageReader(meterService)
	}
	adminConfigH := handler.NewAdminConfigHandler(ratelimit.TrustedProxyMode(), meterService != nil, rateLimiter != nil)
	adminDreamsH := handler.NewAdminDreamsHandler(s)
	// adminWaitlistH reuses authH's ConsumeInviteForUser so the
	// repair path and the normal login path share one invite state
	// machine. authH carries the pool + planChanger the consumer
	// relies on; injecting a narrower interface would add indirection
	// with no benefit here since both handlers live in the same
	// package and construction order.
	adminWaitlistH := handler.NewAdminWaitlistReconcileHandler(s, authH)
	// AdminOps needs River table access (via *PostgresStore) + the
	// River client for retry/cancel. Degraded when either is absent:
	// the handler + routes still register but nil checks inside
	// return 503.
	var adminOpsH *handler.AdminOpsHandler
	if opsStore, ok := s.(store.AdminOpsStore); ok {
		// logsQuerier is nil when LOKI_URL/USERNAME/PASSWORD aren't all
		// set (dev without Grafana Cloud). The handler treats nil as
		// "feature disabled" and returns 503 with a clear message, so
		// dev and degraded prod don't crash — they just hide the logs
		// panel.
		logsQuerier := observability.NewQueryClient()
		// observability.QueryClient satisfies handler.jobLogQuerier
		// by having QueryJobLogs + QueryEnv. Passing a typed-nil
		// would hide the nil from the handler's nil check, so only
		// pass when non-nil.
		var logs handler.JobLogQuerier
		if logsQuerier != nil {
			logs = logsQuerier
		}
		adminOpsH = handler.NewAdminOpsHandler(s, opsStore, queueClient, logs)
	}
	adminNotificationsH := handler.NewAdminNotificationsHandler(s)
	adminNotificationsH.SetEvents(eventsBroker)
	adminCampaignsH := handler.NewAdminCampaignsHandler(s)
	adminCampaignTemplatesH := handler.NewAdminCampaignTemplatesHandler(s)
	// AI-assist lives on the admin surface because the persona + legal
	// constraints in its system prompt are PMM-specific. nil `llm`
	// causes the handler to return 503 per-request (dev without
	// ANTHROPIC_API_KEY is the common case).
	adminAIAssistH := handler.NewAdminAIAssistHandler(llm, s)
	// Wire recall so AI-assist can ground generations in the admin's
	// own memax knowledge. Graceful degradation: if recall isn't
	// constructable (no embedder/reranker in dev), the handler emits
	// ungrounded copy instead of hard-failing. `recall` is the same
	// handler that backs POST /v1/recall.
	if recall != nil {
		adminAIAssistH.SetRecall(recall)
	}
	adminAudiencesH := handler.NewAdminAudiencesHandler(s)
	waitlistH := handler.NewWaitlistHandler(s)

	// Resend webhook handler is ALWAYS constructed so the /v1/webhooks/resend
	// route exists in every environment. When the secret is unset (or
	// malformed) the handler responds 503 on incoming events — ops/tests
	// can probe the endpoint and distinguish "not configured" from
	// "route missing". Nil-secret verification in the handler itself
	// refuses to accept unsigned payloads.
	var resendSecret *email.ResendWebhookSecret
	if rawSecret := os.Getenv("RESEND_WEBHOOK_SECRET"); rawSecret != "" {
		parsed, err := email.NewResendWebhookSecret(rawSecret)
		if err != nil {
			slog.Warn("resend webhook disabled: invalid RESEND_WEBHOOK_SECRET", "error", err)
		} else {
			resendSecret = parsed
			slog.Info("resend webhook enabled")
		}
	} else {
		slog.Info("resend webhook endpoint mounted but disabled (RESEND_WEBHOOK_SECRET unset)")
	}
	resendWebhookH := handler.NewResendWebhookHandler(s, resendSecret)

	// Public /v1/unsubscribe handler — token IS the auth. Always
	// mounted so one-click links embedded in already-sent campaign
	// emails keep working across deploys.
	unsubscribeH := handler.NewUnsubscribeHandler(s)
	if templateStore, ok := s.(email.OverrideStore); ok {
		brandProvider, _ := s.(email.BrandProvider)
		templateManager, tmplErr := email.NewTemplateManager(templateStore, brandProvider)
		if tmplErr != nil {
			return nil, fmt.Errorf("load email template manager: %w", tmplErr)
		}
		// Relative logo paths (e.g. "/images/memax-icon.svg" picked from
		// the brand editor's preset dropdown) get resolved to absolute
		// URLs via APP_BASE_URL. Email clients don't follow relative
		// URLs, so without this the logo would silently break.
		templateManager.SetAppBaseURL(os.Getenv("APP_BASE_URL"))
		// DOCS_BASE_URL feeds the auto-injected `{{ .DocsURL }}` template
		// variable (see TemplateManager.injectCommonVars). Set per-env
		// so staging/dev emails link to the right docs surface. Empty
		// falls back to the hardcoded prod default so forgotten envs
		// don't produce broken links.
		templateManager.SetDocsBaseURL(os.Getenv("DOCS_BASE_URL"))
		adminH.SetTemplateManager(templateManager)
		// Campaign test-send shares the template manager so test emails
		// render through the same brand layout as the real broadcast.
		adminCampaignsH.SetTemplateManager(templateManager)
	}

	// Wire email enqueuing into admin + waitlist handlers
	if queueClient != nil {
		enqueueEmail := func(template, to string, vars map[string]string) error {
			return queueClient.Insert(context.Background(), queue.SendEmailArgs{
				Template:  template,
				To:        to,
				Variables: vars,
			}, nil)
		}
		enqueueRenderedEmail := func(to, subject, html, text string, tags map[string]string) error {
			return queueClient.Insert(context.Background(), queue.SendRenderedEmailArgs{
				To:      to,
				Subject: subject,
				HTML:    html,
				Text:    text,
				Tags:    tags,
			}, nil)
		}
		adminH.SetEnqueueEmail(enqueueEmail)
		adminH.SetEnqueueRenderedEmail(enqueueRenderedEmail)
		adminCampaignsH.SetEnqueueRenderedEmail(enqueueRenderedEmail)
		waitlistH.SetEnqueueEmail(enqueueEmail)
		hubsH.SetEnqueueEmail(enqueueEmail)
		// authH is constructed conditionally upstream; only wire the
		// email enqueue when we actually have a handler. Without this,
		// /v1/auth/email/request returns 503 even though the queue is
		// healthy — email OTP needs both the handler AND the queue.
		if authH != nil {
			authH.SetEnqueueEmail(enqueueEmail)
		}
		adminH.SetAppBaseURL(os.Getenv("APP_BASE_URL"))
		hubsH.SetAppBaseURL(os.Getenv("APP_BASE_URL"))

		// Wire broadcast notification job
		adminNotificationsH.SetEnqueueBroadcast(func(kind string, payload json.RawMessage, batchID, adminID string) error {
			return queueClient.Insert(context.Background(), queue.SendBroadcastNotificationArgs{
				NotifKind: kind,
				Payload:   payload,
				BatchID:   batchID,
				AdminID:   adminID,
			}, nil)
		})

		// Wire campaign send job (immediate or scheduled).
		adminCampaignsH.SetEnqueue(func(ctx context.Context, campaignID, initiatedBy string, scheduledAt *time.Time) error {
			var opts *river.InsertOpts
			if scheduledAt != nil {
				opts = &river.InsertOpts{ScheduledAt: *scheduledAt}
			}
			return queueClient.Insert(ctx, queue.CampaignSendArgs{
				CampaignID:  campaignID,
				InitiatedBy: initiatedBy,
			}, opts)
		})
	}
	if capStr := os.Getenv("WAITLIST_WAVE_CAP"); capStr != "" {
		if cap, err := strconv.Atoi(capStr); err == nil && cap > 0 {
			adminH.SetWaveCap(cap)
		}
	}

	// Bootstrap admin roles from ADMIN_EMAILS env var
	if adminEmails := os.Getenv("ADMIN_EMAILS"); adminEmails != "" {
		emails := strings.Split(adminEmails, ",")
		for i := range emails {
			emails[i] = strings.TrimSpace(emails[i])
		}
		count, seedErr := s.EnsureAdminRolesByEmail(ctx, emails, "super_admin")
		if seedErr != nil {
			slog.Error("failed to seed admin roles", "error", seedErr)
		} else if count > 0 {
			slog.Info("admin roles seeded", "count", count)
		}
	}

	registerRoutes(mux, routeDeps{
		memories:               memories,
		uploads:                uploadsH,
		topics:                 topicsH,
		recall:                 recall,
		ask:                    ask,
		dreams:                 dreamsH,
		chat:                   chatH,
		notifications:          notificationsH,
		onboarding:             onboardingH,
		settings:               settingsH,
		hubs:                   hubsH,
		boards:                 boardsH,
		configs:                configsH,
		agents:                 agentsH,
		events:                 eventsH,
		auth:                   authH,
		admin:                  adminH,
		adminNotifications:     adminNotificationsH,
		adminCampaigns:         adminCampaignsH,
		adminCampaignTemplates: adminCampaignTemplatesH,
		adminAIAssist:          adminAIAssistH,
		adminAudiences:         adminAudiencesH,
		adminSeedMemories:      handler.NewAdminSeedMemoriesHandler(s, queueClient),
		adminSeedImages:        handler.NewAdminSeedImagesHandler(blobStore, os.Getenv("R2_PUBLIC_BASE_URL")),
		waitlist:               waitlistH,
		authMiddleware:         authMiddleware,
		hubMiddleware:          hubMiddleware,
		plansH:                 handler.NewPlansHandler(planRegistry),
		adminUsers:             adminUsersH,
		adminConfig:            adminConfigH,
		adminDreams:            adminDreamsH,
		adminWaitlist:          adminWaitlistH,
		adminOps:               adminOpsH,
		resendWebhook:          resendWebhookH,
		unsubscribe:            unsubscribeH,
		bar:                    handler.NewBarHandler(s, memories),
		billing:                billingService,
		meter:                  meterService,
		rateLimiter:            rateLimiter,
		hubResolver:            hubResolver,
		planRegistry:           planRegistry,
		store:                  s,
		eventsBroker:           eventsBroker,
	})

	configured = true
	return app, nil
}

func configureStore(ctx context.Context, app *App) (store.Store, *pgxpool.Pool, error) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		slog.Info("no DATABASE_URL set, using in-memory store")
		return store.NewInMemoryStore(), nil, nil
	}

	slog.Info("connecting to PostgreSQL")
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return nil, nil, fmt.Errorf("connect to database: %w", err)
	}
	app.addClose(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		return nil, nil, fmt.Errorf("ping database: %w", err)
	}
	slog.Info("connected to PostgreSQL")

	// Migrations are handled by the standalone /memax-migrate binary
	// via Fly.io's release_command — runs before instances start.
	return store.NewPostgresStore(pool), pool, nil
}

func configureQueue(pool *pgxpool.Pool, memories *handler.MemoriesHandler) (*queue.Client, error) {
	if pool == nil {
		slog.Info("no database, memory processing via goroutines (fallback)")
		return nil, nil
	}

	queueClient, err := queue.InsertClient(pool)
	if err != nil {
		return nil, fmt.Errorf("create queue client: %w", err)
	}

	memories.SetEnqueue(func(memoryID, ownerID string, req model.PushRequest) {
		if err := queueClient.Insert(context.Background(), queue.MemoryProcessArgs{
			MemoryID:            memoryID,
			OwnerID:             ownerID,
			Content:             req.Content,
			Title:               req.Title,
			Hint:                req.Hint,
			MemoryKind:          req.Kind,
			Stability:           req.Stability,
			Tags:                req.Tags,
			ContentType:         req.ContentType,
			Source:              req.Source,
			SourceAgent:         req.SourceAgent,
			SourcePath:          req.SourcePath,
			HubReason:           req.HubReason,
			ProjectContext:      req.ProjectContext,
			BatchID:             req.BatchID,
			FileRef:             req.FileRef,
			AllowRelatedContext: req.AllowRelatedContext,
		}, nil); err != nil {
			slog.Error("failed to enqueue memory processing, falling back to goroutine", "error", err, "memory_id", memoryID)
			go memories.FallbackProcessMemory(memoryID, ownerID, req)
		}
	})
	slog.Info("memory processing via River queue")

	return queueClient, nil
}

func configureAuth(pool *pgxpool.Pool, s store.Store) (*handler.AuthHandler, func(http.Handler) http.Handler, error) {
	var jwtSecret []byte
	var keyResolver handler.APIKeyResolver
	var grantResolver handler.GrantResolver
	var authH *handler.AuthHandler
	var err error

	if pool != nil {
		authH, err = handler.NewAuthHandler(pool)
		if err != nil {
			return nil, nil, fmt.Errorf("initialize auth handler: %w", err)
		}
		authH.SetStore(s)

		jwtSecret, err = handler.RequiredJWTSecret()
		if err != nil {
			return nil, nil, fmt.Errorf("load JWT secret: %w", err)
		}
		keyResolver = authH.ResolveAPIKey
		grantResolver = authH.ResolveOAuthGrant
	}

	return authH, handler.RequireAuth(jwtSecret, keyResolver, grantResolver), nil
}
