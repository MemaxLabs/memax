// Command v2-switch-space switches a space to the V2 record, or back, as
// its owner (plan 25 §10, epic 2.8; ledger.StartSwitch). It runs the whole
// switch in the process: every V1 memory becomes a note, the owner's own
// short ones are offered for bulk keep through the import conflict check,
// agents' are left for Dream, personas and agent files become notes, the
// members' V1 keys and grants are connected at Propose (and told), and the
// space moves to V2 last. A switch that stopped resumes where it did.
//
//	DATABASE_URL=… go run ./cmd/v2-switch-space -space <uuid> [-space <uuid> …]
//	DATABASE_URL=… go run ./cmd/v2-switch-space -space <uuid> -preview
//	DATABASE_URL=… go run ./cmd/v2-switch-space -space <uuid> -off
//	DATABASE_URL=… go run ./cmd/v2-switch-space -space <uuid> -kind project -repository acme/web
//
// -preview prints what a switch would move and changes nothing. -off
// switches back: V1 behaves exactly as before, and the V2 record stays for
// a later switch. The import's conflict check and the judge run in the
// worker (judge_import, judge_proposal); without one they wait in River.
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
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/queue"
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
	off := flag.Bool("off", false, "switch the spaces back to V1 instead")
	preview := flag.Bool("preview", false, "print what switching would move, and change nothing")
	kind := flag.String("kind", "", "switch a V1 team hub as a project space (project)")
	repository := flag.String("repository", "", "the repository the space compiles for (owner/name)")
	flag.Parse()
	if len(spaces) == 0 {
		fmt.Fprintln(os.Stderr, "usage: v2-switch-space -space <uuid> [-space <uuid> …] [-preview | -off] [-kind project] [-repository owner/name]")
		os.Exit(2)
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		slog.Error("DATABASE_URL is required")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		slog.Error("connect", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	// The import and the judge's jobs go to River for the worker; the
	// switch itself runs here (no inserter: StartSwitch runs it inline).
	opts := []ledger.Option{}
	if jobs, err := queue.InsertClient(pool); err == nil {
		opts = append(opts, ledger.WithJobs(jobs))
	} else {
		slog.Warn("no River client: the judge's jobs won't be queued", "error", err)
	}
	l := ledger.New(pool, opts...)
	failed := false
	for _, id := range spaces {
		if err := switchOne(ctx, pool, l, id, *off, *preview, policy.SpaceKind(*kind), *repository); err != nil {
			slog.Error("switch failed", "space", id.String(), "error", err)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}

func switchOne(ctx context.Context, pool *pgxpool.Pool, l *ledger.Ledger, id uuid.UUID, off, preview bool, kind policy.SpaceKind, repo string) error {
	var owner uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT owner_id FROM public.hubs WHERE id = $1`, id).Scan(&owner); err != nil {
		return fmt.Errorf("read the space: %w", err)
	}
	scope, err := ledger.ResolveUserScope(ctx, pool, owner)
	if err != nil {
		return err
	}
	actor := ledger.Actor{Kind: policy.ActorPerson, ID: owner, Credential: policy.CredentialSession}
	key := "cmd-v2-switch-space:" + time.Now().UTC().Format(time.RFC3339Nano)
	var st *ledger.SpaceSwitch
	switch {
	case preview:
		st, err = l.SwitchStatus(ctx, scope, id)
	case off:
		st, err = l.SwitchBack(ctx, actor, policy.ViaSystem, scope, id, key)
	default:
		o := ledger.SwitchOptions{Kind: kind, Key: key}
		if repo != "" {
			o.Repository = &repo
		}
		st, err = l.StartSwitchInline(ctx, actor, policy.ViaSystem, scope, id, o)
	}
	if err != nil {
		return err
	}
	out := json.NewEncoder(os.Stdout)
	out.SetIndent("", "  ")
	return out.Encode(st)
}
