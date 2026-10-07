// Command v2-reapply-forgets re-applies the forget ledger after a database
// restore (plan 25 §5.13 step 4). A point-in-time restore brings back the
// words of every memory forgotten after the restore point; this forgets
// them again. It reads every Forget's op (IDs and refs, never words) from
// object storage, where the propagation job copied it and a restore can't
// take it back, and from the tombstones, then re-applies each, oldest
// first, as Memax: whatever is back is forgotten again with new receipts,
// a deleted space is retired again and its hub deleted, and what is still
// forgotten is left alone. Run it after every restore, before the API and
// the worker take traffic.
//
//	DATABASE_URL=… S3_BUCKET=… go run ./cmd/v2-reapply-forgets -all
//	DATABASE_URL=… S3_BUCKET=… go run ./cmd/v2-reapply-forgets -space <uuid> [-space <uuid> …]
//	… -dry-run lists the ops without writing anything
//
// Without object storage (S3_*) only the tombstones are read, which a
// restore rolls back too: it says so, and exits 1 unless -db-only is set.
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

	"github.com/MemaxLabs/memax/packages/server/internal/forget"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/objectstore"
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
	flag.Var(&spaces, "space", "a space (hub) whose forgets to re-apply (repeatable)")
	all := flag.Bool("all", false, "re-apply every space's forgets")
	dryRun := flag.Bool("dry-run", false, "list the ops without writing anything")
	dbOnly := flag.Bool("db-only", false, "run without object storage, from the tombstones alone")
	flag.Parse()
	if len(spaces) == 0 && !*all {
		fmt.Fprintln(os.Stderr, "usage: v2-reapply-forgets -space <uuid> [-space <uuid> …] | -all [-dry-run] [-db-only]")
		os.Exit(2)
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		slog.Error("DATABASE_URL is required")
		os.Exit(1)
	}
	store := objectstore.NewFromEnv()
	if store == nil && !*dbOnly {
		slog.Error("object storage isn't configured (S3_ACCESS_KEY, S3_SECRET_KEY, S3_BUCKET): the forget ledger's " +
			"restore-proof copy is there. Set them, or pass -db-only to re-apply from the tombstones alone.")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		slog.Error("connect", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	rep, err := forget.Reapply(ctx, ledger.New(pool), pool, store,
		forget.ReapplyOptions{Spaces: spaces, DryRun: *dryRun}, slog.Default())
	fmt.Printf("ops %d (object storage %d): re-applied %d, unchanged %d, spaces gone %d, hubs deleted %d\n",
		rep.Ops, rep.FromObjectStore, len(rep.Reapplied), len(rep.Unchanged), len(rep.Gone), len(rep.HubsDeleted))
	if err != nil {
		slog.Error("re-apply the forget ledger", "error", err)
		os.Exit(1)
	}
}
