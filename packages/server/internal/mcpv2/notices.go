package mcpv2

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/MemaxLabs/memax/packages/server/internal/handler"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger/policy"
)

// Forget's notices (plan 25 §5.13, rule 7: "tells every agent on its next
// read"). A Forget queues one notice for every agent connection that read
// the memory, or is connected to its space; the connection's next response
// from any V2 tool carries it, once, in two places:
//
//   - the text, a short paragraph the model reads ("Forgotten in memax-v2:
//     M-0201. Drop anything you took from it …");
//   - _meta["app.memax/notices"], for clients: [{kind, space_id, space,
//     refs, message}], and recall's structuredContent.notices too.
//
// Taking a notice marks it delivered in the same statement, so it is told
// exactly once, whichever tool the agent calls next. The stdio server
// (memax mcp serve) reads the same notices over /v2/notices.

// MetaNotices is the _meta key of the notices a response carries.
const MetaNotices = "app.memax/notices"

// maxNotices bounds how many notices one response carries.
const maxNotices = 20

// deliverNotices adds the connection's waiting notices to a V2 response.
func (s *Server) deliverNotices(ctx context.Context, v *view, tool string, res *mcp.CallToolResult) *mcp.CallToolResult {
	if res == nil || v == nil {
		return res
	}
	p, errRes := v.principal(ctx)
	if errRes != nil || p == nil || p.Actor.Kind != policy.ActorAgent || p.Connection == nil {
		return res
	}
	notices, err := s.ledger.TakeNotices(ctx, p.Scope, p.Connection.ID, tool, maxNotices)
	if err != nil {
		s.log.WarnContext(ctx, "mcp: forget notices", "error", err)
		return res
	}
	if len(notices) == 0 {
		return res
	}
	names := map[uuid.UUID]string{}
	for _, sp := range v.spaces {
		names[sp.ID] = sp.Hub.Name
	}
	var lines []string
	meta := make([]map[string]any, 0, len(notices))
	out := make([]handler.MCPNotice, 0, len(notices))
	for _, n := range notices {
		name := names[n.SpaceID]
		if name == "" {
			name = n.Space
		}
		if name == "" {
			name = "a space"
		}
		msg := noticeMessage(n, name)
		lines = append(lines, msg)
		meta = append(meta, map[string]any{"kind": n.Kind, "space_id": n.SpaceID.String(), "space": name,
			"refs": n.Refs, "message": msg})
		out = append(out, handler.MCPNotice{Kind: n.Kind, Message: msg, SpaceID: n.SpaceID.String(), Refs: n.Refs})
	}
	text := strings.Join(lines, "\n")
	if len(res.Content) > 0 {
		if t, ok := res.Content[0].(*mcp.TextContent); ok {
			res.Content[0] = &mcp.TextContent{Text: t.Text + "\n\n" + text}
		} else {
			res.Content = append(res.Content, &mcp.TextContent{Text: text})
		}
	} else {
		res.Content = []mcp.Content{&mcp.TextContent{Text: text}}
	}
	if res.Meta == nil {
		res.Meta = mcp.Meta{}
	}
	res.Meta[MetaNotices] = meta
	if rec, ok := res.StructuredContent.(handler.MCPRecallOutput); ok {
		rec.Notices = append(rec.Notices, out...)
		res.StructuredContent = rec
	}
	s.log.InfoContext(ctx, "mcp: forget notices delivered", "connection", p.Connection.ID.String(), "notices", len(notices), "tool", tool)
	return res
}

// noticeMessage is what the agent reads.
func noticeMessage(n ledger.Notice, space string) string {
	if n.Kind == ledger.NoticeSpaceForgotten {
		return fmt.Sprintf("Everything in %s was forgotten. Drop anything you took from it, including what you saved in your own memory.", space)
	}
	return fmt.Sprintf("Forgotten in %s: %s. Drop anything you took from it, including what you saved in your own memory; it is gone from Memax and every compiled file.",
		space, strings.Join(n.Refs, ", "))
}

// PurgeSpace drops this process's cached copies of a space's compiled
// files (Forget's propagation signals it).
func (s *Server) PurgeSpace(spaceID uuid.UUID) {
	if s == nil {
		return
	}
	if d, ok := s.digest.(*compiledDigest); ok {
		d.purgeSpace(spaceID)
	}
}
