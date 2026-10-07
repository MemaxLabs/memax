package spacemode_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/spacemode"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

func TestSwitchIsPerSpace(t *testing.T) {
	t.Parallel()
	_, pool := testdb.Acquire(t)
	ctx := context.Background()
	owner := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, name) VALUES ($1, $2, 'o')`, owner, owner.String()[:8]+"@mode.test"); err != nil {
		t.Fatal(err)
	}
	hub := func(name string) uuid.UUID {
		id := uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO hubs (id, name, slug, hub_type, owner_id) VALUES ($1, $2, $3, 'team', $4)`,
			id, name, name+"-"+id.String()[:8], owner); err != nil {
			t.Fatal(err)
		}
		return id
	}
	a, b := hub("a"), hub("b")
	r := spacemode.New(pool)

	got, err := r.V2Spaces(ctx, []string{a.String(), b.String(), "not-a-uuid"})
	if err != nil || len(got) != 0 {
		t.Fatalf("before the switch: %v %v", got, err)
	}
	first := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	if err := r.Enable(ctx, a, first); err != nil {
		t.Fatal(err)
	}
	if err := r.Enable(ctx, a, first.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var at time.Time
	if err := pool.QueryRow(ctx, `SELECT v2_enabled_at FROM hubs WHERE id = $1`, a).Scan(&at); err != nil || !at.Equal(first) {
		t.Errorf("switch time = %v (%v), want the first switch %v", at, err, first)
	}
	got, _ = r.V2Spaces(ctx, []string{a.String(), b.String()})
	if !got[a.String()] || got[b.String()] {
		t.Errorf("after switching a: %v", got)
	}
	if ok, _ := r.IsV2(ctx, b.String()); ok {
		t.Error("b switched with a")
	}
	if err := r.Disable(ctx, a); err != nil {
		t.Fatal(err)
	}
	if ok, _ := r.IsV2(ctx, a.String()); ok {
		t.Error("a still on V2 after Disable")
	}
	if err := r.Enable(ctx, uuid.New(), first); err != spacemode.ErrNoSpace {
		t.Errorf("unknown space: %v", err)
	}

	var nilResolver *spacemode.Resolver
	if m, err := nilResolver.V2Spaces(ctx, []string{a.String()}); err != nil || len(m) != 0 {
		t.Errorf("nil resolver: %v %v", m, err)
	}
}
