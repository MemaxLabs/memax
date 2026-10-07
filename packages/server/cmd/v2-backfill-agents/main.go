// Command v2-backfill-agents connects V1 API keys and OAuth grants to the
// V2 record as agent connections, at Propose (plan 25 §10; see
// ledger.BackfillConnections). It is idempotent: run it again and only
// credentials created since are connected.
//
//	DATABASE_URL=… go run ./cmd/v2-backfill-agents -user <uuid> [-user <uuid> …]
//	DATABASE_URL=… go run ./cmd/v2-backfill-agents -all
//
// Prefer -user for the people moving to V2: connections are V2 records,
// which block V1's space and account deletion until those go through the
// ledger.
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
	all := flag.Bool("all", false, "connect every user's credentials")
	flag.Var(&users, "user", "connect this user's credentials (repeatable)")
	flag.Parse()
	if *all == (len(users) > 0) {
		fmt.Fprintln(os.Stderr, "usage: v2-backfill-agents -user <uuid> [-user <uuid> …] | -all")
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

	report, err := ledger.New(pool).BackfillConnections(ctx, ledger.BackfillOptions{Users: users})
	if err != nil {
		slog.Error("backfill failed", "error", err)
		os.Exit(1)
	}
	_ = json.NewEncoder(os.Stdout).Encode(report)
	if report.Failed > 0 {
		os.Exit(1)
	}
}
