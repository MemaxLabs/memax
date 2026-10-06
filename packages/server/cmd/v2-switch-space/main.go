// Command v2-switch-space moves a space to the V2 record, or back, for
// development and internal dogfooding (plan 25 §10). A space on V2 is
// served through the ledger by the MCP server; every other space keeps V1's
// behaviour. The real "Switch to V2" step, with the import cleanup, is epic
// 2.8; until then this is how a space switches.
//
//	DATABASE_URL=… go run ./cmd/v2-switch-space -space <uuid> [-space <uuid> …]
//	DATABASE_URL=… go run ./cmd/v2-switch-space -space <uuid> -off
//
// Connect the space's agents too (cmd/v2-backfill-agents), or they only
// read there.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/spacemode"
)

type spaceList []uuid.UUID

func (s *spaceList) String() string { return fmt.Sprint([]uuid.UUID(*s)) }

func (s *spaceList) Set(v string) error {
	id, err := uuid.Parse(strings.TrimSpace(v))
	if err != nil {
		return fmt.Errorf("%q is not a space id", v)
	}
	*s = append(*s, id)
	return nil
}

func main() {
	var spaces spaceList
	flag.Var(&spaces, "space", "the space (hub) to switch (repeatable)")
	off := flag.Bool("off", false, "move the spaces back to V1 instead")
	flag.Parse()
	if len(spaces) == 0 {
		fmt.Fprintln(os.Stderr, "usage: v2-switch-space -space <uuid> [-space <uuid> …] [-off]")
		os.Exit(2)
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		slog.Error("DATABASE_URL is required")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		slog.Error("connect", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	r := spacemode.New(pool)
	failed := false
	for _, id := range spaces {
		if *off {
			err = r.Disable(ctx, id)
		} else {
			err = r.Enable(ctx, id, time.Now())
		}
		if err != nil {
			slog.Error("switch failed", "space", id.String(), "error", err)
			failed = true
			continue
		}
		record := "V2"
		if *off {
			record = "V1"
		}
		fmt.Printf("%s is on %s\n", id, record)
	}
	if failed {
		os.Exit(1)
	}
}
