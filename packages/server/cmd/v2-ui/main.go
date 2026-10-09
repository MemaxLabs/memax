// Command v2-ui reads or changes one person's V2 UI flag (plan 25 E1,
// internal/v2ui), for the founders on staging, as the admin panel's V2 UI
// card does: on, off (wins over every rule), or back to the rules
// (default). Without -on, -off or -default it only says what the flag is
// and why.
//
//	DATABASE_URL=… go run ./cmd/v2-ui -email zz@example.com
//	DATABASE_URL=… go run ./cmd/v2-ui -email zz@example.com -on
//	DATABASE_URL=… go run ./cmd/v2-ui -user <uuid> -off
//	DATABASE_URL=… go run ./cmd/v2-ui -user <uuid> -default -json
//
// A change is audited (admin_audit, action v2_ui, via cmd) without an
// operator, and the person's browser picks it up at its next page load.
// The rule for new accounts reads V2_UI_SINCE from this process's
// environment: run it with the API's (doppler run …) to see what the API
// sees.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MemaxLabs/memax/packages/server/internal/v2ui"
)

const usage = "usage: v2-ui -email <email> | -user <uuid> [-on | -off | -default] [-json]"

func main() {
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
	if err := run(ctx, pool, os.Args[1:], os.Stdout, os.Getenv); err != nil {
		var u usageError
		if errors.As(err, &u) {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, "v2-ui:", err)
		os.Exit(1)
	}
}

type usageError string

func (e usageError) Error() string { return string(e) }

// Result is what the command prints with -json.
type Result struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	v2ui.Decision
	Changed bool    `json:"changed"`
	Since   *string `json:"since"`
}

// run parses args, finds the person, applies -on, -off or -default if
// given, and writes the flag to out.
func run(ctx context.Context, pool *pgxpool.Pool, args []string, out io.Writer, getenv func(string) string) error {
	fs := flag.NewFlagSet("v2-ui", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	email := fs.String("email", "", "the person's email")
	user := fs.String("user", "", "the person's id (uuid)")
	on := fs.Bool("on", false, "turn the V2 UI on for them")
	off := fs.Bool("off", false, "turn it off for them (wins over every rule)")
	def := fs.Bool("default", false, "let the rules decide again")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return usageError(fmt.Sprintf("v2-ui: %v\n%s", err, usage))
	}
	if fs.NArg() > 0 {
		return usageError(fmt.Sprintf("v2-ui: unexpected %q\n%s", fs.Arg(0), usage))
	}
	if (*email == "") == (*user == "") {
		return usageError("v2-ui: name the person with -email or -user, not both\n" + usage)
	}
	var setting v2ui.Setting
	for _, s := range []struct {
		set bool
		to  v2ui.Setting
	}{{*on, v2ui.On}, {*off, v2ui.Off}, {*def, v2ui.Default}} {
		if !s.set {
			continue
		}
		if setting != "" {
			return usageError("v2-ui: choose one of -on, -off and -default\n" + usage)
		}
		setting = s.to
	}

	since, err := v2ui.SinceFromEnv(getenv)
	if err != nil {
		return usageError(err.Error())
	}
	ui := v2ui.New(pool, since)
	var id uuid.UUID
	if *user != "" {
		if id, err = uuid.Parse(strings.TrimSpace(*user)); err != nil {
			return usageError(fmt.Sprintf("v2-ui: -user %q is not a uuid", *user))
		}
	} else if id, err = ui.PersonByEmail(ctx, *email); err != nil {
		if errors.Is(err, v2ui.ErrNoPerson) {
			return fmt.Errorf("no account has the email %q", *email)
		}
		return err
	}

	var d v2ui.Decision
	if setting == "" {
		d, err = ui.For(ctx, id)
	} else {
		d, err = ui.Set(ctx, id, setting, uuid.Nil, v2ui.ViaCmd)
	}
	if errors.Is(err, v2ui.ErrNoPerson) {
		return fmt.Errorf("no account has the id %s", id)
	}
	if err != nil {
		return err
	}
	var addr string
	if err := pool.QueryRow(ctx, `SELECT email FROM public.users WHERE id = $1`, id).Scan(&addr); err != nil {
		return fmt.Errorf("read the email: %w", err)
	}

	res := Result{UserID: id.String(), Email: addr, Decision: d, Changed: setting != ""}
	if !since.IsZero() {
		s := since.UTC().Format(time.RFC3339)
		res.Since = &s
	}
	if *asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	_, err = fmt.Fprintf(out, "%s (%s): %s, %s; operator setting %s\n", addr, id, uiWords(d.UI), reasonWords(d.Reason, res.Since), d.Setting)
	return err
}

func uiWords(ui v2ui.UI) string {
	if ui == v2ui.V2 {
		return "V2 UI on"
	}
	return "V2 UI off"
}

func reasonWords(r v2ui.Reason, since *string) string {
	switch r {
	case v2ui.ReasonOperatorOff:
		return "an operator turned it off"
	case v2ui.ReasonOperatorOn:
		return "an operator turned it on"
	case v2ui.ReasonV2Space:
		return "a member of a space on V2"
	case v2ui.ReasonSignedUp:
		return "signed up since " + *since
	}
	if since == nil {
		return "no space on V2 (V2_UI_SINCE unset)"
	}
	return "no space on V2, and signed up before " + *since
}
