package v2api_test

// Rule 7 end to end (plan 25 §5.13): after a Forget, the words are in no
// table, column, object or key Memax holds, within the minute, and every
// agent that read them is told. It runs the real stack: Postgres, River
// with the compile, judge, index and forget workers, the real compile
// service under Node, object storage (in memory) and Redis (miniredis), with
// /v2 behind the real auth middleware. The grep doesn't trust a list of
// columns: it reads the catalog for every text, character, json, jsonb,
// tsvector, text array and bytea column in every schema, and proves it can
// see the words before the Forget (so row-level security isn't hiding
// them). V1's deletes of a space and of a person's data go through the
// same ledger, and the chains verify after each.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/MemaxLabs/memax/packages/server/internal/compile"
	"github.com/MemaxLabs/memax/packages/server/internal/compile/compiletest"
	"github.com/MemaxLabs/memax/packages/server/internal/forget"
	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed/mockembed"
	"github.com/MemaxLabs/memax/packages/server/internal/judge"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/objectstore/mockobjectstore"
	"github.com/MemaxLabs/memax/packages/server/internal/testdb"
	"github.com/MemaxLabs/memax/packages/server/internal/v2index"
)

// forgetStack is the real stack, plus V1's deletes and an API process
// listening for the cache purge on Redis.
type forgetStack struct {
	*env
	redis  *miniredis.Miniredis
	purged chan uuid.UUID
	v1     http.Handler
}

func newForgetStack(t *testing.T) *forgetStack {
	t.Helper()
	serviceURL := compiletest.StartService(t)
	st, pool := testdb.Acquire(t)
	dbTests.Add(1)
	authH, err := handler.NewAuthHandler(pool)
	if err != nil {
		t.Fatal(err)
	}
	authH.SetStore(st)
	chain := func(h http.Handler) http.Handler {
		return handler.RequireAuth([]byte(testSecret), authH.ResolveAPIKey, authH.ResolveOAuthGrant)(
			handler.HubContext(st)(handler.AuthorizeHTTP(h)))
	}
	jobs := &lateJobs{}
	e := &env{t: t, pool: pool, store: mockobjectstore.New()}
	e.ledger = ledger.New(pool, ledger.WithLogger(quiet), ledger.WithJobs(jobs), ledger.WithIndexJobs())
	e.svc = compile.New(e.ledger, compiletest.Client(serviceURL), e.store,
		compile.Config{AppBaseURL: "https://memax.app", Log: quiet})

	// Redis: the worker signals; an API process listens.
	mr := miniredis.RunT(t)
	workerRedis := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	apiRedis := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = workerRedis.Close(); _ = apiRedis.Close() })
	s := &forgetStack{env: e, redis: mr, purged: make(chan uuid.UUID, 16)}
	apiBus := forget.NewBus(apiRedis, quiet)
	apiBus.Local.Register(func(id uuid.UUID) {
		select {
		case s.purged <- id:
		default:
		}
	})
	listenCtx, stopListening := context.WithCancel(context.Background())
	t.Cleanup(stopListening)
	apiBus.Listen(listenCtx)

	workers := river.NewWorkers()
	compile.AddWorkers(workers, e.ledger, e.svc)
	judge.AddWorkers(workers, judge.New(e.ledger, nil, judge.Config{Log: quiet}))
	v2index.AddWorkers(workers, v2index.New(e.ledger, mockembed.New(), "voyage-4", 64, quiet), e.ledger)
	forget.AddWorkers(workers, forget.New(e.ledger, e.svc, forget.NewBus(workerRedis, quiet), quiet))
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Logger: quiet, Workers: workers,
		Queues: map[string]river.QueueConfig{
			ledger.QueueCompile: {MaxWorkers: compile.MaxWorkers}, ledger.QueueJudge: {MaxWorkers: judge.MaxWorkers},
			ledger.QueueIndex: {MaxWorkers: v2index.MaxWorkers}, ledger.QueueForget: {MaxWorkers: forget.MaxWorkers}},
	})
	if err != nil {
		t.Fatal(err)
	}
	jobs.client = client
	ctx, cancel := context.WithCancel(context.Background())
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stop, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		_ = client.Stop(stop)
		cancel()
	})
	mux := http.NewServeMux()
	// Errors reach the test's output: a 500 here once failed only on CI,
	// and the quiet logger kept its cause.
	h := v2api.New(e.ledger, slog.New(slog.NewTextHandler(testWriter{t}, &slog.HandlerOptions{Level: slog.LevelError})), v2api.WithCompile(e.svc))
	t.Cleanup(h.Wait)
	h.Mount(mux, chain)
	e.h, e.srv = h, spec.Handler(t, mux)

	// V1's deletes, wired to the ledger as serverapp wires them.
	hubs := handler.NewHubsHandler(st)
	hubs.SetV2Forgetter(e.ledger)
	mems := handler.NewMemoriesHandler(st, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	mems.SetV2Forgetter(e.ledger)
	v1 := http.NewServeMux()
	v1.Handle("DELETE /v1/hubs/{id}", chain(http.HandlerFunc(hubs.Delete)))
	v1.Handle("DELETE /v1/account/data", chain(http.HandlerFunc(mems.DeleteAllData)))
	s.v1 = v1
	return s
}

// v1Do sends a V1 request and answers its status and body.
func (s *forgetStack) v1Do(method, path, token string) (int, string) {
	s.t.Helper()
	r := httptest.NewRequest(method, path, nil)
	r.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.v1.ServeHTTP(rec, r)
	return rec.Code, rec.Body.String()
}

// word is a token found nowhere else: the words a test forgets carry it.
func word(prefix string) string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// textColumns lists every column in the database that can hold words.
func (s *forgetStack) textColumns() [][3]string {
	s.t.Helper()
	rows, err := s.pool.Query(context.Background(), `
		SELECT c.table_schema, c.table_name, c.column_name
		  FROM information_schema.columns c
		  JOIN information_schema.tables t ON t.table_schema = c.table_schema AND t.table_name = c.table_name
		 WHERE c.table_schema NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		   AND t.table_type = 'BASE TABLE'
		   AND (c.data_type IN ('text', 'character varying', 'character', 'json', 'jsonb', 'tsvector', 'bytea', 'xml')
		        OR (c.data_type = 'ARRAY' AND c.udt_name IN ('_text', '_varchar', '_bpchar', '_jsonb', '_json')))
		 ORDER BY 1, 2, 3`)
	if err != nil {
		s.t.Fatal(err)
	}
	cols, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) ([3]string, error) {
		var c [3]string
		err := r.Scan(&c[0], &c[1], &c[2])
		return c, err
	})
	if err != nil {
		s.t.Fatal(err)
	}
	if len(cols) < 100 {
		s.t.Fatalf("the catalog lists only %d text columns", len(cols))
	}
	return cols
}

// grepDB answers every column holding token (case-insensitive; bytea as
// its bytes), as the login role.
func (s *forgetStack) grepDB(token string) []string {
	s.t.Helper()
	ctx := context.Background()
	var hits []string
	for _, c := range s.textColumns() {
		col := pgx.Identifier{c[2]}.Sanitize()
		tbl := pgx.Identifier{c[0], c[1]}.Sanitize()
		var cond string
		var dataType string
		if err := s.pool.QueryRow(ctx, `SELECT data_type FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2 AND column_name = $3`,
			c[0], c[1], c[2]).Scan(&dataType); err != nil {
			s.t.Fatal(err)
		}
		if dataType == "bytea" {
			cond = fmt.Sprintf("position(convert_to(lower($1), 'UTF8') in lower(encode(%s, 'escape'))::bytea) > 0", col)
		} else {
			cond = fmt.Sprintf("strpos(lower(%s::text), lower($1)) > 0", col)
		}
		var n int
		if err := s.pool.QueryRow(ctx, fmt.Sprintf("SELECT count(*) FROM %s WHERE %s", tbl, cond), token).Scan(&n); err != nil {
			s.t.Fatalf("grep %s.%s.%s: %v", c[0], c[1], c[2], err)
		}
		if n > 0 {
			hits = append(hits, fmt.Sprintf("%s.%s.%s (%d)", c[0], c[1], c[2], n))
		}
	}
	return hits
}

// grepObjects answers every stored object whose key or content holds token.
func (s *forgetStack) grepObjects(token string) []string {
	s.t.Helper()
	var hits []string
	for _, k := range s.store.Keys() {
		r, err := s.store.Get(context.Background(), k)
		if err != nil {
			s.t.Fatal(err)
		}
		raw, _ := io.ReadAll(r.Body)
		if strings.Contains(strings.ToLower(k+"\n"+string(raw)), strings.ToLower(token)) {
			hits = append(hits, k)
		}
	}
	return hits
}

// grepRedis answers every Redis key whose name or value holds token.
func (s *forgetStack) grepRedis(token string) []string {
	var hits []string
	for _, k := range s.redis.Keys() {
		var dump string
		switch s.redis.Type(k) {
		case "string":
			dump, _ = s.redis.Get(k)
		case "hash":
			fields, _ := s.redis.HKeys(k)
			for _, f := range fields {
				dump += f + "=" + s.redis.HGet(k, f) + "\n"
			}
		case "list":
			l, _ := s.redis.List(k)
			dump = strings.Join(l, "\n")
		case "set":
			m, _ := s.redis.Members(k)
			dump = strings.Join(m, "\n")
		case "zset":
			m, _ := s.redis.ZMembers(k)
			dump = strings.Join(m, "\n")
		default:
			dump = s.redis.Dump()
		}
		if strings.Contains(strings.ToLower(k+"\n"+dump), strings.ToLower(token)) {
			hits = append(hits, k)
		}
	}
	return hits
}

// awaitTargets waits until every target of the space compiled its latest
// generation.
func (s *forgetStack) awaitTargets(tok, slug string, within time.Duration) []gateTarget {
	s.t.Helper()
	deadline := time.Now().Add(within)
	for {
		var list page[gateTarget]
		s.do(call{method: "GET", path: "/v2/spaces/" + slug + "/targets", token: tok}).ok(http.StatusOK, &list)
		done := true
		for _, tg := range list.Items {
			if tg.SyncState != "off" && (tg.CompiledGen < tg.DirtyGen || tg.LastCompile == nil) {
				done = false
			}
		}
		if done {
			return list.Items
		}
		if time.Now().After(deadline) {
			s.t.Fatalf("the targets didn't compile within %v: %+v", within, list.Items)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// awaitEmbedded waits until every listed memory's current version has an
// embedding.
func (s *forgetStack) awaitEmbedded(ids ...uuid.UUID) {
	s.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for s.count(`SELECT count(DISTINCT memory_id) FROM v2.memory_embeddings WHERE memory_id = ANY($1)`, ids) < len(ids) {
		if time.Now().After(deadline) {
			s.t.Fatalf("the memories weren't embedded")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// sealAndVerify seals a space's receipts and verifies its chain, as
// cmd/v2-verify-receipts -seal does.
func (s *forgetStack) sealAndVerify(space uuid.UUID) {
	s.t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := s.ledger.SealSpace(ctx, space, nil, 1000); err != nil {
			s.t.Fatalf("seal: %v", err)
		}
		v, err := s.ledger.VerifySpace(ctx, space, nil)
		if err != nil {
			s.t.Fatalf("verify: %v", err)
		}
		if v.Unsealed == 0 || time.Now().After(deadline) {
			if !v.OK() || v.Unsealed != 0 || v.Receipts == 0 {
				s.t.Errorf("space %s: the chain doesn't verify (%d receipts, %d unsealed): %v", space, v.Receipts, v.Unsealed, v.Problems)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

type forgetCarries struct {
	Error struct {
		Code    string `json:"code"`
		Details struct {
			Carries []struct {
				Ref    string `json:"ref"`
				Reason string `json:"reason"`
			} `json:"carries"`
		} `json:"details"`
	} `json:"error"`
}

// seedWords fills a space with what a Forget of m must reach, through the
// API and the workers: m (a source quote, a URI and a reason carrying the
// token), an agent's repeat of it the judge folds, a memory citing it,
// Brief prose citing it, AGENTS.md and the ChatGPT copy-out compiled with
// it, a delivery and a hand edit's stored copy, an agent's load of the
// compile, and the embeddings. It answers m and the agent's key.
func (s *forgetStack) seedWords(owner uuid.UUID, sp space, token string) (memory, string) {
	s.t.Helper()
	ctx := context.Background()
	tok := s.session(owner)
	base := "/v2/spaces/" + sp.id.String()
	statement := "The staging vault answers to " + token + " on Thursdays."
	var m result
	s.do(call{method: "POST", path: base + "/memories", token: tok, body: map[string]any{
		"statement": statement, "section": "conventions", "reason": "said by " + token + " in standup",
		"sources": []map[string]any{{"kind": "session", "ref": "Session 3e1a", "uri": "https://example.test/" + token,
			"quote": token + " runs the vault"}},
	}}).ok(http.StatusCreated, &m)
	key, _ := s.apiKey(owner, keyOpts{agent: "codex"})
	s.connectAll(owner)
	var repeat result
	s.do(call{method: "POST", path: base + "/memories", token: key, body: map[string]any{
		"statement": statement, "section": "conventions", "reason": "heard " + token + " again"}}).ok(http.StatusCreated, &repeat)
	var cited result
	s.do(call{method: "POST", path: base + "/memories", token: tok, body: map[string]any{
		"statement": "Vault questions go to the platform channel.", "section": "conventions",
		"sources": []map[string]any{{"kind": "memory", "ref": m.Memory.Ref}}}}).ok(http.StatusCreated, &cited)
	other := s.remember(tok, sp, "Use pnpm workspaces only.")
	s.do(call{method: "POST", path: base + "/brief", token: tok, body: map[string]any{
		"title": "Memax V2", "sections": []map[string]any{{"key": "conventions", "heading": "Conventions", "items": []map[string]any{
			{"ref": m.Memory.Ref}, {"text": "Ask " + token + " before touching the vault.", "cites": []string{m.Memory.Ref}},
			{"ref": other.Memory.Ref}}}},
	}}).ok(http.StatusCreated, nil)
	var agents, chatgpt targetResult
	s.do(call{method: "POST", path: base + "/targets", token: tok, body: map[string]any{"kind": "agents_md"}}).ok(http.StatusCreated, &agents)
	s.do(call{method: "POST", path: base + "/targets", token: tok, body: map[string]any{"kind": "chatgpt"}}).ok(http.StatusCreated, &chatgpt)
	s.awaitTargets(tok, sp.slug, 30*time.Second)

	// The device delivers AGENTS.md, then a hand edit adds a line.
	var pv gatePreview
	s.do(call{method: "GET", path: targetPath(agents.Target.ID, "/preview"), token: tok}).ok(http.StatusOK, &pv)
	if len(pv.Files) != 1 || !strings.Contains(pv.Files[0].Content, token) {
		s.t.Fatalf("AGENTS.md doesn't hold the words: %+v", pv.Files)
	}
	s.do(call{method: "POST", path: targetPath(agents.Target.ID, "/deliveries"), token: tok, header: map[string]string{"X-Memax-Via": "cli"},
		body: map[string]any{"compile": pv.Compile.Ref, "sha256": ledger.ManifestSHA256(map[string]string{pv.Files[0].Path: pv.Files[0].DriftSHA256})}}).
		ok(http.StatusOK, nil)
	scope, err := s.ledger.UserScope(ctx, owner)
	if err != nil {
		s.t.Fatal(err)
	}
	if _, err := s.svc.Observe(ctx, ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorPerson, ID: owner, Credential: policy.CredentialSession},
		Scope: scope, Via: policy.ViaCLI, IdempotencyKey: uuid.NewString()}, agents.Target.ID, compile.ObserveInput{
		Path: "AGENTS.md", Content: pv.Files[0].Content + "- Prefer named exports.\n", ObserverKind: ledger.ObserverDevice, ObserverID: "zz-laptop"}); err != nil {
		s.t.Fatal(err)
	}
	// The agent's session loads the compile: it read the memory.
	s.do(call{method: "POST", path: base + "/compile-loads", token: key, body: map[string]any{"compile": pv.Compile.Ref}}).ok(http.StatusCreated, nil)

	// The judge folded the agent's repeat into m.
	deadline := time.Now().Add(30 * time.Second)
	for s.count(`SELECT count(*) FROM v2.memory_links WHERE from_memory_id = $1 AND to_memory_id = $2 AND kind = 'merged_into'`, repeat.Memory.ID, m.Memory.ID) == 0 {
		if time.Now().After(deadline) {
			s.t.Fatal("the judge didn't fold the repeat")
		}
		time.Sleep(50 * time.Millisecond)
	}
	s.awaitEmbedded(m.Memory.ID, cited.Memory.ID, other.Memory.ID)

	// The judge compared an agent's decision with m on its way to flagging
	// a conflict, and wrote its question and labels with m's words in them.
	var kept, proposed result
	s.do(call{method: "POST", path: base + "/memories", token: tok,
		body: decisionBody("Secrets live in the platform vault.", "secrets")}).ok(http.StatusCreated, &kept)
	s.do(call{method: "POST", path: base + "/memories", token: key,
		body: decisionBody("Secrets live in each service's own vault.", "secrets")}).ok(http.StatusCreated, &proposed)
	sscope, err := s.ledger.SpaceScope(ctx, sp.id)
	if err != nil {
		s.t.Fatal(err)
	}
	if _, err := s.ledger.Apply(ctx, &ledger.RecordVerdict{
		Meta:   ledger.Meta{Actor: ledger.Actor{Kind: policy.ActorMemax}, Scope: sscope, Via: policy.ViaSystem, IdempotencyKey: uuid.NewString()},
		Memory: proposed.Memory.ID, Version: 1, Mode: ledger.JudgeProposal, Round: 1, Force: true,
		Outcome: ledger.OutcomeFlagged, Target: kept.Memory.ID,
		Verdict: ledger.Verdict{Stage: ledger.StageLLM, Relation: ledger.RelationContradicts, Related: kept.Memory.ID,
			Question: "Does " + token + " keep the vault?", Suggested: ledger.SuggestDecision,
			Labels: ledger.ConflictLabels{Proposal: "Each service", Decision: token + "'s vault", Both: "Both", Open: "Undecided"},
			Candidates: []ledger.VerdictCandidate{{MemoryID: kept.Memory.ID, Ref: kept.Memory.Ref, Sets: []string{"keyed"}},
				{MemoryID: m.Memory.ID, Ref: m.Memory.Ref, Sets: []string{"lexical"}}}},
	}); err != nil {
		s.t.Fatal(err)
	}
	return m.Memory, key
}

func TestForgetLeavesTheWordsNowhere(t *testing.T) {
	t.Parallel()
	s := newForgetStack(t)
	zz := s.user("zz")
	sp := s.space(zz, policy.SpaceProject, "memax-v2")
	tok := s.session(zz)
	token := word("zq")
	m, key := s.seedWords(zz, sp, token)

	// The grep sees the words where they are: it isn't blind.
	before := s.grepDB(token)
	for _, want := range []string{"v2.memory_versions.statement", "v2.sources.quote", "v2.sources.uri", "v2.receipts.reason",
		"v2.memories.search", "v2.brief_versions.structure", "v2.judge_verdicts.question", "v2.judge_verdicts.labels"} {
		if !slices.ContainsFunc(before, func(h string) bool { return strings.HasPrefix(h, want+" ") }) {
			t.Errorf("before the Forget, the grep doesn't see the words in %s: %v", want, before)
		}
	}
	if len(s.grepObjects(token)) == 0 {
		t.Fatal("before the Forget, object storage doesn't hold the words")
	}
	t.Logf("before the Forget the words are in %d columns: %v", len(before), before)

	// The preview: what goes with it, the two targets that hold it, and the
	// one agent that read it.
	var pv forgetPreview
	s.do(call{method: "GET", path: "/v2/memories/" + m.Ref + "/forget-preview?space=" + sp.slug, token: tok}).ok(http.StatusOK, &pv)
	if !pv.Allowed || len(pv.Carries) != 2 || len(pv.Files) != 2 || pv.Agents != 1 || pv.Readers != 1 {
		t.Errorf("preview = %+v", pv)
	}

	// Forget: first it says what goes with it, then it goes.
	path := "/v2/memories/" + m.Ref + ":forget?space=" + sp.slug
	ifm := map[string]string{"If-Match": `"1"`}
	raw := s.do(call{method: "POST", path: path, token: tok, header: ifm})
	var conflict forgetCarries
	if raw.status != http.StatusConflict {
		t.Fatalf("forget: %d %s", raw.status, raw.body)
	}
	if err := json.Unmarshal(raw.body, &conflict); err != nil || conflict.Error.Code != "forget_carries" || len(conflict.Error.Details.Carries) != 2 {
		t.Fatalf("forget_carries: %s", raw.body)
	}
	var carries []string
	for _, c := range conflict.Error.Details.Carries {
		carries = append(carries, c.Ref)
	}
	var res forgetResult
	s.do(call{method: "POST", path: path, token: tok, header: ifm, body: map[string]any{"carries": carries}}).ok(http.StatusOK, &res)
	forgotAt := time.Now()

	// Propagation: within the minute.
	var ts tombstone
	for {
		s.do(call{method: "GET", path: "/v2/memories/" + m.Ref + "/tombstone?space=" + sp.slug, token: tok}).ok(http.StatusOK, &ts)
		if ts.Status == ledger.TombstoneDone {
			break
		}
		if time.Since(forgotAt) > time.Minute {
			t.Fatalf("propagation didn't finish within the minute: %+v", ts)
		}
		time.Sleep(25 * time.Millisecond)
	}
	took := time.Since(forgotAt)
	t.Logf("propagation (real compile service, River): done %v after the Forget's response", took.Round(time.Millisecond))

	if hits := s.grepDB(token); len(hits) != 0 {
		t.Errorf("after the Forget, the words are still in: %v", hits)
	}
	if hits := s.grepObjects(token); len(hits) != 0 {
		t.Errorf("after the Forget, object storage still holds the words in: %v", hits)
	}
	if hits := s.grepRedis(token); len(hits) != 0 {
		t.Errorf("after the Forget, Redis still holds the words in: %v", hits)
	}
	if n := s.count(`SELECT count(*) FROM v2.memory_embeddings WHERE memory_id = ANY($1)`, []uuid.UUID{m.ID, res.Memories[0].ID}); n != 0 {
		t.Errorf("%d embeddings left", n)
	}
	select {
	case id := <-s.purged:
		if id != sp.id {
			t.Errorf("the API process purged %s", id)
		}
	case <-time.After(5 * time.Second):
		t.Error("the API process wasn't told to purge its caches")
	}
	// The other memory is untouched, and AGENTS.md compiles without m.
	if s.count(`SELECT count(*) FROM v2.memory_versions WHERE statement = 'Use pnpm workspaces only.'`) != 1 {
		t.Error("the Forget took another memory's words")
	}
	// The agent that loaded it is told, once.
	var notices struct {
		Notices []struct {
			ID   uuid.UUID `json:"id"`
			Kind string    `json:"kind"`
			Refs []string  `json:"refs"`
		} `json:"notices"`
	}
	s.do(call{method: "GET", path: "/v2/notices", token: key}).ok(http.StatusOK, &notices)
	if len(notices.Notices) != 1 || notices.Notices[0].Kind != "forgotten" || !slices.Contains(notices.Notices[0].Refs, m.Ref) {
		t.Errorf("notices = %+v", notices)
	}
	agentStep := false
	for _, st := range ts.Steps {
		agentStep = agentStep || st.Kind == "agent"
	}
	if !agentStep {
		t.Errorf("the tombstone doesn't list the agent: %+v", ts.Steps)
	}
	s.sealAndVerify(sp.id)
}

// V1's DELETE /v1/hubs/{id} forgets and retires the space's V2 record
// first: the words are nowhere, the hub is gone, and the chain verifies.
func TestV1SpaceDeleteForgetsTheRecord(t *testing.T) {
	t.Parallel()
	s := newForgetStack(t)
	zz := s.user("zz")
	sp := s.space(zz, policy.SpaceProject, "memax-v2")
	keep := s.space(zz, policy.SpaceProject, "elsewhere")
	token := word("zh")
	s.seedWords(zz, sp, token)
	s.remember(s.session(zz), keep, "Other spaces keep their record.")

	// An API key, even one V1 lets manage hubs, can't delete a space with
	// a V2 record.
	key, _ := s.apiKey(zz, keyOpts{agent: "codex", perms: []string{"memory:read", "memory:write", "hub:read", "hub:manage"}})
	if code, body := s.v1Do("DELETE", "/v1/hubs/"+sp.id.String(), key); code != http.StatusForbidden || !strings.Contains(body, "forget_needs_person") {
		t.Errorf("an API key's delete: %d %s", code, body)
	}
	if code, body := s.v1Do("DELETE", "/v1/hubs/"+sp.id.String(), s.session(zz)); code != http.StatusOK {
		t.Fatalf("delete: %d %s", code, body)
	}
	if s.count(`SELECT count(*) FROM hubs WHERE id = $1`, sp.id) != 0 {
		t.Error("the hub is still there")
	}
	// The space's propagation runs, then nothing holds the words.
	deadline := time.Now().Add(time.Minute)
	for s.count(`SELECT count(*) FROM v2.tombstones WHERE space_id = $1 AND object_kind = 'space' AND status = 'done'`, sp.id) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the space's propagation didn't finish within the minute")
		}
		time.Sleep(25 * time.Millisecond)
	}
	if hits := s.grepDB(token); len(hits) != 0 {
		t.Errorf("the words are still in: %v", hits)
	}
	if hits := s.grepObjects(token); len(hits) != 0 {
		t.Errorf("object storage still holds them in: %v", hits)
	}
	for _, table := range []string{"memories", "memory_versions", "targets", "compile_runs", "brief_versions", "agent_connection_spaces"} {
		if n := s.count(fmt.Sprintf(`SELECT count(*) FROM v2.%s WHERE space_id = $1`, table), sp.id); n != 0 {
			t.Errorf("v2.%s keeps %d rows of the deleted space", table, n)
		}
	}
	if s.count(`SELECT count(*) FROM v2.receipts WHERE space_id = $1 AND reason IS NOT NULL`, sp.id) != 0 {
		t.Error("a receipt of the deleted space keeps its reason")
	}
	s.sealAndVerify(sp.id)
	s.sealAndVerify(keep.id)
	if s.count(`SELECT count(*) FROM v2.memories WHERE space_id = $1 AND lifecycle = 'kept'`, keep.id) != 1 {
		t.Error("the other space lost its record")
	}
}

// V1's DELETE /v1/account/data forgets the record of every space the
// person owns (they stay, empty), keeps the team's, and keeps the account's
// agents; an API key is refused while there is a record.
func TestV1AccountDataDeleteForgetsTheRecord(t *testing.T) {
	t.Parallel()
	s := newForgetStack(t)
	zz, jy := s.user("zz"), s.user("jy")
	personal := s.space(zz, policy.SpacePersonal, "Personal")
	project := s.space(zz, policy.SpaceProject, "memax-v2")
	team := s.space(zz, policy.SpaceTeam, "memax-team")
	s.join(team, jy, "contributor")
	token := word("za")
	s.seedWords(zz, project, token)
	s.remember(s.session(zz), personal, "I keep "+token+" notes in a paper notebook.")
	s.remember(s.session(zz), team, "The team ships on Fridays.")

	key, _ := s.apiKey(zz, keyOpts{agent: "codex", perms: []string{"memory:read", "memory:write", "hub:read", "account:delete"}})
	if code, body := s.v1Do("DELETE", "/v1/account/data", key); code != http.StatusForbidden || !strings.Contains(body, "forget_needs_person") {
		t.Errorf("an API key's delete: %d %s", code, body)
	}
	if code, body := s.v1Do("DELETE", "/v1/account/data", s.session(zz)); code != http.StatusOK {
		t.Fatalf("delete all data: %d %s", code, body)
	}
	deadline := time.Now().Add(time.Minute)
	for s.count(`SELECT count(*) FROM v2.tombstones WHERE object_kind = 'space' AND status = 'done'`) < 2 {
		if time.Now().After(deadline) {
			t.Fatal("the spaces' propagation didn't finish within the minute")
		}
		time.Sleep(25 * time.Millisecond)
	}
	if hits := s.grepDB(token); len(hits) != 0 {
		t.Errorf("the words are still in: %v", hits)
	}
	if hits := s.grepObjects(token); len(hits) != 0 {
		t.Errorf("object storage still holds them in: %v", hits)
	}
	if s.count(`SELECT count(*) FROM v2.memories WHERE space_id = $1 AND lifecycle = 'kept'`, team.id) != 1 {
		t.Error("the team's record was forgotten")
	}
	if s.count(`SELECT count(*) FROM hubs WHERE id = ANY($1)`, []uuid.UUID{personal.id, project.id}) != 2 {
		t.Error("the spaces should stay, empty")
	}
	if s.count(`SELECT count(*) FROM api_keys WHERE user_id = $1 AND revoked_at IS NULL`, zz) == 0 {
		t.Error("deleting the data revoked the agents' keys")
	}
	for _, id := range []uuid.UUID{personal.id, project.id, team.id} {
		s.sealAndVerify(id)
	}
	// Again: nothing left to forget, so no new Forget, and V1 answers as
	// before; an API key isn't refused any more (there is nothing of V2's
	// to forget), only by V1's own permissions.
	tombstones := s.count(`SELECT count(*) FROM v2.tombstones WHERE object_kind = 'space'`)
	if code, body := s.v1Do("DELETE", "/v1/account/data", s.session(zz)); code != http.StatusOK {
		t.Errorf("again: %d %s", code, body)
	}
	if n := s.count(`SELECT count(*) FROM v2.tombstones WHERE object_kind = 'space'`); n != tombstones {
		t.Errorf("repeating it forgot the spaces again (%d tombstones, was %d)", n, tombstones)
	}
}

// testWriter sends a logger's lines to the test's log.
type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Helper()
	w.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}
