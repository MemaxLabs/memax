package testdb

import (
	"context"
	"testing"
	"time"
)

func TestCreatedAt(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind string
		at   time.Time
		ok   bool
	}{
		{"memax_test_1759800000000000000_42", "test", time.Unix(0, 1759800000000000000), true},
		{"memax_copy_1759800000000000000_7", "copy", time.Unix(0, 1759800000000000000), true},
		{"memax_migtest_1759800000000000000_9", "migtest", time.Unix(0, 1759800000000000000), true},
		{"memax_tpl_1759800000000000000_123", "tpl", time.Unix(0, 1759800000000000000), true},
		{"memax_tpl_a1b2c3d4e5f6_1759800000", "tpl_hash", time.Unix(1759800000, 0), true},
		{"memax", "", time.Time{}, false},
		{"memax_embedded_check", "", time.Time{}, false},
	} {
		at, kind, ok := createdAt(tc.name)
		if ok != tc.ok || kind != tc.kind || !at.Equal(tc.at) {
			t.Errorf("createdAt(%q) = %v, %q, %v; want %v, %q, %v", tc.name, at, kind, ok, tc.at, tc.kind, tc.ok)
		}
	}
}

func TestMigrationsHashIsStable(t *testing.T) {
	a, err := migrationsHash(findMigrationsDir())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := migrationsHash(findMigrationsDir())
	if a != b || len(a) != 12 {
		t.Fatalf("hash %q then %q, want the same 12 characters", a, b)
	}
	// Even an all-digit hash must read as a hash, never as a timestamp.
	if _, kind, ok := createdAt("memax_tpl_" + a + "_1759800000"); !ok || kind != "tpl_hash" {
		t.Fatalf("createdAt on this template = %q, %v; want tpl_hash", kind, ok)
	}
	if _, kind, _ := createdAt("memax_tpl_123456789012_1759800000"); kind != "tpl_hash" {
		t.Fatalf("an all-digit hash read as %q; want tpl_hash", kind)
	}
}

// Two builds for the same migrations find one template: processes and
// worktrees share it instead of leaving one each behind.
func TestTemplateIsShared(t *testing.T) {
	ctx := context.Background()
	admin, err := getAdminPool(ctx)
	if err != nil {
		t.Skipf("Postgres unavailable: %v", err)
	}
	first, err := buildTemplate(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildTemplate(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("built %q, then %q; want one shared template", first, second)
	}
}
