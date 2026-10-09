package mcpv2

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/v2recall"
)

// Digester builds what memax_recall returns without a query (session
// start, plan 25 §5.11): each space's digest. The compiled digest
// (compiled.go: each space's latest compile, through compile.Service's
// preview) is the default when there is a compile service; the lexical
// one below serves spaces that haven't compiled yet.
type Digester interface {
	Digest(ctx context.Context, scope ledger.Scope, spaces []SpaceRef, since *time.Time) (Digest, error)
}

// SpaceRef names a space for a Digester.
type SpaceRef struct {
	ID   uuid.UUID
	Name string
	Slug string
	// ReviewURL is the space's Review, when there is a web app.
	ReviewURL string
}

// Digest is a Digester's answer.
type Digest struct {
	Spaces []handler.MCPSpaceDigest
	// Forgotten lists, per space, the memories forgotten since the agent
	// was last seen (refs only: the words are gone).
	Forgotten map[uuid.UUID][]string
}

// digestPerSection is how many kept memories each section shows.
const digestPerSection = 8

// lexicalDigest is the stand-in until the compiled digest ships: the
// newest kept memories in each section, what changed since the agent was
// last seen, what was forgotten, and how much waits in Review.
type lexicalDigest struct {
	search *v2recall.Searcher
}

func (d lexicalDigest) Digest(ctx context.Context, scope ledger.Scope, spaces []SpaceRef, since *time.Time) (Digest, error) {
	out := Digest{Forgotten: map[uuid.UUID][]string{}}
	ids := make([]uuid.UUID, len(spaces))
	for i, sp := range spaces {
		ids[i] = sp.ID
	}
	raw, err := d.search.Digest(ctx, scope, ids, digestPerSection, since)
	if err != nil {
		return out, err
	}
	for i, r := range raw {
		sp := spaces[i]
		sd := handler.MCPSpaceDigest{SpaceID: sp.ID.String(), Space: sp.Name, Sections: []handler.MCPDigestSection{},
			Changed: r.Changed, WaitingInReview: r.Waiting, ReviewURL: sp.ReviewURL}
		if since != nil {
			sd.ChangedSince = since.UTC().Format(time.RFC3339)
		}
		for _, section := range ledger.Sections {
			hits := r.Sections[section]
			if len(hits) == 0 {
				continue
			}
			sec := handler.MCPDigestSection{Section: string(section), Memories: []handler.MCPItem{}}
			for _, h := range hits {
				sec.Memories = append(sec.Memories, handler.MCPItem{
					ID: h.ID.String(), Ref: h.Ref, Record: handler.MCPRecordV2, SpaceID: sp.ID.String(), Space: sp.Name,
					Text: h.Statement, Section: string(h.Section), Kind: string(h.Kind), State: h.State,
				})
			}
			sd.Sections = append(sd.Sections, sec)
		}
		if len(r.Forgotten) > 0 {
			out.Forgotten[sp.ID] = r.Forgotten
		}
		out.Spaces = append(out.Spaces, sd)
	}
	return out, nil
}
