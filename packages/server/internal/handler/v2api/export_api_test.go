package v2api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/auth"
	"github.com/MemaxLabs/memax/packages/server/internal/export"
	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/lifecycle"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
	"github.com/MemaxLabs/memax/packages/server/internal/receiptchain"
)

// countingReads counts the reads handed to the recorder.
type countingReads struct{ n atomic.Int64 }

func (c *countingReads) Record(ledger.ReadEvent) { c.n.Add(1) }

// exportCall exports sp with an Idempotency-Key.
func (e *env) exportSpace(token string, sp space, key string) *resp {
	e.t.Helper()
	return e.do(call{method: "POST", path: "/v2/spaces/" + sp.slug + ":export", token: token,
		header: map[string]string{"Idempotency-Key": key}})
}

// readExport opens an export's archive and reads it back.
func readExport(t *testing.T, r *resp) (string, map[string][]byte, *export.Record) {
	t.Helper()
	if r.status != http.StatusOK {
		t.Fatalf("export: %d %s", r.status, r.body)
	}
	root, files, err := export.ReadZip(r.body)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := export.Parse(files)
	if err != nil {
		t.Fatal(err)
	}
	return root, files, rec
}

// sealAll seals the space's receipts until none wait.
func (e *env) sealAll(sp space, signer receiptchain.Signer) {
	e.t.Helper()
	deadline := time.Now().Add(90 * time.Second) // the watermark is the cluster's
	for {
		res, err := e.ledger.SealSpace(context.Background(), sp.id, signer, 1000)
		if err != nil {
			e.t.Fatal(err)
		}
		total := e.count(`SELECT count(*) FROM v2.receipts WHERE space_id = $1`, sp.id)
		if e.count(`SELECT COALESCE(max(position), 0) FROM v2.receipt_chain_heads WHERE space_id = $1`, sp.id) == total && !res.More {
			return
		}
		if time.Now().After(deadline) {
			e.t.Fatal("sealing never caught up")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestExportRoundTrip is rule 14 on the real stack: a space with a
// decision (edited, with sources and decision fields), a fact with a
// scope and a stale date, an agent's proposal, an answered gate, a Brief,
// a compiled target and a forgotten memory is exported over /v2, read back
// from the archive, and is the same record, field for field, as the
// ledger serves it; its receipts verify against the signed checkpoints;
// the forgotten memory left a tombstone and no words; and the export is
// one receipt and no reads.
func TestExportRoundTrip(t *testing.T) {
	t.Parallel()
	signer, err := receiptchain.NewEd25519Signer(bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatal(err)
	}
	keys := receiptchain.Keyring{}
	keys.Add(signer.Public())
	reads := &countingReads{}
	e := newEnv(t, v2api.WithReceiptKeys(keys), v2api.WithReads(reads))
	ctx := context.Background()
	zz := e.user("zz")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	tok := e.session(zz)
	base := "/v2/spaces/" + sp.slug

	var dec result
	e.do(call{method: "POST", path: base + "/memories", token: tok, body: map[string]any{
		"statement": "Background jobs run on River.", "section": "decisions", "kind": "decision",
		"decision": map[string]any{"why": "One database, one transaction.", "status": "in_force",
			"options": []map[string]any{{"label": "River", "detail": "Postgres-backed"}, {"label": "Temporal"}}},
		"conditions": []map[string]any{{"kind": "file_exists", "path": "go.mod"}},
		"sources": []map[string]any{
			{"kind": "pr", "ref": "PR #212", "uri": "https://github.com/MemaxLabs/memax/pull/212"},
			{"kind": "file", "ref": "go.mod:14", "locator": map[string]any{"path": "go.mod", "line": 14}, "quote": "github.com/riverqueue/river"},
		}}}).ok(http.StatusCreated, &dec)
	e.do(call{method: "POST", path: "/v2/memories/" + dec.Memory.Ref + ":edit?space=" + sp.slug, token: tok,
		header: map[string]string{"If-Match": `"1"`},
		body:   map[string]any{"statement": `Background jobs run on River, Postgres-backed ("one queue" & <one> DB).`}}).ok(http.StatusOK, nil)
	stale := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	var fact result
	e.do(call{method: "POST", path: base + "/memories", token: tok, body: map[string]any{
		"statement": "pnpm workspaces only.\nNever run `npm install` at the root.", "section": "conventions",
		"stale_after": stale, "scope": map[string]any{"paths": []string{"packages/**"}}}}).ok(http.StatusCreated, &fact)
	secret := "The staging database password rotates on Thursdays at 06:00."
	gone := e.remember(tok, sp, secret).Memory

	key, _ := e.apiKey(zz, keyOpts{agent: "codex"})
	var proposal result
	e.do(call{method: "POST", path: base + "/memories", token: key, header: map[string]string{"X-Memax-Via": "mcp"},
		body: map[string]any{"statement": "Deploy the v2 API to Fly.io in iad.", "section": "decisions",
			"sources": []map[string]any{{"kind": "session", "ref": "session cx-7f3a"}}}}).ok(http.StatusCreated, &proposal)
	if proposal.Outcome != "proposed" {
		t.Fatalf("the agent's write = %s", proposal.Outcome)
	}
	var gate struct {
		Gate struct {
			ID uuid.UUID `json:"id"`
		} `json:"gate"`
	}
	e.do(call{method: "POST", path: base + "/gates", token: key, body: map[string]any{
		"question": "iad or ams for the v2 API?", "options": []map[string]any{{"label": "iad"}, {"label": "ams"}}}}).
		ok(http.StatusCreated, &gate)
	e.do(call{method: "POST", path: "/v2/gates/" + gate.Gate.ID.String() + ":answer", token: tok,
		header: map[string]string{"If-Match": `"1"`}, body: map[string]any{"option": 2}}).ok(http.StatusOK, nil)

	e.do(call{method: "POST", path: base + "/brief", token: tok, body: map[string]any{"title": "memax-v2",
		"summary": "How memax-v2 is built.", "sections": []map[string]any{
			{"key": "decisions", "heading": "Decisions", "items": []map[string]any{{"ref": dec.Memory.Ref},
				{"text": "Everything async goes through one queue.", "cites": []string{dec.Memory.Ref}}}},
			{"key": "conventions", "heading": "Conventions", "items": []map[string]any{{"ref": fact.Memory.Ref}}}}}}).
		ok(http.StatusCreated, nil)
	e.do(call{method: "POST", path: base + "/targets", token: tok, body: map[string]any{"kind": "agents_md"}}).ok(http.StatusCreated, nil)
	e.compileAll()
	e.do(call{method: "POST", path: "/v2/memories/" + gone.Ref + ":forget?space=" + sp.slug, token: tok,
		header: map[string]string{"If-Match": `"1"`}, body: map[string]any{}}).ok(http.StatusOK, nil)
	e.sealAll(sp, signer)

	res := e.exportSpace(tok, sp, "export-1")
	root, files, rec := readExport(t, res)
	if root != sp.slug {
		t.Errorf("the archive's folder is %s, want %s", root, sp.slug)
	}
	if cd := res.header.Get("Content-Disposition"); !strings.HasPrefix(cd, `attachment; filename="memax-`+sp.slug+`-`) {
		t.Errorf("Content-Disposition = %q", cd)
	}
	exportReceipt := uuid.MustParse(res.header.Get("X-Memax-Export-Receipt"))
	if rec.Manifest.Receipt == nil || *rec.Manifest.Receipt != exportReceipt {
		t.Errorf("export.json names receipt %v, the header %s", rec.Manifest.Receipt, exportReceipt)
	}

	scope, err := e.ledger.UserScope(ctx, zz)
	if err != nil {
		t.Fatal(err)
	}
	scope = scope.Narrow(sp.id)

	// Every memory that isn't forgotten, as the ledger serves it.
	page, err := e.ledger.ListMemories(ctx, scope, ledger.MemoryQuery{SpaceID: sp.id, Limit: 200,
		States: []lifecycle.Mark{lifecycle.MarkKept, lifecycle.MarkProposed, lifecycle.MarkStale, lifecycle.MarkConflict,
			lifecycle.MarkMerged, lifecycle.MarkFaded, lifecycle.MarkRejected}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Memories) != len(page.Memories) || len(rec.Memories) != 4 {
		t.Fatalf("the export holds %d memories, the ledger %d", len(rec.Memories), len(page.Memories))
	}
	for _, got := range rec.Memories {
		h, err := e.ledger.GetMemoryHistory(ctx, scope, got.Memory.Ref)
		if err != nil {
			t.Fatal(err)
		}
		want := *h.Memory
		want.Judge, want.Updates = nil, nil
		sameJSON(t, got.Memory.Ref, got.Memory, want)
		versions := append([]ledger.MemoryVersion(nil), h.Versions...)
		sort.Slice(versions, func(i, j int) bool { return versions[i].Version < versions[j].Version })
		sameJSON(t, got.Memory.Ref+" versions", got.Versions, versions)
		receipts, err := e.ledger.ListReceipts(ctx, scope, ledger.ReceiptQuery{SpaceID: sp.id, ObjectID: want.ID, Limit: 200})
		if err != nil {
			t.Fatal(err)
		}
		var stamps []ledger.ReceiptStamp
		for i := len(receipts.Receipts) - 1; i >= 0; i-- {
			r := receipts.Receipts[i]
			stamps = append(stamps, ledger.ReceiptStamp{ID: r.ID, Seq: r.Seq, Action: r.Action, OccurredAt: r.OccurredAt})
		}
		sameJSON(t, got.Memory.Ref+" receipts", got.Receipts, stamps)
	}

	// The Brief, the gates and the targets.
	briefs, err := e.ledger.ListBriefVersions(ctx, scope, ledger.BriefVersionQuery{SpaceID: sp.id})
	if err != nil {
		t.Fatal(err)
	}
	for i := range briefs.Versions {
		briefs.Versions[i].Receipt = nil
	}
	sort.Slice(briefs.Versions, func(i, j int) bool { return briefs.Versions[i].Version < briefs.Versions[j].Version })
	sameJSON(t, "briefs", rec.Briefs, briefs.Versions)
	gates, err := e.ledger.ListGates(ctx, scope, ledger.GateQuery{SpaceID: sp.id})
	if err != nil {
		t.Fatal(err)
	}
	sameJSON(t, "gates", rec.Gates, gates.Gates)
	targets, err := e.ledger.ListTargets(ctx, scope, sp.id)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].LastCompile == nil {
		t.Fatalf("targets = %+v", targets)
	}
	sameJSON(t, "targets", rec.Targets, targets)

	// Every receipt, as Activity lists them, in chain order and verified.
	var all []ledger.Receipt
	for cursor := ""; ; {
		p, err := e.ledger.ListReceipts(ctx, scope, ledger.ReceiptQuery{SpaceID: sp.id, Cursor: cursor, Limit: 200})
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, p.Receipts...)
		if !p.HasMore {
			break
		}
		cursor = p.NextCursor
	}
	if len(rec.Receipts) != len(all) {
		t.Fatalf("the export holds %d receipts, the ledger %d", len(rec.Receipts), len(all))
	}
	byID := map[uuid.UUID]ledger.Receipt{}
	for _, r := range all {
		byID[r.ID] = r
	}
	for _, r := range rec.Receipts {
		want, ok := byID[r.ID]
		if !ok {
			t.Fatalf("receipt %s isn't the ledger's", r.ID)
		}
		got := ledger.Receipt{ID: r.ID, Seq: r.Seq, TenantID: r.TenantID, SpaceID: r.SpaceID, ObjectKind: r.ObjectKind,
			ObjectID: r.ObjectID, ObjectRef: r.ObjectRef, Action: ledger.Action(r.Action), ActorKind: policy.ActorKind(r.ActorKind),
			ActorID: r.ActorID, Agent: deref(r.Agent), Via: policy.Via(r.Via), Assurance: policy.Assurance(deref(r.Assurance)),
			SessionRef: deref(r.SessionRef), Reason: deref(r.Reason), OccurredAt: r.OccurredAt, RecordedAt: r.RecordedAt,
			StreamID: r.StreamID, StreamVersion: int(r.StreamVersion)}
		if r.SourceKind != nil {
			got.Source = &ledger.ReceiptSource{Kind: *r.SourceKind, Ref: deref(r.SourceRef)}
		}
		sameJSON(t, "receipt "+r.ID.String(), got, want)
	}
	exported := byID[exportReceipt]
	if exported.Action != ledger.ActionExported || exported.ObjectKind != ledger.ObjectSpace || exported.ActorID == nil ||
		*exported.ActorID != zz {
		t.Errorf("the export's receipt = %+v", exported)
	}
	cps, embedded, err := rec.ChainCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := embedded[signer.KeyID()]; !ok || len(embedded) != 1 {
		t.Errorf("checkpoints.json embeds %v", embedded)
	}
	v := receiptchain.NewVerifier(sp.id, keys, cps)
	for _, r := range rec.Receipts {
		v.Add(r)
	}
	report := v.Finish()
	if !report.OK() || report.Signed != len(cps) || report.Receipts != int64(len(all)) || len(cps) == 0 {
		// What the sealer and the export each saw, to tell an ordering
		// fault from a test's race (it failed only on CI, Oct 7).
		for i, r := range rec.Receipts {
			t.Logf("export #%d %s seq %d", i+1, r.ID, r.Seq)
		}
		rows, err := e.pool.Query(ctx, `SELECT id, txid::text, seq, action, recorded_at FROM v2.receipts WHERE space_id = $1 ORDER BY txid, seq`, sp.id)
		if err == nil {
			for rows.Next() {
				var id uuid.UUID
				var txid, action string
				var seq int64
				var at time.Time
				if rows.Scan(&id, &txid, &seq, &action, &at) == nil {
					t.Logf("db %s txid %s seq %d %s %s", id, txid, seq, action, at.Format(time.RFC3339Nano))
				}
			}
			rows.Close()
		}
		for _, c := range cps {
			t.Logf("checkpoint %d: positions %d-%d, first %s, last %s, last seq %d", c.Number, c.PositionFrom, c.PositionTo, c.FirstReceiptID, c.LastReceiptID, c.LastSeq)
		}
		t.Fatalf("the export doesn't verify: %+v", report)
	}
	// Sealed through everything before the export; its own receipt waits.
	if rec.Manifest.Seal.SealedReceipts != int64(len(all)-1) || rec.Manifest.Seal.Unsealed != 1 {
		t.Errorf("seal = %+v of %d receipts", rec.Manifest.Seal, len(all))
	}

	// Forget: a tombstone, and the words nowhere.
	if len(rec.Tombstones) != 1 || rec.Tombstones[0].Ref != gone.Ref || rec.Tombstones[0].ReceiptID == uuid.Nil {
		t.Fatalf("tombstones = %+v", rec.Tombstones)
	}
	if _, ok := files["memories/"+gone.Ref+".md"]; ok {
		t.Error("the forgotten memory has a memory file")
	}
	for name, data := range files {
		for _, words := range []string{secret, "password rotates"} {
			if bytes.Contains(data, []byte(words)) {
				t.Errorf("%s holds the forgotten words", name)
			}
		}
	}

	// One receipt for the export, no reads.
	if n := e.count(`SELECT count(*) FROM v2.receipts WHERE space_id = $1 AND action = 'exported'`, sp.id); n != 1 {
		t.Errorf("%d exported receipts", n)
	}
	if reads.n.Load() != 0 {
		t.Errorf("the export recorded %d reads", reads.n.Load())
	}
}

// TestExportIsDeterministic: a retry with the same Idempotency-Key writes
// no receipt and gives the same bytes; a new export is a new receipt.
func TestExportIsDeterministic(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz := e.user("zz")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	tok := e.session(zz)
	e.remember(tok, sp, "Background jobs run on River.")
	e.remember(tok, sp, "pnpm workspaces only.")

	a := e.exportSpace(tok, sp, "export-a")
	b := e.exportSpace(tok, sp, "export-a")
	if a.status != http.StatusOK || b.status != http.StatusOK {
		t.Fatalf("export: %d, %d", a.status, b.status)
	}
	if !bytes.Equal(a.body, b.body) {
		t.Error("a retried export isn't byte-identical")
	}
	if b.header.Get("Idempotent-Replayed") != "true" || a.header.Get("Idempotent-Replayed") != "" {
		t.Errorf("Idempotent-Replayed: %q then %q", a.header.Get("Idempotent-Replayed"), b.header.Get("Idempotent-Replayed"))
	}
	if n := e.count(`SELECT count(*) FROM v2.receipts WHERE space_id = $1 AND action = 'exported'`, sp.id); n != 1 {
		t.Errorf("a retry wrote a receipt: %d", n)
	}
	c := e.exportSpace(tok, sp, "export-c")
	_, _, rec := readExport(t, c)
	if n := e.count(`SELECT count(*) FROM v2.receipts WHERE space_id = $1 AND action = 'exported'`, sp.id); n != 2 ||
		rec.Manifest.Counts.Receipts != 4 {
		t.Errorf("a second export: %d exported receipts, %d in the export", n, rec.Manifest.Counts.Receipts)
	}
}

// TestExportIsAPersonsInTheirSpaces: a person exports the spaces they can
// read, a viewer too; another person's space is not found; agents and an
// impersonation session are refused; and exports are rate-limited.
func TestExportIsAPersonsInTheirSpaces(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	zz, jy, vi := e.user("zz"), e.user("jy"), e.user("vi")
	sp := e.space(zz, policy.SpaceTeam, "memax-team")
	other := e.space(jy, policy.SpaceProject, "jiahao")
	e.join(sp, vi, "viewer")
	tok := e.session(zz)
	e.remember(tok, sp, "Background jobs run on River.")
	e.remember(e.session(jy), other, "Jiahao's own words.")

	_, files, _ := readExport(t, e.exportSpace(e.session(vi), sp, "viewer"))
	for name, data := range files {
		if bytes.Contains(data, []byte("Jiahao's own words.")) {
			t.Errorf("%s holds another space's words", name)
		}
	}
	e.exportSpace(tok, other, "theirs").fails(http.StatusNotFound, "not_found")
	e.do(call{method: "POST", path: "/v2/spaces/" + other.id.String() + ":export", token: tok}).fails(http.StatusNotFound, "not_found")

	key, _ := e.apiKey(zz, keyOpts{agent: "codex"})
	if got := e.exportSpace(key, sp, "agent").fails(http.StatusForbidden, "refused"); got.Details.Policy.Code != "export_by_person" {
		t.Errorf("an agent's export: %+v", got.Details.Policy)
	}
	admin := e.user("admin")
	imp, err := auth.SignImpersonationToken(zz.String(), admin.String(), []byte(testSecret), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	e.exportSpace(imp, sp, "impersonated").fails(http.StatusForbidden, "impersonation_read_only")
	e.do(call{method: "POST", path: "/v2/spaces/" + sp.slug + ":export", token: tok, invalid: true,
		header: map[string]string{"Idempotency-Key": ""}}).fails(http.StatusBadRequest, "idempotency_key_required")
	if n := e.count(`SELECT count(*) FROM v2.receipts WHERE action = 'exported'`); n != 1 {
		t.Errorf("%d exported receipts, want the viewer's one", n)
	}

	// zz spent two of their burst on the other space's 404s.
	limited := false
	for i := range 10 {
		r := e.exportSpace(tok, sp, uuid.NewString())
		if r.status == http.StatusTooManyRequests {
			r.fails(http.StatusTooManyRequests, "rate_limited")
			if r.header.Get("Retry-After") == "" || i < 1 {
				t.Errorf("rate limited after %d exports, Retry-After %q", i, r.header.Get("Retry-After"))
			}
			limited = true
			break
		}
	}
	if !limited {
		t.Error("ten exports in a row were never rate-limited")
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// sameJSON compares two values by their JSON, with times in UTC and empty
// lists as none.
func sameJSON(t *testing.T, what string, got, want any) {
	t.Helper()
	g, w := plainJSON(t, got), plainJSON(t, want)
	if !reflect.DeepEqual(g, w) {
		gj, _ := json.MarshalIndent(g, "", "  ")
		wj, _ := json.MarshalIndent(w, "", "  ")
		t.Errorf("%s isn't the same record:\n got %s\nwant %s", what, gj, wj)
	}
}

func plainJSON(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var out any
	if err := d.Decode(&out); err != nil {
		t.Fatal(err)
	}
	return normalizeJSON(out)
}

func normalizeJSON(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, item := range x {
			if n := normalizeJSON(item); n == nil {
				delete(x, k)
			} else {
				x[k] = n
			}
		}
	case []any:
		if len(x) == 0 {
			return nil
		}
		for i, item := range x {
			x[i] = normalizeJSON(item)
		}
	case string:
		if ts, err := time.Parse(time.RFC3339Nano, x); err == nil {
			return ts.UTC().Format(time.RFC3339Nano)
		}
		if x == "" {
			return nil
		}
	}
	return v
}
