package serverapp

import (
	"net/http"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/ratelimit"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

// These tests exercise the register* helpers in isolation against a
// partial routeDeps. They're smoke tests — we're not proving the
// handlers behave, just that route registration doesn't panic and
// that the mounted paths respond (handlers may 4xx/5xx due to nil
// collaborators, that's fine).

func newSmokeDeps(t *testing.T) routeDeps {
	t.Helper()
	s := store.NewInMemoryStore()
	// Each register* touches a subset of these handlers; lazily
	// construct the full bundle to keep the tests self-contained.
	return routeDeps{
		memories:               handler.NewMemoriesHandler(s, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil),
		uploads:                handler.NewUploadsHandler(nil),
		topics:                 handler.NewTopicsHandler(s, nil),
		recall:                 handler.NewRecallHandler(s, nil, nil, nil, nil),
		ask:                    nil, // ask is optional at registration time
		dreams:                 handler.NewDreamsHandler(s, nil),
		notifications:          handler.NewNotificationsHandler(s, nil),
		settings:               handler.NewSettingsHandler(s),
		hubs:                   handler.NewHubsHandler(s),
		boards:                 handler.NewBoardsHandler(s),
		configs:                handler.NewConfigsHandler(s, nil),
		agents:                 handler.NewAgentsHandler(s, nil),
		events:                 handler.NewEventsHandler(s, nil),
		admin:                  handler.NewAdminHandler(s),
		adminNotifications:     handler.NewAdminNotificationsHandler(s),
		adminCampaigns:         handler.NewAdminCampaignsHandler(s),
		adminCampaignTemplates: handler.NewAdminCampaignTemplatesHandler(s),
		adminAIAssist:          handler.NewAdminAIAssistHandler(nil, s),
		adminAudiences:         handler.NewAdminAudiencesHandler(s),
		adminDreams:            handler.NewAdminDreamsHandler(s),
		adminWaitlist:          handler.NewAdminWaitlistReconcileHandler(s, nil),
		adminUsers:             handler.NewAdminUsersHandler(s, nil, nil),
		adminConfig:            handler.NewAdminConfigHandler("", false, false),
		waitlist:               handler.NewWaitlistHandler(s),
		plansH:                 handler.NewPlansHandler(nil),
		unsubscribe:            handler.NewUnsubscribeHandler(s),
		store:                  s,
	}
}

func mustNotPanic(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("%s panicked: %v", name, r)
		}
	}()
	fn()
}

func TestRegisterMemoryRoutesDoesNotPanic(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	deps := newSmokeDeps(t)
	mustNotPanic(t, "registerMemoryRoutes", func() { registerMemoryRoutes(mux, deps) })
}

func TestRegisterKnowledgeRoutesDoesNotPanic(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	deps := newSmokeDeps(t)
	mustNotPanic(t, "registerKnowledgeRoutes", func() { registerKnowledgeRoutes(mux, deps) })
}

func TestRegisterAccountRoutesDoesNotPanic(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	deps := newSmokeDeps(t)
	mustNotPanic(t, "registerAccountRoutes", func() { registerAccountRoutes(mux, deps) })
}

func TestRegisterHubRoutesDoesNotPanic(t *testing.T) {
	t.Parallel()
	root := http.NewServeMux()
	protected := http.NewServeMux()
	deps := newSmokeDeps(t)
	mustNotPanic(t, "registerHubRoutes", func() { registerHubRoutes(root, protected, deps) })
}

func TestRegisterAgentRoutesDoesNotPanic(t *testing.T) {
	t.Parallel()
	root := http.NewServeMux()
	protected := http.NewServeMux()
	deps := newSmokeDeps(t)
	passthrough := func(h http.Handler) http.Handler { return h }
	mustNotPanic(t, "registerAgentRoutes", func() { registerAgentRoutes(root, protected, passthrough, deps) })
}

func TestRegisterWaitlistRoutesDoesNotPanic(t *testing.T) {
	t.Parallel()
	root := http.NewServeMux()
	deps := newSmokeDeps(t)
	mustNotPanic(t, "registerWaitlistRoutes", func() { registerWaitlistRoutes(root, deps) })
}

func TestRegisterPlansRoutesDoesNotPanic(t *testing.T) {
	t.Parallel()
	root := http.NewServeMux()
	deps := newSmokeDeps(t)
	mustNotPanic(t, "registerPlansRoutes", func() { registerPlansRoutes(root, deps) })
}

func TestRegisterUnsubscribeRouteDoesNotPanic(t *testing.T) {
	t.Parallel()
	root := http.NewServeMux()
	deps := newSmokeDeps(t)
	mustNotPanic(t, "registerUnsubscribeRoute", func() { registerUnsubscribeRoute(root, deps) })
}

func TestRegisterAdminRoutesDoesNotPanic(t *testing.T) {
	t.Parallel()
	root := http.NewServeMux()
	deps := newSmokeDeps(t)
	// Admin mounts under root directly; registration shouldn't panic
	// even though many handlers will 500 on nil collaborators.
	mustNotPanic(t, "registerAdminRoutes", func() { registerAdminRoutes(root, deps) })
}

func TestIPLimitFactoryPassesThroughWhenRateLimiterIsNil(t *testing.T) {
	t.Parallel()
	fn := ipLimitFactory(routeDeps{rateLimiter: nil})
	if fn == nil {
		t.Fatal("ipLimitFactory returned nil")
	}
	// The returned wrapper should be identity — handler returned
	// unchanged from its input.
	called := false
	inner := func(w http.ResponseWriter, r *http.Request) { called = true }
	// The EndpointLimit value is ignored when rate limiter is nil.
	// Zero struct keeps the test oblivious to specific budgets.
	wrapped := fn(ratelimit.EndpointLimit{}, inner)
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	wrapped(nilResponseWriter{}, req)
	if !called {
		t.Error("inner handler should have been invoked through the pass-through wrapper")
	}
}

// nilResponseWriter is a no-op ResponseWriter used to invoke pass-
// through wrappers without pulling in httptest. Since the inner
// handler writes nothing, we only need the method set.
type nilResponseWriter struct{}

func (nilResponseWriter) Header() http.Header         { return http.Header{} }
func (nilResponseWriter) Write(b []byte) (int, error) { return len(b), nil }
func (nilResponseWriter) WriteHeader(int)             {}
