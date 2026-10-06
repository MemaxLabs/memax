package v2api_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// syncReads writes each read at once, so tests see it without waiting for
// the production recorder's flush (internal/reads is tested on its own).
type syncReads struct {
	t *testing.T
	l *ledger.Ledger
}

func (s *syncReads) Record(e ledger.ReadEvent) {
	if _, err := s.l.RecordReads(context.Background(), []ledger.ReadEvent{e}); err != nil {
		s.t.Errorf("RecordReads: %v", err)
	}
}

type readWire struct {
	ID           uuid.UUID  `json:"id"`
	Ref          string     `json:"ref"`
	SpaceID      uuid.UUID  `json:"space_id"`
	ReaderKind   string     `json:"reader_kind"`
	ConnectionID *uuid.UUID `json:"connection_id"`
	Agent        string     `json:"agent"`
	Kind         string     `json:"kind"`
	Via          string     `json:"via"`
	SessionRef   string     `json:"session_ref"`
	Compile      string     `json:"compile"`
	Memories     int        `json:"memories"`
	MemoryRefs   []string   `json:"memory_refs"`
}

type memoryReadsWire struct {
	Reads   int `json:"reads"`
	Reads7d int `json:"reads_7d"`
	Agents  int `json:"agents"`
	Readers []struct {
		ReaderKind string `json:"reader_kind"`
		Agent      string `json:"agent"`
		Reads      int    `json:"reads"`
	} `json:"readers"`
	Unobserved bool `json:"unobserved_target"`
}

func loadsPath(sp space) string { return "/v2/spaces/" + sp.id.String() + "/compile-loads" }

// A session start's compile load counts as a read of every fact in the
// compile; the agent reports its own loads where it is connected, a person
// names the agent, and the counts reach the memory, the agent and Activity.
func TestReadEndpoints(t *testing.T) {
	t.Parallel()
	rec := &syncReads{t: t}
	e := newEnv(t, v2api.WithReads(rec))
	rec.l = e.ledger
	zz, jy := e.user("zz"), e.user("jy")
	sp := e.space(zz, policy.SpaceProject, "memax-v2")
	tok := e.session(zz)
	river := e.remember(tok, sp, "Background jobs run on River.").Memory
	e.do(call{method: "POST", path: briefPath(sp), token: tok, body: briefBody("Memax V2 engineering brief",
		map[string]any{"key": "conventions", "heading": "Conventions", "items": []map[string]any{{"ref": river.Ref}}})}).
		ok(http.StatusCreated, nil)
	var tr targetResult
	e.do(call{method: "POST", path: targetsPath(sp), token: tok, body: map[string]any{"kind": "agents_md"}}).ok(http.StatusCreated, &tr)
	e.compileAll()
	var runs page[compileRun]
	e.do(call{method: "GET", path: targetPath(tr.Target.ID, "/runs"), token: tok}).ok(http.StatusOK, &runs)
	if len(runs.Items) != 1 {
		t.Fatalf("runs = %+v", runs)
	}
	run := runs.Items[0]

	key, keyID := e.apiKey(zz, keyOpts{agent: "codex"}) // connected in every space zz has now
	conn := e.connection(keyID)
	later := e.space(zz, policy.SpaceProject, "later") // made after: the key isn't connected here
	foreign := e.space(jy, policy.SpaceProject, "foreign")

	// The agent reports its own load: a read of the compile's one fact.
	var res struct {
		Read readWire `json:"read"`
	}
	idem := uuid.NewString()
	load := map[string]any{"compile": run.Ref, "session_ref": "cx-1"}
	r := e.do(call{method: "POST", path: loadsPath(sp), token: key, body: load, header: map[string]string{"Idempotency-Key": idem}})
	r.ok(http.StatusCreated, &res)
	first := res.Read
	if first.Kind != "compile_load" || first.Compile != run.Ref || first.Memories != 1 || first.ReaderKind != "agent" ||
		first.ConnectionID == nil || *first.ConnectionID != conn || first.Via != "api" || first.SessionRef != "cx-1" {
		t.Errorf("load = %+v", first)
	}
	r = e.do(call{method: "POST", path: loadsPath(sp), token: key, body: load, header: map[string]string{"Idempotency-Key": idem}})
	r.ok(http.StatusCreated, &res)
	if r.header.Get("Idempotent-Replayed") != "true" || res.Read.ID != first.ID {
		t.Errorf("replay = %+v (%q)", res.Read, r.header.Get("Idempotent-Replayed"))
	}
	e.do(call{method: "POST", path: loadsPath(sp), token: key, body: map[string]any{"compile": run.ID.String()},
		header: map[string]string{"Idempotency-Key": idem}}).fails(http.StatusUnprocessableEntity, "idempotency_key_reused")

	// Where it isn't connected it can't count a read; elsewhere is not found.
	if f := e.do(call{method: "POST", path: loadsPath(later), token: key, body: load}).fails(http.StatusForbidden, "refused"); f.Details.Policy.Code != "agent_not_connected" {
		t.Errorf("a space it isn't connected to: %+v", f.Details.Policy)
	}
	e.do(call{method: "POST", path: loadsPath(foreign), token: key, body: load}).fails(http.StatusNotFound, "not_found")
	e.do(call{method: "POST", path: loadsPath(sp), token: key, body: map[string]any{"compile": "C-9999"}}).fails(http.StatusNotFound, "not_found")
	e.do(call{method: "POST", path: loadsPath(sp), token: key,
		body: map[string]any{"compile": run.Ref, "loaded_at": time.Now().Add(-48 * time.Hour)}}).fails(http.StatusBadRequest, "invalid_request")

	// The person's CLI reports Claude Code's session start.
	var mine struct {
		Read readWire `json:"read"`
	}
	r = e.do(call{method: "POST", path: loadsPath(sp), token: tok, body: map[string]any{"compile": run.Ref, "agent": "claude-code"},
		header: map[string]string{"X-Memax-Via": "cli"}})
	r.ok(http.StatusCreated, &mine)
	if mine.Read.ReaderKind != "person" || mine.Read.Agent != "claude-code" || mine.Read.Via != "cli" || mine.Read.ConnectionID != nil {
		t.Errorf("person's load = %+v", mine.Read)
	}

	// The agent reads the memory over /v2: recorded off the request path.
	e.do(call{method: "GET", path: "/v2/memories/" + river.ID.String(), token: key}).ok(http.StatusOK, nil)
	var detail struct {
		Reads *memoryReadsWire `json:"reads"`
	}
	e.do(call{method: "GET", path: "/v2/memories/" + river.ID.String(), token: tok}).ok(http.StatusOK, &detail)
	if d := detail.Reads; d == nil || d.Reads != 3 || d.Reads7d != 3 || d.Agents != 2 || len(d.Readers) != 2 || d.Unobserved {
		t.Errorf("memory reads = %+v", detail.Reads)
	}

	var reads struct {
		Items   []readWire `json:"items"`
		HasMore bool       `json:"has_more"`
		Next    string     `json:"next_cursor"`
		Week    int        `json:"reads_7d"`
	}
	e.do(call{method: "GET", path: "/v2/spaces/" + sp.slug + "/reads?limit=2", token: tok}).ok(http.StatusOK, &reads)
	if len(reads.Items) != 2 || !reads.HasMore || reads.Week != 3 || reads.Items[0].Kind != "get" ||
		len(reads.Items[0].MemoryRefs) != 1 || reads.Items[0].MemoryRefs[0] != river.Ref {
		t.Errorf("reads = %+v", reads)
	}
	e.do(call{method: "GET", path: "/v2/spaces/" + sp.slug + "/reads?cursor=" + reads.Next, token: tok}).ok(http.StatusOK, &reads)
	if len(reads.Items) != 1 || reads.Items[0].ID != first.ID {
		t.Errorf("reads page 2 = %+v", reads.Items)
	}
	e.do(call{method: "GET", path: "/v2/spaces/" + foreign.id.String() + "/reads", token: tok}).fails(http.StatusNotFound, "not_found")

	// The agent's reads in Agents.
	var agentDetail struct {
		Agent struct {
			Reads  int `json:"reads_7d"`
			Spaces []struct {
				SpaceID uuid.UUID `json:"space_id"`
				Reads   int       `json:"reads_7d"`
			} `json:"spaces"`
		} `json:"agent"`
		Week struct {
			Reads int `json:"reads"`
		} `json:"this_week"`
		Sessions []struct {
			SessionRef string `json:"session_ref"`
			Reads      int    `json:"reads"`
			Writes     int    `json:"writes"`
		} `json:"sessions"`
	}
	e.do(call{method: "GET", path: agentPath(conn, ""), token: tok}).ok(http.StatusOK, &agentDetail)
	inSpace := 0
	for _, s := range agentDetail.Agent.Spaces {
		if s.SpaceID == sp.id {
			inSpace = s.Reads
		}
	}
	if agentDetail.Agent.Reads != 2 || inSpace != 2 || agentDetail.Week.Reads != 2 ||
		len(agentDetail.Sessions) != 1 || agentDetail.Sessions[0].SessionRef != "cx-1" || agentDetail.Sessions[0].Reads != 1 {
		t.Errorf("agent detail = %+v", agentDetail)
	}
}
