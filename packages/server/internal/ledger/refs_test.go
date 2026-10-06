package ledger_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

func TestFormatAndParseRef(t *testing.T) {
	t.Parallel()
	for n, want := range map[int64]string{1: "M-0001", 219: "M-0219", 9999: "M-9999", 10000: "M-10000", 123456: "M-123456"} {
		if got := ledger.FormatRef(ledger.PrefixMemory, n); got != want {
			t.Errorf("FormatRef(%d) = %s, want %s", n, got, want)
		}
	}
	cases := []struct {
		in   string
		p    ledger.Prefix
		n    int64
		okay bool
	}{
		{"M-0219", ledger.PrefixMemory, 219, true},
		{"m-219", ledger.PrefixMemory, 219, true},
		{" G-0007 ", ledger.PrefixDecision, 7, true},
		{"M-10000", ledger.PrefixMemory, 10000, true},
		{"M-0", "", 0, false},
		{"M-", "", 0, false},
		{"X-0001", "", 0, false},
		{"M-12a", "", 0, false},
		{"M--1", "", 0, false},
		{"0219", "", 0, false},
		{"M-9999999999999999999", "", 0, false},
	}
	for _, c := range cases {
		p, n, ok := ledger.ParseRef(c.in)
		if ok != c.okay || p != c.p || n != c.n {
			t.Errorf("ParseRef(%q) = %s %d %v, want %s %d %v", c.in, p, n, ok, c.p, c.n, c.okay)
		}
	}
}

// Display IDs are per tenant: a person's personal and project spaces
// share one sequence, a team space has its own, and other people's
// tenants are independent.
func TestDisplayIDsArePerTenant(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	jy := f.user("jy")
	zzPersonal := f.space(zz, policy.SpacePersonal, "Personal")
	zzProject := f.space(zz, policy.SpaceProject, "memax-v2")
	team := f.space(zz, policy.SpaceTeam, "MemaxLabs")
	jyPersonal := f.space(jy, policy.SpacePersonal, "Personal")

	refs := func(owner, space uuid.UUID, n int) []string {
		var out []string
		for i := 0; i < n; i++ {
			out = append(out, f.remember(owner, space, fmt.Sprintf("Memory %d in %s", i, space)).Ref)
		}
		return out
	}
	if got := refs(zz, zzPersonal, 2); !slices.Equal(got, []string{"M-0001", "M-0002"}) {
		t.Errorf("personal = %v", got)
	}
	if got := refs(zz, zzProject, 2); !slices.Equal(got, []string{"M-0003", "M-0004"}) {
		t.Errorf("project (same tenant as personal) = %v", got)
	}
	if got := refs(zz, team, 2); !slices.Equal(got, []string{"M-0001", "M-0002"}) {
		t.Errorf("team (its own tenant) = %v", got)
	}
	if got := refs(jy, jyPersonal, 1); !slices.Equal(got, []string{"M-0001"}) {
		t.Errorf("another person = %v", got)
	}

	// A refused command gives its number back (the counter is in the
	// transaction): the next memory takes it.
	res := f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyRead), f.scope(zz), policy.ViaMCP), NewMemory: fact(zzProject, "Refused.")})
	if res.Outcome != ledger.OutcomeRefused {
		t.Fatalf("read agent: %s", res.Outcome)
	}
	if got := refs(zz, zzProject, 1); got[0] != "M-0005" {
		t.Errorf("after a refusal = %v, want M-0005", got)
	}

	// The same display ID in two tenants of one scope is ambiguous; a
	// narrowed scope or the uuid resolves it.
	ctx := context.Background()
	scope := f.scope(zz)
	if _, err := f.l.GetMemory(ctx, scope, "M-0001"); !errors.Is(err, ledger.ErrAmbiguousRef) {
		t.Errorf("M-0001 across personal and team: %v, want ErrAmbiguousRef", err)
	}
	m, err := f.l.GetMemory(ctx, scope.Narrow(team), "M-0001")
	if err != nil || m.SpaceID != team {
		t.Errorf("narrowed to the team: %v %v", m, err)
	}
	if _, err := f.l.GetMemory(ctx, scope, "M-0004"); err != nil {
		t.Errorf("M-0004 exists only in zz's tenant: %v", err)
	}
}

// Concurrent writers in one tenant get distinct, gapless numbers.
func TestDisplayIDsUnderConcurrency(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	jy := f.user("jy")
	a := f.space(zz, policy.SpacePersonal, "Personal")
	b := f.space(zz, policy.SpaceProject, "memax-v2")
	c := f.space(jy, policy.SpacePersonal, "Personal")
	zzScope, jyScope := f.scope(zz), f.scope(jy)

	const per = 12
	type job struct {
		owner uuid.UUID
		space uuid.UUID
		scope ledger.Scope
	}
	var jobs []job
	for i := 0; i < per; i++ {
		jobs = append(jobs, job{zz, a, zzScope}, job{zz, b, zzScope}, job{jy, c, jyScope})
	}
	var mu sync.Mutex
	got := map[uuid.UUID][]int64{} // tenant → numbers
	var wg sync.WaitGroup
	errs := make(chan error, len(jobs))
	for i, j := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := f.l.Apply(context.Background(), &ledger.Remember{
				Meta: meta(person(j.owner), j.scope, policy.ViaWeb), NewMemory: fact(j.space, fmt.Sprintf("Concurrent %d", i)),
			})
			if err != nil {
				errs <- err
				return
			}
			_, n, _ := ledger.ParseRef(res.Memory.Ref)
			mu.Lock()
			got[res.Memory.TenantID] = append(got[res.Memory.TenantID], n)
			mu.Unlock()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent remember: %v", err)
	}
	want := map[uuid.UUID]int{zz: 2 * per, jy: per}
	for tenant, n := range want {
		nums := got[tenant]
		slices.Sort(nums)
		for i, x := range nums {
			if x != int64(i+1) {
				t.Fatalf("tenant %s numbers %v: want 1..%d with no repeats or gaps", tenant, nums, n)
			}
		}
		if len(nums) != n {
			t.Errorf("tenant %s got %d numbers, want %d", tenant, len(nums), n)
		}
	}
}
