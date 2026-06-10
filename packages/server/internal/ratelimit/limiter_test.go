package ratelimit

import (
	"context"
	"testing"
	"time"
)

func TestClassifyRequest(t *testing.T) {
	tests := []struct {
		method string
		path   string
		want   OpClass
	}{
		// Heavy ops
		{"POST", "/v1/memories", OpClassHeavy},
		{"POST", "/v1/memories/", OpClassHeavy},
		{"POST", "/v1/recall", OpClassHeavy},
		{"POST", "/v1/recall/", OpClassHeavy},
		{"POST", "/v1/ask", OpClassHeavy},
		{"POST", "/v1/ask/", OpClassHeavy},
		// Light ops
		{"GET", "/v1/memories", OpClassLight},
		{"GET", "/v1/memories/abc", OpClassLight},
		{"DELETE", "/v1/memories/abc", OpClassLight},
		{"PATCH", "/v1/memories/abc", OpClassLight},
		{"POST", "/v1/memories/batch-delete", OpClassLight},
		{"POST", "/v1/memories/batch-move", OpClassLight},
		// Metadata ops
		{"GET", "/v1/hubs", OpClassMetadata},
		{"GET", "/v1/hubs/abc", OpClassMetadata},
		{"GET", "/v1/hubs/abc/members", OpClassMetadata},
		// Explicit data-plane POST → light
		{"POST", "/v1/topics", OpClassLight},
		// Control-plane routes are intentionally exempt
		{"POST", "/v1/configs/sync", OpClassNone},
		{"PATCH", "/v1/settings", OpClassNone},
		{"POST", "/v1/notifications/seen", OpClassNone},
		{"POST", "/v1/invites/token/accept", OpClassNone},
	}
	for _, tt := range tests {
		got := ClassifyRequest(tt.method, tt.path)
		if got != tt.want {
			t.Errorf("ClassifyRequest(%q, %q) = %q, want %q", tt.method, tt.path, got, tt.want)
		}
	}
}

func TestCheck_NilRedis_AllowsAll(t *testing.T) {
	l := New(nil)
	result := l.Check(context.Background(), "user:heavy", 10)
	if !result.Allowed {
		t.Fatal("nil redis should allow all requests")
	}
	if result.Limit != 10 {
		t.Errorf("expected limit 10, got %d", result.Limit)
	}
	if result.Remaining != 10 {
		t.Errorf("expected remaining 10, got %d", result.Remaining)
	}
}

func TestCheck_ZeroLimit_AllowsAll(t *testing.T) {
	l := New(nil)
	result := l.Check(context.Background(), "user:heavy", 0)
	if !result.Allowed {
		t.Fatal("zero limit should allow (fail open)")
	}
}

func TestResult_RetryAfter(t *testing.T) {
	result := Result{
		Allowed:    false,
		RetryAfter: 15 * time.Second,
	}
	if result.RetryAfter < time.Second {
		t.Fatal("retry after should be at least 1 second")
	}
}

func TestNextWindowBoundary(t *testing.T) {
	// A time 30 seconds into a minute should have next boundary at the next minute
	now := time.Unix(1000*60+30, 0) // 30 seconds into minute 1000
	next := nextWindowBoundary(now)
	expected := time.Unix(1001*60, 0) // start of minute 1001
	if !next.Equal(expected) {
		t.Errorf("nextWindowBoundary(%v) = %v, want %v", now, next, expected)
	}
}

func TestApplyPathOverride(t *testing.T) {
	tests := []struct {
		name         string
		method       string
		path         string
		defaultLimit int
		want         int
	}{
		{"ask tighter than heavy default", "POST", "/v1/ask", 120, 30},
		{"ask: trailing slash normalized", "POST", "/v1/ask/", 120, 30},
		{"recall unaffected (no override)", "POST", "/v1/recall", 120, 120},
		{"override floors to plan when plan is tighter", "POST", "/v1/ask", 10, 10}, // plan wins when plan < override
		{"override ignored for non-matching method", "GET", "/v1/ask", 120, 120},
		{"batch-delete override applied", "POST", "/v1/memories/batch-delete", 120, 15},
		{"zero default passes through", "POST", "/v1/ask", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := applyPathOverride(tt.method, tt.path, tt.defaultLimit)
			if got != tt.want {
				t.Errorf("applyPathOverride(%s %q, %d) = %d, want %d",
					tt.method, tt.path, tt.defaultLimit, got, tt.want)
			}
		})
	}
}

func TestApplyPathOverrideReportsWhenApplied(t *testing.T) {
	_, applied := applyPathOverride("POST", "/v1/memories/batch-delete", 120)
	if !applied {
		t.Fatal("expected batch-delete override to report applied=true")
	}

	_, applied = applyPathOverride("POST", "/v1/memories/batch-delete", 10)
	if applied {
		t.Fatal("expected override above plan limit to report applied=false")
	}

	_, applied = applyPathOverride("GET", "/v1/memories", 120)
	if applied {
		t.Fatal("expected no override for regular light endpoint")
	}
}

func TestStartsWithPrefix(t *testing.T) {
	tests := []struct {
		s, prefix string
		want      bool
	}{
		{"/v1/hubs/abc", "/v1/hubs/", true},
		{"/v1/hubs", "/v1/hubs/", false},
		{"/v1/memories", "/v1/hubs/", false},
		{"", "/v1/", false},
	}
	for _, tt := range tests {
		got := startsWithPrefix(tt.s, tt.prefix)
		if got != tt.want {
			t.Errorf("startsWithPrefix(%q, %q) = %v, want %v", tt.s, tt.prefix, got, tt.want)
		}
	}
}
