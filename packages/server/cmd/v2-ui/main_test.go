package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

// The command as the founders run it: read a person's flag by email or id,
// turn it on, off and back to the rules, each change audited as cmd, and
// refuse what isn't a person or a choice.
func TestV2UICommand(t *testing.T) {
	t.Parallel()
	_, pool := testdb.Acquire(t)
	ctx := context.Background()
	id := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, name, created_at) VALUES ($1, 'Founder@Memax.test', 'founder', '2026-10-01T00:00:00Z')`, id); err != nil {
		t.Fatal(err)
	}
	noEnv := env(nil)
	cmd := func(getenv func(string) string, args ...string) (string, error) {
		var out bytes.Buffer
		err := run(ctx, pool, args, &out, getenv)
		return out.String(), err
	}

	out, err := cmd(noEnv, "-email", "founder@memax.test")
	if err != nil {
		t.Fatal(err)
	}
	if want := "Founder@Memax.test (" + id.String() + "): V2 UI off, no space on V2 (V2_UI_SINCE unset); operator setting default\n"; out != want {
		t.Errorf("read:\n%s\nwant:\n%s", out, want)
	}

	out, err = cmd(noEnv, "-email", "founder@memax.test", "-on")
	if err != nil || !strings.Contains(out, "V2 UI on, an operator turned it on; operator setting on") {
		t.Errorf("on: %q %v", out, err)
	}
	out, err = cmd(noEnv, "-user", id.String(), "-off", "-json")
	if err != nil {
		t.Fatal(err)
	}
	var res Result
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("%s: %v", out, err)
	}
	if res.UserID != id.String() || res.UI != "v1" || res.Reason != "operator_off" || res.Setting != "off" || !res.Changed || res.Since != nil {
		t.Errorf("off = %+v", res)
	}
	// Back to the rules, with V2_UI_SINCE before the account: on by signup.
	out, err = cmd(env(map[string]string{"V2_UI_SINCE": "2026-09-01T00:00:00Z"}), "-user", id.String(), "-default")
	if err != nil || out != "Founder@Memax.test ("+id.String()+"): V2 UI on, signed up since 2026-09-01T00:00:00Z; operator setting default\n" {
		t.Errorf("default: %q %v", out, err)
	}
	var audited string
	if err := pool.QueryRow(ctx, `SELECT string_agg((metadata->>'setting') || ':' || (metadata->>'via') || ':' || (actor_id IS NULL)::text, ',' ORDER BY created_at)
		FROM admin_audit WHERE resource_id = $1::text AND action = 'v2_ui'`, id).Scan(&audited); err != nil {
		t.Fatal(err)
	}
	if audited != "on:cmd:true,off:cmd:true,default:cmd:true" {
		t.Errorf("audit = %s", audited)
	}

	// Usage mistakes exit 2; a person who isn't there exits 1.
	for _, args := range [][]string{
		{},
		{"-email", "a@b.test", "-user", id.String()},
		{"-user", id.String(), "-on", "-off"},
		{"-user", "nope"},
		{"-user", id.String(), "extra"},
		{"-bogus"},
	} {
		_, err := cmd(noEnv, args...)
		var u usageError
		if !errors.As(err, &u) {
			t.Errorf("%q: %v, want a usage error", args, err)
		}
	}
	if _, err := cmd(env(map[string]string{"V2_UI_SINCE": "soon"}), "-user", id.String()); err == nil {
		t.Error("a V2_UI_SINCE that isn't a time was accepted")
	}
	for _, args := range [][]string{{"-email", "nobody@memax.test"}, {"-user", uuid.NewString(), "-on"}} {
		_, err := cmd(noEnv, args...)
		var u usageError
		if err == nil || errors.As(err, &u) {
			t.Errorf("%q: %v, want not found", args, err)
		}
	}
}
