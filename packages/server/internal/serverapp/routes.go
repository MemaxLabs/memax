package serverapp

import (
	"context"
	"net/http"

	"github.com/MemaxLabs/memax/packages/server/internal/billing"
	"github.com/MemaxLabs/memax/packages/server/internal/events"
	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/meter"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
	"github.com/MemaxLabs/memax/packages/server/internal/plans"
	"github.com/MemaxLabs/memax/packages/server/internal/ratelimit"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// hubAwarePlanResolver resolves limits with per-hub elevation.
// Includes both legacy ResolveForRequest and scoped methods for
// the meter and ratelimit middleware.
type hubAwarePlanResolver interface {
	ResolveForRequest(ctx context.Context, userID, billingHubID string) model.UserLimits
	ResolveReadEntitlements(ctx context.Context, userID string) model.ResolvedEntitlements
	ResolvePersonalWriteEntitlements(ctx context.Context, userID string) model.ResolvedEntitlements
	ResolveHubWriteEntitlements(ctx context.Context, userID, hubID string) model.ResolvedEntitlements
}

type routeDeps struct {
	memories               *handler.MemoriesHandler
	uploads                *handler.UploadsHandler
	topics                 *handler.TopicsHandler
	recall                 *handler.RecallHandler
	ask                    *handler.AskHandler
	dreams                 *handler.DreamsHandler
	chat                   *handler.ChatHandler
	notifications          *handler.NotificationsHandler
	onboarding             *handler.OnboardingHandler
	settings               *handler.SettingsHandler
	hubs                   *handler.HubsHandler
	configs                *handler.ConfigsHandler
	agents                 *handler.AgentsHandler
	events                 *handler.EventsHandler
	auth                   *handler.AuthHandler
	admin                  *handler.AdminHandler
	adminNotifications     *handler.AdminNotificationsHandler
	adminCampaigns         *handler.AdminCampaignsHandler
	adminCampaignTemplates *handler.AdminCampaignTemplatesHandler
	adminAIAssist          *handler.AdminAIAssistHandler
	adminAudiences         *handler.AdminAudiencesHandler
	adminSeedMemories      *handler.AdminSeedMemoriesHandler
	adminSeedImages        *handler.AdminSeedImagesHandler
	waitlist               *handler.WaitlistHandler
	plansH                 *handler.PlansHandler
	adminUsers             *handler.AdminUsersHandler
	adminConfig            *handler.AdminConfigHandler
	adminDreams            *handler.AdminDreamsHandler
	adminWaitlist          *handler.AdminWaitlistReconcileHandler
	adminOps               *handler.AdminOpsHandler
	resendWebhook          *handler.ResendWebhookHandler
	unsubscribe            *handler.UnsubscribeHandler
	bar                    *handler.BarHandler
	billing                *billing.Service
	meter                  *meter.Meter
	rateLimiter            *ratelimit.Limiter
	hubResolver            hubAwarePlanResolver
	planRegistry           *plans.Registry
	authMiddleware         func(http.Handler) http.Handler
	hubMiddleware          func(http.Handler) http.Handler
	store                  store.Store
	eventsBroker           events.Publisher
}

// ipLimitFactory returns a helper that wraps a handler with a per-IP
// rate limit. When deps.rateLimiter is nil (dev without Redis), the
// returned function is a pass-through — matches the degraded-mode
// contract of every other rate-limit code path.
//
// Shared across route groups because the unauthenticated endpoints
// that need IP-level protection are scattered (auth, mcp-oauth,
// invites). Keeping this one definition keeps the rate-limit budget
// table (ratelimit/iplimiter.go) as the single place to tune
// unauthenticated throughput.
func ipLimitFactory(deps routeDeps) func(ratelimit.EndpointLimit, http.HandlerFunc) http.HandlerFunc {
	if deps.rateLimiter == nil {
		return func(_ ratelimit.EndpointLimit, h http.HandlerFunc) http.HandlerFunc { return h }
	}
	return deps.rateLimiter.WrapIP
}

func registerRoutes(mux *http.ServeMux, deps routeDeps) {
	// Middleware chain (execution order, outermost first):
	//   RequireAuth → HubContext → AuthorizeHTTP → RateLimit → Meter → Handler
	withAuth := func(h http.Handler) http.Handler {
		inner := h
		if deps.meter != nil {
			inner = deps.meter.Middleware()(inner)
		}
		if deps.rateLimiter != nil {
			inner = deps.rateLimiter.Middleware(deps.planRegistry, deps.store, deps.hubResolver)(inner)
		}
		inner = handler.AuthorizeHTTP(inner)
		return deps.authMiddleware(deps.hubMiddleware(inner))
	}

	protected := http.NewServeMux()
	registerMemoryRoutes(protected, deps)
	registerKnowledgeRoutes(protected, deps)
	registerAgentSyncRoutes(protected, deps)
	registerHubRoutes(mux, protected, deps)
	registerAccountRoutes(protected, deps)
	registerProtectedMounts(mux, protected, withAuth)
	registerAgentRoutes(mux, protected, withAuth, deps)
	registerMCPRoutes(mux, withAuth, deps)
	registerAuthRoutes(mux, protected, deps)
	registerAdminRoutes(mux, deps)
	registerWaitlistRoutes(mux, deps)
	registerWebhookRoutes(mux, deps)
	registerUnsubscribeRoute(mux, deps)
	registerPlansRoutes(mux, deps)

	// GET /v1/attachments/view is deliberately unauthenticated at the
	// middleware layer — the HMAC signature on the query string IS
	// the authorization, mirroring how signed S3 URLs work. The
	// handler verifies the signature, then loads the attachment row
	// from the DB so content-type and disposition come from trusted
	// server-side state, not the signed payload.
	mux.HandleFunc("GET /v1/attachments/view", deps.memories.ServeAttachmentView)

	// Favicon served at both paths so browsers auto-requesting
	// /favicon.ico don't flood logs with 404s. Embedded in the
	// binary; 1-year immutable cache, busted naturally by deploys.
	mux.HandleFunc("GET /favicon.ico", handler.FaviconHandler)
	mux.HandleFunc("GET /favicon.svg", handler.FaviconHandler)
}

func registerMemoryRoutes(mux *http.ServeMux, deps routeDeps) {
	mux.HandleFunc("POST /v1/memories", deps.memories.Create)
	mux.HandleFunc("GET /v1/memories", deps.memories.List)
	mux.HandleFunc("GET /v1/memories/search", deps.memories.Search)
	if deps.bar != nil {
		mux.HandleFunc("GET /v1/bar/search", deps.bar.Search)
	}
	mux.HandleFunc("GET /v1/memories/{id}", deps.memories.Get)
	mux.HandleFunc("POST /v1/memories/{id}/access", deps.memories.TrackAccessed)
	mux.HandleFunc("GET /v1/memories/{id}/related", deps.memories.Related)
	mux.HandleFunc("GET /v1/memories/{id}/attachments/{attachmentID}/download", deps.memories.DownloadAttachment)
	mux.HandleFunc("POST /v1/memories/{id}/attachments/{attachmentID}/view-url", deps.memories.CreateAttachmentViewURL)
	mux.HandleFunc("PATCH /v1/memories/{id}", deps.memories.Update)
	mux.HandleFunc("DELETE /v1/memories/{id}", deps.memories.Delete)
	mux.HandleFunc("POST /v1/memories/batch-delete", deps.memories.BatchDelete)
	mux.HandleFunc("POST /v1/memories/batch-move", deps.memories.BatchMove)
	mux.HandleFunc("POST /v1/memories/{id}/share", deps.memories.Share)
	// /v1/admin/reindex moved to registerAdminRoutes — every other
	// /v1/admin/* path lives under the admin sub-mux which is
	// mounted directly on root, so a reindex registered here was
	// shadowed by the /v1/admin/ prefix mount and served 404.
}

func registerKnowledgeRoutes(mux *http.ServeMux, deps routeDeps) {
	mux.HandleFunc("GET /v1/topics", deps.topics.List)
	mux.HandleFunc("POST /v1/topics", deps.topics.Create)
	// Literal segment wins over {id} in the 1.22 mux, so /archived is safe
	// alongside /v1/topics/{id}.
	mux.HandleFunc("GET /v1/topics/archived", deps.topics.ListArchived)
	mux.HandleFunc("GET /v1/topics/{id}", deps.topics.Get)
	mux.HandleFunc("PATCH /v1/topics/{id}", deps.topics.Update)
	mux.HandleFunc("DELETE /v1/topics/{id}", deps.topics.Delete)
	mux.HandleFunc("GET /v1/topics/{id}/memories", deps.topics.ListMemories)
	mux.HandleFunc("POST /v1/topics/{id}/memories", deps.topics.AddMemory)
	mux.HandleFunc("DELETE /v1/topics/{id}/memories/{mid}", deps.topics.RemoveMemory)
	mux.HandleFunc("POST /v1/topics/{id}/visit", deps.topics.RecordVisit)
	mux.HandleFunc("POST /v1/topics/{id}/archive", deps.topics.Archive)
	mux.HandleFunc("POST /v1/topics/{id}/restore", deps.topics.Restore)
	mux.HandleFunc("POST /v1/topics/reorder", deps.topics.Reorder)

	mux.HandleFunc("POST /v1/recall", deps.recall.Recall)
	if deps.ask != nil {
		mux.HandleFunc("POST /v1/ask", deps.ask.Ask)
	}
	mux.HandleFunc("GET /v1/events/stream", deps.events.Stream)

	mux.HandleFunc("GET /v1/dreams", deps.dreams.List)
	mux.HandleFunc("GET /v1/dreams/report", deps.dreams.GetReport)
	mux.HandleFunc("POST /v1/dreams/trigger", deps.dreams.Trigger)

	// Chat (Phase 3.2 — session CRUD; Phase 3.3 — message
	// persistence + idempotency. SSE stream and approval
	// endpoints land in subsequent commits).
	mux.HandleFunc("POST /v1/chat/sessions", deps.chat.Create)
	mux.HandleFunc("GET /v1/chat/sessions", deps.chat.List)
	mux.HandleFunc("GET /v1/chat/sessions/{id}", deps.chat.Get)
	mux.HandleFunc("PATCH /v1/chat/sessions/{id}", deps.chat.Patch)
	mux.HandleFunc("DELETE /v1/chat/sessions/{id}", deps.chat.Delete)
	mux.HandleFunc("POST /v1/chat/sessions/{id}/messages", deps.chat.Send)
	mux.HandleFunc("GET /v1/chat/sessions/{id}/messages", deps.chat.ListMessages)
	mux.HandleFunc("GET /v1/chat/sessions/{id}/messages/{msg_id}", deps.chat.GetMessage)
	mux.HandleFunc("GET /v1/chat/sessions/{id}/messages/{msg_id}/stream", deps.chat.Stream)
	mux.HandleFunc("POST /v1/chat/sessions/{id}/messages/{msg_id}/cancel", deps.chat.Cancel)
	mux.HandleFunc("POST /v1/chat/sessions/{id}/messages/{msg_id}/regenerate", deps.chat.Regenerate)
	mux.HandleFunc("GET /v1/chat/tools", deps.chat.Tools)
	mux.HandleFunc("POST /v1/chat/sessions/{id}/approvals/{approval_id}", deps.chat.DecideApproval)

	// Notifications — inbox notification framework (Phase 3b). The
	// legacy /v1/reviews surface is fully retired in Phase 6; this
	// is the only path for reading + resolving inbox items.
	if deps.notifications != nil {
		mux.HandleFunc("GET /v1/notifications", deps.notifications.List)
		mux.HandleFunc("GET /v1/notifications/summary", deps.notifications.Summary)
		mux.HandleFunc("POST /v1/notifications/seen", deps.notifications.BulkSeen)
		mux.HandleFunc("POST /v1/notifications/dismiss", deps.notifications.BulkDismiss)
		mux.HandleFunc("POST /v1/notifications/{id}/seen", deps.notifications.MarkSeen)
		mux.HandleFunc("POST /v1/notifications/{id}/dismiss", deps.notifications.Dismiss)
		mux.HandleFunc("POST /v1/notifications/{id}/resolve", deps.notifications.Resolve)
		// Plan 18 — super-notif sub-item mutations. Generic across
		// `checklist` and `digest` kinds; /complete refuses non-checklist
		// kinds at the handler with `400 kind_not_supported`.
		mux.HandleFunc("POST /v1/notifications/{id}/items/{item_id}/view", deps.notifications.ViewItem)
		mux.HandleFunc("POST /v1/notifications/{id}/items/{item_id}/complete", deps.notifications.CompleteItem)
	}

	// Plan 18 §3.3 — restart endpoint. Single onboarding-specific
	// route; everything else (read, dismiss, complete) lives on
	// /v1/notifications.
	if deps.onboarding != nil {
		mux.HandleFunc("POST /v1/onboarding/restart", deps.onboarding.Restart)
		mux.HandleFunc("GET /v1/onboarding/state", deps.onboarding.State)
	}
}

func registerAccountRoutes(mux *http.ServeMux, deps routeDeps) {
	mux.HandleFunc("GET /v1/settings", deps.settings.Get)
	mux.HandleFunc("PATCH /v1/settings", deps.settings.Update)
	mux.HandleFunc("GET /v1/usage", deps.settings.GetUsage)
	mux.HandleFunc("GET /v1/usage/dreams", deps.dreams.GetUsage)
	mux.HandleFunc("DELETE /v1/account/data", deps.memories.DeleteAllData)
}

func registerAgentSyncRoutes(mux *http.ServeMux, deps routeDeps) {
	mux.HandleFunc("PUT /v1/configs", deps.configs.Upsert)
	mux.HandleFunc("GET /v1/configs", deps.configs.List)
	mux.HandleFunc("GET /v1/configs/deleted", deps.configs.ListDeleted)
	mux.HandleFunc("POST /v1/configs/restore", deps.configs.Restore)
	mux.HandleFunc("GET /v1/configs/{id}", deps.configs.Get)
	mux.HandleFunc("DELETE /v1/configs/{id}", deps.configs.Delete)
	mux.HandleFunc("POST /v1/configs/batch-delete", deps.configs.BatchDelete)
	mux.HandleFunc("POST /v1/configs/sync", deps.configs.Sync)
	mux.HandleFunc("POST /v1/configs/ack", deps.configs.Ack)
	mux.HandleFunc("POST /v1/configs/local-delete", deps.configs.LocalDelete)
	mux.HandleFunc("POST /v1/configs/merge", deps.configs.Merge)
	mux.HandleFunc("GET /v1/personas", deps.configs.ListPersonas)
	mux.HandleFunc("DELETE /v1/personas/{id}", deps.configs.DeletePersona)
	mux.HandleFunc("GET /v1/personas/{id}/revisions", deps.configs.ListPersonaRevisions)
	mux.HandleFunc("GET /v1/personas/{id}/revisions/{version}", deps.configs.GetPersonaRevision)
	mux.HandleFunc("POST /v1/personas/{id}/revisions/{version}/restore", deps.configs.RestorePersonaRevision)

	mux.HandleFunc("POST /v1/uploads", deps.uploads.Create)
}

func registerHubRoutes(root *http.ServeMux, protected *http.ServeMux, deps routeDeps) {
	protected.HandleFunc("POST /v1/hubs", deps.hubs.Create)
	protected.HandleFunc("GET /v1/hubs/check-slug", deps.hubs.CheckSlug)
	protected.HandleFunc("GET /v1/hubs", deps.hubs.List)
	protected.HandleFunc("GET /v1/hubs/{id}", deps.hubs.Get)
	protected.HandleFunc("GET /v1/hubs/{id}/summary", deps.hubs.GetSummary)
	protected.HandleFunc("PATCH /v1/hubs/{id}", deps.hubs.Update)
	protected.HandleFunc("DELETE /v1/hubs/{id}", deps.hubs.Delete)
	protected.HandleFunc("POST /v1/hubs/{id}/visit", deps.hubs.RecordVisit)
	protected.HandleFunc("POST /v1/hubs/{id}/members", deps.hubs.AddMember)
	protected.HandleFunc("PATCH /v1/hubs/{id}/members/{user_id}", deps.hubs.UpdateMemberRole)
	protected.HandleFunc("DELETE /v1/hubs/{id}/members/{user_id}", deps.hubs.RemoveMember)
	protected.HandleFunc("POST /v1/hubs/{id}/leave", deps.hubs.Leave)
	protected.HandleFunc("POST /v1/hubs/{id}/ownership-transfer", deps.hubs.CreateOwnershipTransfer)
	protected.HandleFunc("POST /v1/hubs/{id}/ownership-transfer/{transfer_id}/accept", deps.hubs.AcceptOwnershipTransfer)
	protected.HandleFunc("POST /v1/hubs/{id}/ownership-transfer/{transfer_id}/cancel", deps.hubs.CancelOwnershipTransfer)
	protected.HandleFunc("GET /v1/hubs/{id}/invites", deps.hubs.ListInvites)
	protected.HandleFunc("POST /v1/hubs/{id}/invites", deps.hubs.CreateInvite)
	protected.HandleFunc("DELETE /v1/hubs/{id}/invites/{invite_id}", deps.hubs.RevokeInvite)
	protected.HandleFunc("POST /v1/hubs/{id}/invites/{invite_id}/regenerate", deps.hubs.RegenerateInvite)
	protected.HandleFunc("POST /v1/hubs/{id}/invites/{invite_id}/resend", deps.hubs.ResendInvite)
	protected.HandleFunc("POST /v1/invites/{token}/accept", deps.hubs.AcceptInvite)

	// GET /v1/invites/{token} is public; anyone with the link can see
	// details. Per-IP limit keeps brute-force token enumeration in
	// check — tokens are high-entropy so this is defense-in-depth.
	ipLimit := ipLimitFactory(deps)
	root.HandleFunc("GET /v1/invites/{token}", ipLimit(ratelimit.IPInviteLimit, deps.hubs.GetInvite))
}

func registerProtectedMounts(root *http.ServeMux, protected *http.ServeMux, withAuth func(http.Handler) http.Handler) {
	root.Handle("/v1/memories", withAuth(protected))
	root.Handle("/v1/memories/", withAuth(protected))
	// Bar search endpoint — handler lives in registerMemoryRoutes
	// next to memories/search because they share a row-shape
	// type. The mount needs its own prefix because the ServeMux
	// doesn't merge sibling paths under a common ancestor.
	root.Handle("/v1/bar/", withAuth(protected))
	root.Handle("/v1/uploads", withAuth(protected))
	root.Handle("/v1/topics", withAuth(protected))
	root.Handle("/v1/topics/", withAuth(protected))
	root.Handle("/v1/recall", withAuth(protected))
	root.Handle("/v1/ask", withAuth(protected))
	root.Handle("/v1/events", withAuth(protected))
	root.Handle("/v1/events/", withAuth(protected))
	root.Handle("/v1/dreams", withAuth(protected))
	root.Handle("/v1/dreams/", withAuth(protected))
	root.Handle("/v1/chat", withAuth(protected))
	root.Handle("/v1/chat/", withAuth(protected))
	root.Handle("/v1/notifications", withAuth(protected))
	root.Handle("/v1/notifications/", withAuth(protected))
	root.Handle("/v1/onboarding/", withAuth(protected))
	root.Handle("/v1/settings", withAuth(protected))
	root.Handle("/v1/usage", withAuth(protected))
	root.Handle("/v1/usage/", withAuth(protected))
	root.Handle("/v1/configs", withAuth(protected))
	root.Handle("/v1/configs/", withAuth(protected))
	root.Handle("/v1/personas", withAuth(protected))
	root.Handle("/v1/personas/", withAuth(protected))
	root.Handle("/v1/hubs", withAuth(protected))
	root.Handle("/v1/hubs/", withAuth(protected))
	root.Handle("/v1/invites/", withAuth(protected))
	root.Handle("/v1/account/", withAuth(protected))
}

func registerAgentRoutes(root *http.ServeMux, protected *http.ServeMux, withAuth func(http.Handler) http.Handler, deps routeDeps) {
	protected.HandleFunc("GET /v1/agents", deps.agents.List)
	protected.HandleFunc("PATCH /v1/agents/{slug}", deps.agents.Update)
	protected.HandleFunc("DELETE /v1/agents/{slug}", deps.agents.Disconnect)
	root.Handle("/v1/agents", withAuth(protected))
	root.Handle("/v1/agents/", withAuth(protected))
}

func registerMCPRoutes(root *http.ServeMux, withAuth func(http.Handler) http.Handler, deps routeDeps) {
	mcpH := handler.NewMCPHandler(deps.store, deps.recall, deps.memories, deps.eventsBroker)
	chatGPTH := handler.NewChatGPTMCPHandler(deps.store, deps.recall, deps.memories, deps.eventsBroker)
	// Wire the meter's LogEvent into the MCP handlers so tool calls
	// (push/recall/capture) write usage_events with a populated
	// metadata.summary. The HTTP meter middleware sits in withAuth but
	// it classifies only REST paths — MCP's /mcp JSON-RPC endpoint
	// falls through with no logging. Without this, agent cards never
	// show "Recalled X" / "Saved Y" for MCP-native agents, which is
	// most of them. Nil-safe on both sides — dev/memory-mode skips.
	if deps.meter != nil {
		mcpH.SetLogEvent(deps.meter.LogEvent)
		chatGPTH.SetLogEvent(deps.meter.LogEvent)
	}
	mcpProtected := http.NewServeMux()
	mcpProtected.Handle("/mcp", mcpH)
	mcpProtected.Handle("/mcp/chatgpt", chatGPTH)
	root.Handle("/mcp", withAuth(mcpProtected))
	root.Handle("/mcp/chatgpt", withAuth(mcpProtected))
}

func registerAuthRoutes(root *http.ServeMux, protected *http.ServeMux, deps routeDeps) {
	if deps.auth == nil {
		authUnavailable := func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error":{"code":"auth_unavailable","message":"Authentication requires a database. Set DATABASE_URL to enable auth."}}`))
		}
		root.HandleFunc("GET /v1/auth/github", authUnavailable)
		root.HandleFunc("GET /v1/auth/github/callback", authUnavailable)
		root.HandleFunc("GET /v1/auth/google", authUnavailable)
		root.HandleFunc("GET /v1/auth/google/callback", authUnavailable)
		root.HandleFunc("POST /v1/auth/email/request", authUnavailable)
		root.HandleFunc("POST /v1/auth/email/verify", authUnavailable)
		root.HandleFunc("GET /v1/auth/me", authUnavailable)
		root.HandleFunc("POST /v1/auth/refresh", authUnavailable)
		root.HandleFunc("POST /v1/auth/exchange", authUnavailable)
		return
	}

	ipLimit := ipLimitFactory(deps)

	root.HandleFunc("GET /v1/auth/github", ipLimit(ratelimit.IPLoginLimit, deps.auth.GitHubLogin))
	root.HandleFunc("GET /v1/auth/github/callback", ipLimit(ratelimit.IPCallbackLimit, deps.auth.GitHubCallback))
	root.HandleFunc("GET /v1/auth/google", ipLimit(ratelimit.IPLoginLimit, deps.auth.GoogleLogin))
	root.HandleFunc("GET /v1/auth/google/callback", ipLimit(ratelimit.IPCallbackLimit, deps.auth.GoogleCallback))
	// Email OTP — third sign-in path. Two endpoints, both with
	// per-IP RPM caps; verify is more permissive than request
	// because retrying a typo is a normal user behavior, while
	// requesting many codes is the inbox-bomb / enumeration vector.
	root.HandleFunc("POST /v1/auth/email/request", ipLimit(ratelimit.IPEmailOTPRequest, deps.auth.RequestEmailOTP))
	root.HandleFunc("POST /v1/auth/email/verify", ipLimit(ratelimit.IPEmailOTPVerify, deps.auth.VerifyEmailOTP))
	root.HandleFunc("GET /v1/auth/me", deps.auth.Me)
	root.HandleFunc("PATCH /v1/auth/me", deps.auth.UpdateMe)
	root.HandleFunc("POST /v1/auth/refresh", ipLimit(ratelimit.IPTokenLimit, deps.auth.Refresh))
	root.HandleFunc("POST /v1/auth/exchange", ipLimit(ratelimit.IPTokenLimit, deps.auth.ExchangeCode))
	root.HandleFunc("POST /v1/auth/impersonate", ipLimit(ratelimit.IPImpersonate, deps.auth.Impersonate))

	// Auth identity management (protected — requires authenticated session)
	authIdentity := http.NewServeMux()
	authIdentity.HandleFunc("GET /v1/auth/identities", deps.auth.ListIdentities)
	authIdentity.HandleFunc("GET /v1/auth/link/{provider}", deps.auth.LinkProvider)
	authIdentity.HandleFunc("DELETE /v1/auth/link/{provider}", deps.auth.UnlinkProvider)
	root.Handle("/v1/auth/identities", deps.authMiddleware(authIdentity))
	root.Handle("/v1/auth/link/", deps.authMiddleware(authIdentity))

	protected.HandleFunc("POST /v1/auth/api-keys", deps.auth.CreateAPIKey)
	protected.HandleFunc("GET /v1/auth/api-keys", deps.auth.ListAPIKeys)
	protected.HandleFunc("PATCH /v1/auth/api-keys/{id}", deps.auth.UpdateAPIKey)
	protected.HandleFunc("DELETE /v1/auth/api-keys/{id}", deps.auth.RevokeAPIKey)
	root.Handle("/v1/auth/api-keys", deps.authMiddleware(protected))
	root.Handle("/v1/auth/api-keys/", deps.authMiddleware(protected))

	mcpOAuth := handler.NewMCPOAuthHandler(deps.auth)
	deps.auth.SetMCPOAuth(mcpOAuth)
	root.HandleFunc("GET /.well-known/oauth-protected-resource", mcpOAuth.ProtectedResourceMetadata)
	root.HandleFunc("GET /.well-known/oauth-protected-resource/", mcpOAuth.ProtectedResourceMetadata)
	root.HandleFunc("GET /.well-known/oauth-authorization-server", mcpOAuth.AuthorizationServerMetadata)
	// /oauth/register is dynamic client registration — unauthenticated
	// and can be abused to create a flood of OAuth clients. Tightest
	// per-IP budget on the server.
	root.HandleFunc("POST /oauth/register", ipLimit(ratelimit.IPOAuthDCRLimit, mcpOAuth.DynamicClientRegistration))
	root.HandleFunc("GET /oauth/authorize", ipLimit(ratelimit.IPOAuthAuthorize, mcpOAuth.Authorize))
	root.HandleFunc("GET /oauth/authorize/consent-request", ipLimit(ratelimit.IPOAuthAuthorize, mcpOAuth.ConsentRequest))
	root.HandleFunc("POST /oauth/authorize/consent", ipLimit(ratelimit.IPOAuthAuthorize, mcpOAuth.Consent))
	root.HandleFunc("POST /oauth/token", ipLimit(ratelimit.IPOAuthTokenLimit, mcpOAuth.Token))
}

func registerAdminRoutes(root *http.ServeMux, deps routeDeps) {
	if deps.admin == nil || deps.auth == nil {
		return
	}

	admin := http.NewServeMux()

	// Waitlist management
	// Reindex is the admin-only embeddings rebuild. It lives here
	// (not in registerMemoryRoutes) so the /v1/admin/ prefix mount
	// on root reaches it — a registration on the shared protected
	// mux would be shadowed by that prefix and served 404.
	admin.HandleFunc("POST /v1/admin/reindex", deps.memories.Reindex)

	admin.HandleFunc("GET /v1/admin/waitlist", deps.admin.ListWaitlist)
	admin.HandleFunc("PATCH /v1/admin/waitlist/{id}", deps.admin.UpdateWaitlist)
	admin.HandleFunc("POST /v1/admin/waitlist/batch-approve", deps.admin.BatchApprove)
	admin.HandleFunc("GET /v1/admin/waitlist/stats", deps.admin.WaitlistStats)
	admin.HandleFunc("POST /v1/admin/waitlist/invite", deps.admin.InviteByEmail)
	admin.HandleFunc("DELETE /v1/admin/waitlist/{id}/invite", deps.admin.RevokeInviteByEntry)

	// Orphan-invite repair: users who registered during the
	// 2026-04-14 → 2026-04-20 window never had their waitlist
	// invite consumed because the web client dropped the token
	// during OAuth. The GET lists the cohort (preview), the POST
	// reconciles a specific id list via the same ConsumeInviteForUser
	// the login flow uses.
	if deps.adminWaitlist != nil {
		admin.HandleFunc("GET /v1/admin/waitlist/orphans", deps.adminWaitlist.ListOrphans)
		admin.HandleFunc("POST /v1/admin/waitlist/reconcile", deps.adminWaitlist.Reconcile)
		// Per-user orphan check is used by the admin user-detail
		// page to conditionally render the reconcile affordance.
		// Kept on the user path (not /v1/admin/waitlist/...) so the
		// UI can fetch it alongside the existing user-detail
		// queries.
		admin.HandleFunc("GET /v1/admin/users/{id}/orphan-status", deps.adminWaitlist.GetUserOrphanStatus)
	}

	admin.HandleFunc("POST /v1/admin/memories/{id}/regenerate-metadata", deps.memories.RegenerateMetadata)
	admin.HandleFunc("POST /v1/admin/memories/cleanup-metadata", deps.memories.CleanupMetadata)

	// User management
	if deps.adminUsers != nil {
		admin.HandleFunc("GET /v1/admin/users", deps.adminUsers.ListUsers)
		admin.HandleFunc("GET /v1/admin/users/{id}", deps.adminUsers.GetUser)
		admin.HandleFunc("GET /v1/admin/users/{id}/usage", deps.adminUsers.GetUserUsage)
		admin.HandleFunc("POST /v1/admin/users/{id}/set-plan", deps.adminUsers.SetPlan)
		admin.HandleFunc("PUT /v1/admin/users/{id}/overrides", deps.adminUsers.SetOverrides)
		admin.HandleFunc("DELETE /v1/admin/users/{id}/overrides", deps.adminUsers.DeleteOverrides)

		// Plan management
		admin.HandleFunc("GET /v1/admin/plans", deps.adminUsers.ListPlans)
		admin.HandleFunc("PATCH /v1/admin/plans/{id}", deps.adminUsers.UpdatePlan)

		if deps.adminConfig != nil {
			admin.HandleFunc("GET /v1/admin/config", deps.adminConfig.Get)
		}

		// Hub management
		admin.HandleFunc("GET /v1/admin/hubs", deps.adminUsers.ListTeamHubs)
		admin.HandleFunc("GET /v1/admin/hubs/{id}", deps.adminUsers.GetHub)
		admin.HandleFunc("GET /v1/admin/hubs/{id}/members", deps.adminUsers.ListHubMembers)
		admin.HandleFunc("GET /v1/admin/hubs/{id}/subscription", deps.adminUsers.GetHubSubscription)
		admin.HandleFunc("POST /v1/admin/hubs/{id}/set-plan", deps.adminUsers.SetHubPlan)
		admin.HandleFunc("GET /v1/admin/users/{id}/billing-hubs", deps.adminUsers.ListBillingHubs)

		// System stats
		admin.HandleFunc("GET /v1/admin/stats", deps.adminUsers.GetStats)
	}

	// Admin dreams — read-only audit views for operators.
	// ListDreamRunsForUser walks both personal + team-hub membership
	// so one query answers "show me dream activity for this user."
	// GetDreamRunDetail joins the run with its actions + every
	// dream_run_id-tagged notification.
	if deps.adminDreams != nil {
		admin.HandleFunc("GET /v1/admin/users/{id}/dream-runs", deps.adminDreams.ListDreamRunsForUser)
		admin.HandleFunc("GET /v1/admin/dream-runs/{id}", deps.adminDreams.GetDreamRunDetail)
	}

	// Admin notifications
	if deps.adminNotifications != nil {
		admin.HandleFunc("POST /v1/admin/notifications/send", deps.adminNotifications.Send)
		admin.HandleFunc("GET /v1/admin/notifications/sent", deps.adminNotifications.ListSent)
		// Plan 18 §7.3 — super-notif funnel page (parent histogram +
		// per-cohort item funnel). Operator-only; consumed by the
		// admin web app via @/lib/admin-client/.
		admin.HandleFunc("GET /v1/admin/notifications/super", deps.adminNotifications.SuperFunnel)
	}

	// Campaigns — persistent admin communications
	if deps.adminAIAssist != nil {
		admin.HandleFunc("POST /v1/admin/ai-assist/email-copy", deps.adminAIAssist.GenerateEmailCopy)
	}

	// Onboarding seed memories admin (plan 23 §5.7)
	if deps.adminSeedMemories != nil {
		admin.HandleFunc("GET /v1/admin/seed-memories", deps.adminSeedMemories.List)
		admin.HandleFunc("POST /v1/admin/seed-memories", deps.adminSeedMemories.Create)
		admin.HandleFunc("GET /v1/admin/seed-memories/{id}", deps.adminSeedMemories.Get)
		admin.HandleFunc("PATCH /v1/admin/seed-memories/{id}", deps.adminSeedMemories.Update)
		admin.HandleFunc("DELETE /v1/admin/seed-memories/{id}", deps.adminSeedMemories.Delete)
		// Replays the seed-copy worker against the calling admin's own
		// personal hub (delete prior seed copies, re-insert from current
		// templates) so the admin sees what new users would receive.
		admin.HandleFunc("POST /v1/admin/seed-memories/sync-self", deps.adminSeedMemories.SyncToSelf)
	}

	// Seed-image uploads — admin posts an image, gets a stable public URL
	// to inline in seed memory markdown (plan 26 follow-up).
	if deps.adminSeedImages != nil {
		admin.HandleFunc("POST /v1/admin/seed-images/upload", deps.adminSeedImages.Upload)
	}

	if deps.adminCampaignTemplates != nil {
		admin.HandleFunc("GET /v1/admin/campaign-templates", deps.adminCampaignTemplates.List)
		admin.HandleFunc("POST /v1/admin/campaign-templates", deps.adminCampaignTemplates.Create)
		admin.HandleFunc("GET /v1/admin/campaign-templates/{id}", deps.adminCampaignTemplates.Get)
		admin.HandleFunc("PATCH /v1/admin/campaign-templates/{id}", deps.adminCampaignTemplates.Update)
		admin.HandleFunc("POST /v1/admin/campaign-templates/{id}/archive", deps.adminCampaignTemplates.SetArchived)
		admin.HandleFunc("DELETE /v1/admin/campaign-templates/{id}", deps.adminCampaignTemplates.Delete)
	}

	if deps.adminCampaigns != nil {
		admin.HandleFunc("GET /v1/admin/campaigns", deps.adminCampaigns.List)
		admin.HandleFunc("POST /v1/admin/campaigns", deps.adminCampaigns.Create)
		admin.HandleFunc("GET /v1/admin/campaigns/{id}", deps.adminCampaigns.Get)
		admin.HandleFunc("PATCH /v1/admin/campaigns/{id}", deps.adminCampaigns.UpdateDraft)
		admin.HandleFunc("DELETE /v1/admin/campaigns/{id}", deps.adminCampaigns.Delete)
		admin.HandleFunc("POST /v1/admin/campaigns/{id}/send", deps.adminCampaigns.Send)
		admin.HandleFunc("POST /v1/admin/campaigns/{id}/schedule", deps.adminCampaigns.Schedule)
		admin.HandleFunc("POST /v1/admin/campaigns/{id}/test-send", deps.adminCampaigns.TestSend)
		admin.HandleFunc("POST /v1/admin/campaigns/{id}/cancel", deps.adminCampaigns.Cancel)
		admin.HandleFunc("GET /v1/admin/campaigns/{id}/audit", deps.adminCampaigns.Audit)
		admin.HandleFunc("GET /v1/admin/campaigns/{id}/deliveries/stats", deps.adminCampaigns.DeliveryStats)
	}

	// Ops dashboard — live worker/queue/job monitoring.
	// Read endpoints are always safe to expose while the handler is
	// constructed (InMemoryStore in dev skips construction, so the
	// nil check is the switch).
	if deps.adminOps != nil {
		admin.HandleFunc("GET /v1/admin/ops/pulse", deps.adminOps.GetPulse)
		admin.HandleFunc("GET /v1/admin/ops/stream", deps.adminOps.Stream)
		admin.HandleFunc("GET /v1/admin/ops/memory-ingestion", deps.adminOps.IngestionSnapshot)
		admin.HandleFunc("GET /v1/admin/ops/jobs", deps.adminOps.ListJobs)
		admin.HandleFunc("GET /v1/admin/ops/jobs/{id}", deps.adminOps.GetJob)
		admin.HandleFunc("GET /v1/admin/ops/jobs/{id}/logs", deps.adminOps.GetJobLogs)
		admin.HandleFunc("GET /v1/admin/ops/jobs/{id}/logs/stream", deps.adminOps.StreamJobLogs)
		admin.HandleFunc("POST /v1/admin/ops/jobs/{id}/retry", deps.adminOps.RetryJob)
		admin.HandleFunc("POST /v1/admin/ops/jobs/{id}/cancel", deps.adminOps.CancelJob)
		admin.HandleFunc("POST /v1/admin/ops/memories/{id}/force-active", deps.adminOps.ForceActiveMemory)
	}

	// Audiences — saved recipient rules
	if deps.adminAudiences != nil {
		admin.HandleFunc("GET /v1/admin/audiences", deps.adminAudiences.List)
		admin.HandleFunc("POST /v1/admin/audiences", deps.adminAudiences.Create)
		admin.HandleFunc("POST /v1/admin/audiences/estimate", deps.adminAudiences.Estimate)
		admin.HandleFunc("GET /v1/admin/audiences/{id}", deps.adminAudiences.Get)
		admin.HandleFunc("PATCH /v1/admin/audiences/{id}", deps.adminAudiences.Update)
		admin.HandleFunc("DELETE /v1/admin/audiences/{id}", deps.adminAudiences.Delete)
	}

	// Email templates
	admin.HandleFunc("GET /v1/admin/email/templates", deps.admin.ListEmailTemplates)
	admin.HandleFunc("GET /v1/admin/email/templates/{name}", deps.admin.GetEmailTemplate)
	admin.HandleFunc("POST /v1/admin/email/templates/{name}/preview", deps.admin.PreviewEmailTemplate)
	admin.HandleFunc("POST /v1/admin/email/templates/{name}/send", deps.admin.SendEmailTemplate)
	admin.HandleFunc("POST /v1/admin/email/templates/{name}/publish", deps.admin.PublishEmailTemplate)
	admin.HandleFunc("PUT /v1/admin/email/templates/{name}", deps.admin.UpdateEmailTemplate)
	admin.HandleFunc("DELETE /v1/admin/email/templates/{name}", deps.admin.ResetEmailTemplate)

	// Email brand — singleton layout settings wrapping every outbound email
	admin.HandleFunc("GET /v1/admin/email/brand", deps.admin.GetEmailBrand)
	admin.HandleFunc("PUT /v1/admin/email/brand", deps.admin.UpdateEmailBrand)

	withAdmin := deps.authMiddleware(handler.AdminMiddleware(deps.store)(admin))
	root.Handle("/v1/admin/", withAdmin)
}

// registerWebhookRoutes mounts provider-driven webhook receivers. These
// endpoints are unauthenticated at the HTTP layer — the handler enforces
// provider-specific signature verification before acting on the payload.
// Wrapped in the shared IP rate limiter so a malformed-signature storm
// can't saturate the server.
func registerWebhookRoutes(root *http.ServeMux, deps routeDeps) {
	if deps.resendWebhook == nil {
		return
	}
	ipLimit := ipLimitFactory(deps)
	root.HandleFunc("POST /v1/webhooks/resend",
		ipLimit(ratelimit.IPWebhookResend, deps.resendWebhook.Receive))
}

// registerUnsubscribeRoute mounts the public /v1/unsubscribe endpoint.
// Token-in-query is the auth — the handler resolves and rotates the
// token in one write, so an old URL stops working after first use.
// Rate-limited per IP to blunt enumeration.
func registerUnsubscribeRoute(root *http.ServeMux, deps routeDeps) {
	if deps.unsubscribe == nil {
		return
	}
	ipLimit := ipLimitFactory(deps)
	// One handler serves both methods; go ServeMux routes by method
	// prefix so we register twice against the same HandlerFunc.
	root.HandleFunc("GET /v1/unsubscribe",
		ipLimit(ratelimit.IPUnsubscribe, deps.unsubscribe.Receive))
	root.HandleFunc("POST /v1/unsubscribe",
		ipLimit(ratelimit.IPUnsubscribe, deps.unsubscribe.Receive))
}

func registerWaitlistRoutes(root *http.ServeMux, deps routeDeps) {
	if deps.waitlist == nil {
		return
	}

	// Public, unauthenticated — needs IP-level protection because the
	// submit handler writes a DB row and may enqueue email, and the
	// invite validator can be brute-forced for valid tokens.
	ipLimit := ipLimitFactory(deps)
	root.HandleFunc("POST /v1/waitlist", ipLimit(ratelimit.IPWaitlistLimit, deps.waitlist.Submit))
	root.HandleFunc("GET /v1/waitlist/invites/{token}", ipLimit(ratelimit.IPWaitlistLimit, deps.waitlist.ValidateInvite))
}

func registerPlansRoutes(root *http.ServeMux, deps routeDeps) {
	if deps.plansH == nil {
		return
	}
	// Public endpoint — no auth required (for pricing page, CLI, web app)
	root.HandleFunc("GET /v1/plans", deps.plansH.List)
}
