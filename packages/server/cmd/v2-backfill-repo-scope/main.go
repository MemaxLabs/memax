// Command v2-backfill-repo-scope ties the personal facts an earlier
// `memax init` imported from one repository's files (its Claude Code auto
// memory and CLAUDE.local.md) to that repository, with scope.repos, so a
// Personal space's home files leave them out (personal-layer design §3.3;
// see ledger.BackfillRepoScope). The repository is the one whose project
// space the same init run imported into. Each change is one scoped
// receipt by Memax, via system, with a reason; no words change.
//
//	DATABASE_URL=… go run ./cmd/v2-backfill-repo-scope -dry-run -all
//	DATABASE_URL=… go run ./cmd/v2-backfill-repo-scope -user <uuid> [-user <uuid> …]
//	DATABASE_URL=… go run ./cmd/v2-backfill-repo-scope -all
//
// -dry-run prints what it would change and writes nothing. It is
// idempotent: run it again and it finds nothing new to change. It prints
// the report as JSON and exits 1 if any change failed.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

type userList []uuid.UUID

func (u *userList) String() string { return fmt.Sprint([]uuid.UUID(*u)) }

func (u *userList) Set(v string) error {
	id, err := uuid.Parse(strings.TrimSpace(v))
	if err != nil {
		return fmt.Errorf("%q is not a user id", v)
	}
	*u = append(*u, id)
	return nil
}

func main() {
	var users userList
	all := flag.Bool("all", false, "every Personal space on the V2 record")
	dryRun := flag.Bool("dry-run", false, "say what would change, and change nothing")
	flag.Var(&users, "user", "this person's Personal space (repeatable)")
	flag.Parse()
	if *all == (len(users) > 0) {
		fmt.Fprintln(os.Stderr, "usage: v2-backfill-repo-scope [-dry-run] -user <uuid> [-user <uuid> …] | -all")
		os.Exit(2)
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		slog.Error("DATABASE_URL is required")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		slog.Error("connect", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	report, err := ledger.New(pool).BackfillRepoScope(ctx, ledger.RepoScopeOptions{Users: users, DryRun: *dryRun})
	if err != nil {
		slog.Error("backfill failed", "error", err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(report)
	if report.Failed > 0 {
		os.Exit(1)
	}
}
