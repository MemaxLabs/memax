package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/store"
)

type fakeV2Metrics struct {
	from, to time.Time
	day      time.Time
	err      error
}

func (f *fakeV2Metrics) GetProductMetrics(_ context.Context, from, to time.Time) (ledger.ProductMetrics, error) {
	f.from, f.to = from, to
	if f.err != nil {
		return ledger.ProductMetrics{}, f.err
	}
	p50 := 180.0
	return ledger.ProductMetrics{From: from, To: to, AsOf: to, FirstSessionHours: 24, Review: []ledger.ReviewHealth{},
		Cohorts: []ledger.CohortMetrics{{Week: from, Kind: ledger.CohortNew, People: 3, SessionsClosed: 3, Activated: 2,
			FirstFiles: 2, FirstFilesUnder5m: 2, FirstFileP50Seconds: &p50}},
		Totals: []ledger.CohortMetrics{{Kind: ledger.CohortNew, People: 3, SessionsClosed: 3, Activated: 2, FirstFiles: 2,
			FirstFilesUnder5m: 2, FirstFileP50Seconds: &p50}}}, nil
}

func (f *fakeV2Metrics) GetReadMetrics(_ context.Context, day time.Time) (ledger.ReadMetrics, error) {
	f.day = day
	return ledger.ReadMetrics{Day: time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC),
		SpacesRead: 4, SpacesTwoAgents: 2, ConnectionsReading: 3, ConnectionsSeen: 4}, nil
}

func getV2Metrics(t *testing.T, h *AdminV2MetricsHandler, query string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	h.Get(w, httptest.NewRequest(http.MethodGet, "/v1/admin/v2/metrics"+query, nil))
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s: %v", w.Body.String(), err)
	}
	return w, body
}

func TestAdminV2Metrics(t *testing.T) {
	f := &fakeV2Metrics{}
	h := NewAdminV2MetricsHandler(f)
	h.now = func() time.Time { return time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC) } // a Wednesday

	// Without a range: the last eight weeks, this one included.
	w, body := getV2Metrics(t, h, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if got := fmt.Sprintf("%s..%s", f.from.Format(time.DateOnly), f.to.Format(time.DateOnly)); got != "2026-08-17..2026-10-12" {
		t.Errorf("default range = %s", got)
	}
	if f.day.Format(time.DateOnly) != "2026-10-06" {
		t.Errorf("north star day = %s, want the week ending yesterday", f.day)
	}
	data := body["data"].(map[string]any)
	for _, k := range []string{"from", "to", "as_of", "first_session_hours", "cohorts", "totals", "review", "review_total", "gates", "north_star"} {
		if _, ok := data[k]; !ok {
			t.Errorf("no %s in %v", k, data)
		}
	}
	gates := data["gates"].([]any)
	if len(gates) != 4 {
		t.Fatalf("gates = %v", gates)
	}
	if g := gates[0].(map[string]any); g["name"] != "activation" || g["status"] != "pass" || g["numerator"] != 2.0 || g["denominator"] != 3.0 {
		t.Errorf("activation gate = %v", g)
	}
	if ns := data["north_star"].(map[string]any); ns["week_ending"] != "2026-10-06" || ns["spaces_two_agents"] != 2.0 || ns["coverage"] != 0.75 {
		t.Errorf("north star = %v", ns)
	}
	if c := data["cohorts"].([]any)[0].(map[string]any); c["week"] != "2026-08-17T00:00:00Z" || c["cohort"] != "new" {
		t.Errorf("cohort = %v", c)
	}
	if tot := data["totals"].([]any)[0].(map[string]any); tot["week"] != nil {
		t.Errorf("a total has a week: %v", tot)
	}

	// A range is two dates, to exclusive.
	if w, _ := getV2Metrics(t, h, "?from=2026-10-12&to=2026-12-07"); w.Code != http.StatusOK ||
		f.from.Format(time.DateOnly) != "2026-10-12" || f.to.Format(time.DateOnly) != "2026-12-07" {
		t.Errorf("range: %d %s..%s", w.Code, f.from, f.to)
	}
	for _, q := range []string{"?from=last-week", "?to=2026-13-01"} {
		if w, body := getV2Metrics(t, h, q); w.Code != http.StatusBadRequest || body["error"].(map[string]any)["code"] != "invalid_range" {
			t.Errorf("%s: %d %v", q, w.Code, body)
		}
	}
	for err, status := range map[error]int{
		fmt.Errorf("%w: range: backwards", ledger.ErrInvalid): http.StatusBadRequest,
		ledger.ErrDisabled:                                    http.StatusServiceUnavailable,
		errors.New("connection reset"):                        http.StatusInternalServerError,
	} {
		f.err = err
		if w, body := getV2Metrics(t, h, ""); w.Code != status || strings.Contains(w.Body.String(), "connection reset") {
			t.Errorf("%v: %d %v", err, w.Code, body)
		}
	}
}

// Behind the admin sub-mux, only admins get it.
func TestAdminV2MetricsNeedsAdmin(t *testing.T) {
	h := NewAdminV2MetricsHandler(&fakeV2Metrics{})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/admin/v2/metrics", h.Get)
	guarded := AdminMiddleware(store.NewInMemoryStore())(mux)

	for name, req := range map[string]*http.Request{
		"no session": httptest.NewRequest(http.MethodGet, "/v1/admin/v2/metrics", nil),
		"not an admin": func() *http.Request {
			r := httptest.NewRequest(http.MethodGet, "/v1/admin/v2/metrics", nil)
			return r.WithContext(context.WithValue(r.Context(), userIDKey, "person-1"))
		}(),
	} {
		w := httptest.NewRecorder()
		guarded.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized && w.Code != http.StatusForbidden {
			t.Errorf("%s: %d", name, w.Code)
		}
		if strings.Contains(w.Body.String(), "cohorts") {
			t.Errorf("%s got the metrics", name)
		}
	}
}
