// Command v2-verify-receipts verifies spaces' receipt hash chains on
// demand (plan 25 §5.3), the way the worker's nightly receipts_verify job
// does: it recomputes each chain from genesis and checks every
// checkpoint's range, Merkle root, chain hash and signature, and every
// reason against its commitment. It writes nothing unless -record is set
// (then the outcome goes on the chain head, as the nightly job does), and
// exits 1 when anything doesn't match.
//
//	DATABASE_URL=… RECEIPT_VERIFY_KEYS=… go run ./cmd/v2-verify-receipts -space <uuid> [-space <uuid> …]
//	DATABASE_URL=… RECEIPT_VERIFY_KEYS=… go run ./cmd/v2-verify-receipts -all
//
// The keys are RECEIPT_SIGNING_KEY's public half and RECEIPT_VERIFY_KEYS
// (retired keys); without them a signed checkpoint reports an unknown key.
// -seal seals what is waiting first (as Memax, with RECEIPT_SIGNING_KEY).
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

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/receiptchain"
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
	flag.Var(&spaces, "space", "a space (hub) to verify (repeatable)")
	all := flag.Bool("all", false, "verify every space with receipts")
	seal := flag.Bool("seal", false, "seal what is waiting before verifying")
	record := flag.Bool("record", false, "record the outcome on each chain head, as the nightly job does")
	flag.Parse()
	if len(spaces) == 0 && !*all {
		fmt.Fprintln(os.Stderr, "usage: v2-verify-receipts -space <uuid> [-space <uuid> …] | -all [-seal] [-record]")
		os.Exit(2)
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		slog.Error("DATABASE_URL is required")
		os.Exit(1)
	}
	signer, keys, err := receiptchain.FromEnv(os.Getenv)
	if err != nil {
		slog.Error("receipt keys", "error", err)
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
	l := ledger.New(pool)
	if *all {
		if spaces, err = l.ReceiptSpaces(ctx); err != nil {
			slog.Error("list spaces", "error", err)
			os.Exit(1)
		}
	}

	var s receiptchain.Signer
	if signer != nil {
		s = signer
	}
	failed := false
	for _, id := range spaces {
		if *seal {
			for {
				res, err := l.SealSpace(ctx, id, s, 2000)
				if err != nil {
					slog.Error("seal", "space_id", id.String(), "error", err)
					failed = true
					break
				}
				if !res.More {
					break
				}
			}
		}
		v, err := l.VerifySpace(ctx, id, keys)
		if err != nil {
			slog.Error("verify", "space_id", id.String(), "error", err)
			failed = true
			continue
		}
		state := "verified"
		if !v.OK() {
			state, failed = "MISMATCH", true
		}
		fmt.Printf("%s %s: %d receipts in %d checkpoints (%d signed, %d unsigned), %d waiting to be sealed\n",
			id, state, v.Receipts, v.Checkpoints, v.Signed, v.Unsigned, v.Unsealed)
		for _, p := range v.Problems {
			fmt.Printf("  %s\n", p)
		}
		if *record {
			if err := l.RecordVerification(ctx, id, v); err != nil {
				slog.Error("record", "space_id", id.String(), "error", err)
				failed = true
			}
		}
	}
	if failed {
		os.Exit(1)
	}
}
