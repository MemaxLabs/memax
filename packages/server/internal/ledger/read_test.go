package ledger_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

func TestListSpaces(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz, jy := f.user("zz"), f.user("jy")
	personal := f.space(zz, policy.SpacePersonal, "Personal")
	project := f.space(zz, policy.SpaceProject, "memax-v2")
	team := f.space(jy, policy.SpaceTeam, "Acme")
	shared := f.space(jy, policy.SpaceTeam, "Board")
	f.join(team, zz, "contributor")
	f.join(shared, zz, "viewer")
	f.exec(`UPDATE hubs SET repository = 'MemaxLabs/memax' WHERE id = $1`, project)
	f.space(jy, policy.SpacePersonal, "JY personal") // not zz's

	got, err := f.l.ListSpaces(context.Background(), f.scope(zz))
	if err != nil {
		t.Fatalf("ListSpaces: %v", err)
	}
	type row struct {
		id   uuid.UUID
		role policy.Role
		kind policy.SpaceKind
	}
	var rows []row
	for _, sp := range got {
		rows = append(rows, row{sp.ID, sp.Role, sp.Kind})
		if sp.Slug == "" || sp.Name == "" || sp.TenantID == uuid.Nil {
			t.Errorf("space %+v is missing fields", sp)
		}
	}
	want := []row{
		{personal, policy.RoleOwner, policy.SpacePersonal}, // personal first, then by name
		{team, policy.RoleMember, policy.SpaceTeam},
		{shared, policy.RoleViewer, policy.SpaceTeam},
		{project, policy.RoleOwner, policy.SpaceProject},
	}
	if !slices.Equal(rows, want) {
		t.Errorf("spaces = %+v, want %+v", rows, want)
	}
	if len(got) == 4 && got[3].Repository != "MemaxLabs/memax" {
		t.Errorf("repository = %q", got[3].Repository)
	}
	if one, err := f.l.ListSpaces(context.Background(), f.scope(zz).Narrow(team)); err != nil || len(one) != 1 || one[0].ID != team {
		t.Errorf("narrowed = %+v, %v", one, err)
	}
}

func TestGetMemoryHistory(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	scope := f.scope(zz)
	ctx := context.Background()

	res := f.apply(&ledger.Remember{Meta: meta(person(zz), scope, policy.ViaWeb), NewMemory: ledger.NewMemory{
		SpaceID: space, Statement: "River is the queue.", Section: ledger.SectionDecisions,
		Sources: []ledger.SourceInput{{Kind: ledger.SourcePR, Ref: "PR #212"}},
	}})
	ref := res.Memory.Ref
	for i, words := range []string{"River is our queue.", "River is our only queue."} {
		f.apply(&ledger.Edit{Meta: meta(person(zz), scope, policy.ViaWeb), Memory: ref, ExpectedVersion: i + 1, Statement: words})
	}

	h, err := f.l.GetMemoryHistory(ctx, scope, ref)
	if err != nil {
		t.Fatalf("GetMemoryHistory: %v", err)
	}
	if h.Memory.Version != 3 || h.Memory.Statement != "River is our only queue." || len(h.Memory.Sources) != 1 {
		t.Errorf("memory = v%d %q, %d sources", h.Memory.Version, h.Memory.Statement, len(h.Memory.Sources))
	}
	var versions []int
	for _, v := range h.Versions {
		versions = append(versions, v.Version)
	}
	if !slices.Equal(versions, []int{3, 2, 1}) || h.Versions[2].Statement != "River is the queue." {
		t.Errorf("versions = %v (%+v)", versions, h.Versions)
	}
	var actions []ledger.Action
	for _, rc := range h.Receipts.Receipts {
		actions = append(actions, rc.Action)
	}
	if !slices.Equal(actions, []ledger.Action{ledger.ActionEdited, ledger.ActionEdited, ledger.ActionKept}) || h.Receipts.HasMore {
		t.Errorf("receipts = %v (has_more %v)", actions, h.Receipts.HasMore)
	}
	if h.Versions[0].ReceiptID != h.Receipts.Receipts[0].ID {
		t.Errorf("version 3 was written by %s, the newest receipt is %s", h.Versions[0].ReceiptID, h.Receipts.Receipts[0].ID)
	}
	if _, err := f.l.GetMemoryHistory(ctx, scope, "M-0999"); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("missing memory: %v", err)
	}
	other := f.user("jy")
	if _, err := f.l.GetMemoryHistory(ctx, f.scope(other), h.Memory.ID.String()); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("another user's memory by id: %v", err)
	}
}

func TestReviewQueue(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	zz := f.user("zz")
	space := f.space(zz, policy.SpaceProject, "memax-v2")
	other := f.space(zz, policy.SpaceProject, "elsewhere")
	scope := f.scope(zz)
	ctx := context.Background()
	propose := func(sp uuid.UUID, words string) *ledger.Memory {
		return f.apply(&ledger.Propose{Meta: meta(agentFor(policy.AutonomyPropose), scope, policy.ViaMCP),
			NewMemory: ledger.NewMemory{SpaceID: sp, Statement: words, Section: ledger.SectionConventions}}).Memory
	}

	stale := f.remember(zz, space, "Kept, then stale.")          // M-0001
	p1 := propose(space, "First proposal.")                      // M-0002
	f.remember(zz, space, "Kept and fine.")                      // M-0003
	conflict := f.remember(zz, space, "Kept, then in conflict.") // M-0004
	p2 := propose(space, "Second proposal.")                     // M-0005
	gone := propose(space, "Rejected proposal.")                 // M-0006
	propose(other, "In another space.")                          // M-0007
	f.apply(&ledger.Reject{Meta: meta(person(zz), scope, policy.ViaReview), Memory: gone.Ref})
	f.flag(stale, string(lifecycle.Stale))
	f.flag(conflict, string(lifecycle.Conflict))
	f.flag(p2, string(lifecycle.Conflict)) // a proposal in conflict is still a proposal

	want := []string{p1.Ref, p2.Ref, conflict.Ref, stale.Ref}
	page, err := f.l.ReviewQueue(ctx, scope, ledger.ReviewQuery{SpaceID: space})
	if err != nil {
		t.Fatalf("ReviewQueue: %v", err)
	}
	var got []string
	for _, m := range page.Memories {
		got = append(got, m.Ref)
	}
	if !slices.Equal(got, want) || page.Total != 4 || page.HasMore {
		t.Fatalf("queue = %v total %d has_more %v, want %v total 4", got, page.Total, page.HasMore, want)
	}
	if page.Memories[1].State != lifecycle.MarkConflict || page.Memories[3].State != lifecycle.MarkStale {
		t.Errorf("states = %s, %s", page.Memories[1].State, page.Memories[3].State)
	}

	got = nil
	cursor := ""
	for range 10 {
		p, err := f.l.ReviewQueue(ctx, scope, ledger.ReviewQuery{SpaceID: space, Limit: 1, Cursor: cursor})
		if err != nil {
			t.Fatalf("page: %v", err)
		}
		if p.Total != 4 {
			t.Errorf("total on a page = %d, want 4", p.Total)
		}
		for _, m := range p.Memories {
			got = append(got, m.Ref)
		}
		if !p.HasMore {
			break
		}
		cursor = p.NextCursor
	}
	if !slices.Equal(got, want) {
		t.Errorf("paged queue = %v, want %v", got, want)
	}

	// "not-a-cursor", base64url-encoded.
	if _, err := f.l.ReviewQueue(ctx, scope, ledger.ReviewQuery{SpaceID: space, Cursor: "bm90LWEtY3Vyc29y"}); !errors.Is(err, ledger.ErrInvalid) {
		t.Errorf("bad cursor: %v", err)
	}
	memCursor, err := f.l.ListMemories(ctx, scope, ledger.MemoryQuery{SpaceID: space, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.l.ReviewQueue(ctx, scope, ledger.ReviewQuery{SpaceID: space, Cursor: memCursor.NextCursor}); !errors.Is(err, ledger.ErrInvalid) {
		t.Errorf("a memories cursor on Review: %v", err)
	}
	if _, err := f.l.ReviewQueue(ctx, f.scope(f.user("jy")), ledger.ReviewQuery{SpaceID: space}); !errors.Is(err, ledger.ErrNotFound) {
		t.Errorf("space outside the scope: %v", err)
	}
}
