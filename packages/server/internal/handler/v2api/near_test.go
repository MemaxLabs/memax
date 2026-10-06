package v2api_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed/mockembed"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/v2index"
	"github.com/MemaxLabs/memax/packages/server/internal/v2recall"
)

const embedModel = "voyage-4"

// drafts is the handler's draft embedder, built once the env's ledger
// exists.
type drafts struct{ v *v2recall.Vectors }

func (d *drafts) EmbedDraft(ctx context.Context, s string) ([]float32, error) {
	return d.v.EmbedDraft(ctx, s)
}
func (d *drafts) Model() string               { return d.v.Model() }
func (d *drafts) NearDuplicateFloor() float64 { return d.v.NearDuplicateFloor() }

// newNearEnv is an env whose near-duplicate check embeds with e.
func newNearEnv(t *testing.T, e embed.Embedder) *env {
	t.Helper()
	d := &drafts{}
	env := newEnv(t, v2api.WithDrafts(d))
	d.v = v2recall.NewVectors(env.ledger, e, e, v2recall.VectorConfig{Model: embedModel})
	return env
}

// indexAll embeds every waiting version of the space, as the index jobs do.
func (e *env) indexAll(emb embed.Embedder, sp space) {
	e.t.Helper()
	ix := v2index.New(e.ledger, emb, embedModel, 64, quiet)
	for {
		n, err := ix.Index(context.Background(), ledger.IndexArgs{SpaceID: sp.id})
		if err != nil {
			e.t.Fatal(err)
		}
		if n == 0 {
			return
		}
	}
}

type nearResult struct {
	Items []struct {
		Memory     memory  `json:"memory"`
		Similarity float64 `json:"similarity"`
		Match      string  `json:"match"`
		Created    receipt `json:"created"`
	} `json:"items"`
	Semantic bool    `json:"semantic"`
	Floor    float64 `json:"floor"`
}

func (e *env) near(token string, sp space, body any) *resp {
	e.t.Helper()
	return e.do(call{method: "POST", path: "/v2/spaces/" + sp.slug + "/memories:near-duplicates", token: token,
		body: body, header: map[string]string{"Idempotency-Key": ""}})
}

// Remember's check: an exact repeat (same words), a near one (the same
// words in another order, which the content hash doesn't catch), an
// agent's pending proposal with who proposed it, nothing for an unrelated
// draft, and never another space's memory with the same words.
func TestNearDuplicates(t *testing.T) {
	t.Parallel()
	words := mockembed.NewWords()
	e := newNearEnv(t, words)
	zz := e.user("zz")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	tok := e.session(zz)
	kept := e.remember(tok, sp, "Pin shared dependency versions with the pnpm catalog.").Memory
	key, _ := e.apiKey(zz, keyOpts{agent: "codex"})
	prop := e.remember(key, sp, "Releases ship on Thursdays after the freeze.").Memory
	if prop.Lifecycle != "proposed" {
		t.Fatalf("the API key's memory is %s", prop.Lifecycle)
	}
	// Another tenant's space holds the very same words.
	mallory := e.user("mallory")
	theirs := e.space(mallory, policy.SpaceProject, "theirs")
	e.remember(e.session(mallory), theirs, "Pin shared dependency versions with the pnpm catalog.")
	e.indexAll(words, sp)
	e.indexAll(words, theirs)

	for _, c := range []struct {
		name, draft string
		ref, match  string
		lifecycle   string
		actor       string
	}{
		{"exact", "pin shared dependency versions with the pnpm catalog", kept.Ref, "exact", "kept", "person"},
		{"near", "With the pnpm catalog, pin shared dependency versions.", kept.Ref, "near", "kept", "person"},
		{"a proposal", "After the freeze, releases ship on Thursdays", prop.Ref, "near", "proposed", "agent"},
		{"unrelated", "The web app is a Next.js project.", "", "", "", ""},
	} {
		var got nearResult
		e.near(tok, sp, map[string]any{"statement": c.draft}).ok(http.StatusOK, &got)
		if !got.Semantic || got.Floor != v2recall.DefaultNearDuplicateFloor {
			t.Errorf("%s: semantic %v floor %v", c.name, got.Semantic, got.Floor)
		}
		if c.ref == "" {
			if len(got.Items) != 0 {
				t.Errorf("%s: %+v", c.name, got.Items)
			}
			continue
		}
		if len(got.Items) != 1 {
			t.Fatalf("%s: %d items, want 1 (never the other space's): %+v", c.name, len(got.Items), got.Items)
		}
		it := got.Items[0]
		if it.Memory.Ref != c.ref || it.Match != c.match || it.Memory.Lifecycle != c.lifecycle || it.Memory.SpaceID != sp.id ||
			it.Created.ActorKind != c.actor || it.Created.ObjectRef != c.ref || it.Similarity < 0.9 {
			t.Errorf("%s: %+v", c.name, it)
		}
		if c.actor == "agent" && it.Created.Agent != "codex" {
			t.Errorf("%s: created by agent %q", c.name, it.Created.Agent)
		}
	}
	// The other space's owner finds only their own.
	var got nearResult
	e.near(e.session(mallory), theirs, map[string]any{"statement": "Pin shared dependency versions with the pnpm catalog."}).ok(http.StatusOK, &got)
	if len(got.Items) != 1 || got.Items[0].Memory.SpaceID != theirs.id {
		t.Errorf("theirs: %+v", got.Items)
	}
	// An agent may check too: it only reads.
	e.near(key, sp, map[string]any{"statement": "Use the pnpm catalog", "limit": 1}).ok(http.StatusOK, nil)
	// A space outside the caller's is not found.
	e.near(e.session(mallory), sp, map[string]any{"statement": "x"}).fails(http.StatusNotFound, "not_found")
	// What the spec refuses.
	e.do(call{method: "POST", path: "/v2/spaces/" + sp.slug + "/memories:near-duplicates", token: tok,
		body: map[string]any{"statement": ""}, invalid: true}).fails(http.StatusBadRequest, "invalid_request")
	e.do(call{method: "POST", path: "/v2/spaces/" + sp.slug + "/memories:near-duplicates", token: tok,
		body: map[string]any{"statement": "x", "limit": 9}, invalid: true}).fails(http.StatusBadRequest, "invalid_request")
}

// Without an embedder, and with one that misses its deadline, the check
// still answers exact repeats, says it wasn't by meaning, and stays fast.
func TestNearDuplicatesWithoutMeaning(t *testing.T) {
	t.Parallel()
	slow := mockembed.New()
	slow.SetDelay(time.Second)
	for name, e := range map[string]*env{"no embedder": newEnv(t), "slow embedder": newNearEnv(t, slow)} {
		zz := e.user("zz")
		sp := e.space(zz, policy.SpaceProject, "memax-v2")
		tok := e.session(zz)
		kept := e.remember(tok, sp, "Pin shared dependency versions with the pnpm catalog.").Memory
		start := time.Now()
		var exact, near nearResult
		e.near(tok, sp, map[string]any{"statement": "pin shared dependency versions with the pnpm catalog"}).ok(http.StatusOK, &exact)
		if took := time.Since(start); took > 400*time.Millisecond {
			t.Errorf("%s: the check took %v", name, took)
		}
		if exact.Semantic || len(exact.Items) != 1 || exact.Items[0].Memory.Ref != kept.Ref || exact.Items[0].Match != "exact" {
			t.Errorf("%s: exact = %+v", name, exact)
		}
		e.near(tok, sp, map[string]any{"statement": "With the pnpm catalog, pin shared dependency versions."}).ok(http.StatusOK, &near)
		if near.Semantic || len(near.Items) != 0 {
			t.Errorf("%s: near = %+v", name, near)
		}
	}
}

// The check is rate-limited per caller: a burst passes, then 429
// rate_limited with Retry-After; another caller is unaffected.
func TestNearDuplicatesRateLimit(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz, ana := e.user("zz"), e.user("ana")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	e.join(sp, ana, "member")
	tok := e.session(zz)
	body := map[string]any{"statement": "Use the pnpm catalog."}
	limited := false
	for i := range 40 {
		r := e.near(tok, sp, body)
		if r.status == http.StatusTooManyRequests {
			r.fails(http.StatusTooManyRequests, "rate_limited")
			if r.header.Get("Retry-After") == "" {
				t.Error("no Retry-After")
			}
			if i < 30 {
				t.Errorf("limited after %d checks; the burst is 30", i)
			}
			limited = true
			break
		}
		r.ok(http.StatusOK, nil)
	}
	if !limited {
		t.Fatal("40 checks at once were never limited")
	}
	e.near(e.session(ana), sp, body).ok(http.StatusOK, nil)
}
