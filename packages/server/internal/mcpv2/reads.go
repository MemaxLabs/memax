package mcpv2

import (
	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/handler/v2api"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Reads (R-, plan 25 §5.3) are not receipts: they are high-volume and must
// never sit on recall's latency path. Every V2 read path here (recall,
// search, get, list and the session-start digest) hands what it returned to
// a ledger.ReadRecorder after the work of the response is done, from the
// result's items, never from inside the query code. The recorder
// (internal/reads) never blocks: it buffers, flushes to v2.reads every
// 250 ms, and drops (and counts) reads when its buffer is full.

// ReadRecorder records agent reads off the request path.
type ReadRecorder = ledger.ReadRecorder

type noReads struct{}

func (noReads) Record(ledger.ReadEvent) {}

// recordRead records an agent's read, one event per space that returned
// something: the memories it returned, or the compile its digest served.
// A read that returned nothing anywhere is still recorded for each space it
// searched, so the agent's activity shows. Pending proposals aren't reads
// of the record.
func (s *Server) recordRead(p *v2api.Principal, kind ledger.ReadKind, sessionRef string, spaces []space, out handler.MCPRecallOutput) {
	if p == nil || p.Actor.Kind != policy.ActorAgent || p.Connection == nil {
		return
	}
	at := s.now()
	events := map[uuid.UUID]*ledger.ReadEvent{}
	var order []uuid.UUID
	event := func(spaceID uuid.UUID) *ledger.ReadEvent {
		if e, ok := events[spaceID]; ok {
			return e
		}
		sp := spaceOf(spaces, spaceID)
		e := &ledger.ReadEvent{SpaceID: spaceID, TenantID: sp.Grant.TenantID, Reader: ledger.ReaderAgent,
			ConnectionID: p.Connection.ID, PersonID: p.Scope.PersonID, Agent: string(p.Connection.Agent),
			Kind: kind, Via: policy.ViaMCP, SessionRef: sessionRef, At: at}
		events[spaceID] = e
		order = append(order, spaceID)
		return e
	}
	add := func(items []handler.MCPItem) {
		for _, it := range items {
			if it.Record != handler.MCPRecordV2 {
				continue
			}
			spaceID, err := uuid.Parse(it.SpaceID)
			if err != nil {
				continue
			}
			e := event(spaceID)
			e.Count++
			if id, err := uuid.Parse(it.ID); err == nil {
				e.Memories = append(e.Memories, id)
			}
		}
	}
	add(out.Results)
	for _, d := range out.Digest {
		spaceID, err := uuid.Parse(d.SpaceID)
		if err != nil {
			continue
		}
		if d.Compiled != nil {
			event(spaceID).CompileRef = d.Compiled.Ref
		}
		for _, sec := range d.Sections {
			add(sec.Memories)
		}
	}
	if len(events) == 0 {
		for _, sp := range spaces {
			event(sp.ID)
		}
	}
	for _, id := range order {
		if e := events[id]; e.TenantID != uuid.Nil {
			s.reads.Record(*e)
		}
	}
}
