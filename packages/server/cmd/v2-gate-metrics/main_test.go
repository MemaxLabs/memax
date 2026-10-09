package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger/ledgertest"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

var update = flag.Bool("update", false, "rewrite testdata/fixture.golden")

// The gate table for ledgertest's fixture, as the command prints it.
func TestGateTable(t *testing.T) {
	t.Parallel()
	_, pool := testdb.Acquire(t)
	ledgertest.SeedGateFixture(t, pool)
	ctx := context.Background()
	var out bytes.Buffer
	if err := run(ctx, pool, []string{"-from", "2026-08-03", "-to", "2026-10-05", "-as-of", "2026-10-03"}, &out, time.Now); err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "fixture.golden")
	if *update {
		if err := os.WriteFile(golden, out.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != string(want) {
		t.Errorf("got:\n%s\nwant:\n%s", out.String(), want)
	}

	// -json carries the same numbers.
	out.Reset()
	if err := run(ctx, pool, []string{"-from", "2026-08-03", "-to", "2026-10-05", "-as-of", "2026-10-03", "-json"}, &out, time.Now); err != nil {
		t.Fatal(err)
	}
	var j struct {
		Gates []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"gates"`
		Totals []struct {
			People int `json:"people"`
		} `json:"totals"`
	}
	if err := json.Unmarshal(out.Bytes(), &j); err != nil || len(j.Gates) != 4 || j.Gates[0].Status != "fail" ||
		len(j.Totals) != 2 || j.Totals[0].People != 10 {
		t.Errorf("json: %+v %v\n%s", j, err, out.String())
	}

	// Without a range: the eight signup weeks up to the as-of moment's.
	out.Reset()
	if err := run(ctx, pool, []string{"-as-of", "2026-10-03"}, &out, time.Now); err != nil ||
		!bytes.Contains(out.Bytes(), []byte("from 2026-08-10 up to 2026-10-05")) {
		t.Errorf("default range: %v\n%s", err, out.String())
	}
}

func TestUsage(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"-from", "August"},
		{"-as-of", "soon"},
		{"-from", "2026-10-05", "-to", "2026-10-01"},
		{"-unknown"},
	} {
		var usage usageError
		if err := run(context.Background(), nil, args, &bytes.Buffer{}, time.Now); !errors.As(err, &usage) {
			t.Errorf("%v: %v", args, err)
		}
	}
}
