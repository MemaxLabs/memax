package devseed_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/devseed"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
	"github.com/MemaxLabs/memax/packages/server/internal/v2ui"
)

func TestSeedMemaxV2(t *testing.T) {
	t.Parallel()
	_, pool := testdb.Acquire(t)
	ctx := context.Background()
	l := ledger.New(pool, ledger.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))

	if _, err := devseed.SeedMemaxV2(ctx, pool, l, devseed.Options{Env: "Production"}); !errors.Is(err, devseed.ErrProduction) {
		t.Fatalf("production: %v", err)
	}
	d, err := devseed.SeedMemaxV2(ctx, pool, l, devseed.Options{Env: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	for ref, state := range map[string]string{
		"M-0219": "kept", "M-0102": "kept", "M-0098": "kept", "M-0071": "kept", "M-0112": "kept",
		"M-0187": "kept", "M-0174": "kept", "M-0436": "kept", "M-0442": "kept",
		"M-0430": "proposed", "M-0431": "proposed",
	} {
		m := d.Memory[ref]
		if m == nil || string(m.State) != state {
			t.Errorf("%s = %+v, want %s", ref, m, state)
		}
	}
	if d.Brief.Ref != "B-0043" || len(d.Targets) != 4 || d.CC.Agent != ledger.AgentClaudeCode {
		t.Errorf("brief %s, targets %d, CC %+v", d.Brief.Ref, len(d.Targets), d.CC)
	}
	// The space is on the V2 record, as POST /v2/spaces makes them, so ZZ
	// and JY see the V2 UI.
	ui := v2ui.New(pool, time.Time{})
	for _, who := range []uuid.UUID{d.ZZ, d.JY} {
		if got, err := ui.For(ctx, who); err != nil || got.UI != v2ui.V2 || got.Reason != v2ui.ReasonV2Space {
			t.Errorf("%s: %+v, %v", who, got, err)
		}
	}
	// The agents' writes name their connections.
	if rc := d.Memory["M-0436"].CreatedReceiptID; rc == [16]byte{} {
		t.Fatal("no receipt")
	}
	var actor string
	if err := pool.QueryRow(ctx, `SELECT actor_id::text FROM v2.receipts WHERE id = $1`, d.Memory["M-0436"].CreatedReceiptID).Scan(&actor); err != nil || actor != d.CC.ID.String() {
		t.Errorf("M-0436 written by %s (%v), want CC's connection %s", actor, err, d.CC.ID)
	}

	// A second run replays: nothing new.
	var receipts int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM v2.receipts`).Scan(&receipts)
	again, err := devseed.SeedMemaxV2(ctx, pool, l, devseed.Options{})
	if err != nil {
		t.Fatalf("second seed: %v", err)
	}
	var after int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM v2.receipts`).Scan(&after)
	if after != receipts || again.Memory["M-0219"].ID != d.Memory["M-0219"].ID || again.Brief.Ref != "B-0043" {
		t.Errorf("second seed wrote %d receipts", after-receipts)
	}
	if !strings.Contains(string(d.Memory["M-0442"].Applies), "packages/web/**") {
		t.Errorf("M-0442 scope = %s", d.Memory["M-0442"].Applies)
	}
}
