// devseed seeds the memax-v2 demo space of the V2 handoff into a dev or
// test database, through the ledger: the screens' people, agents,
// memories (M-0219 …), Brief B-0043 and four default targets. It enqueues
// the targets' compile jobs, so a running worker (with COMPILE_SERVICE_URL
// and object storage) compiles them as C-0881 onwards.
//
//	cd packages/server && go run ./cmd/devseed
//
// It refuses to run when MEMAX_ENV is production, and running it again
// changes nothing.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/devseed"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/queue"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	env := os.Getenv("MEMAX_ENV")
	if env == "" {
		env = "dev"
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return fmt.Errorf("devseed: set DATABASE_URL to the dev database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("devseed: connect: %w", err)
	}
	defer pool.Close()
	var opts []ledger.Option
	if jobs, err := queue.InsertClient(pool); err == nil {
		opts = append(opts, ledger.WithJobs(jobs))
	} else {
		slog.Warn("devseed: no River client; targets compile on the worker's next sweep", "error", err)
	}
	d, err := devseed.SeedMemaxV2(ctx, pool, ledger.New(pool, opts...), devseed.Options{Env: env})
	if err != nil {
		return err
	}
	fmt.Printf("Seeded %s (%s): %d memories, Brief %s, %d targets.\n", devseed.SpaceSlug, d.Space, len(d.Memory), d.Brief.Ref, len(d.Targets))
	fmt.Printf("People: Ziyang Zeng (ziyang@example.com, owner) %s, Jiahao Ye (jiahao@example.com, member) %s.\n", d.ZZ, d.JY)
	fmt.Printf("Agents: Claude Code at Write %s, Codex at Propose %s.\n", d.CC.ID, d.CX.ID)
	return nil
}
