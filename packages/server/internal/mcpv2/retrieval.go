package mcpv2

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/MemaxLabs/memax/packages/server/internal/v2recall"
)

// MetaRetrieval is the _meta key of a recall or search result in spaces on
// the V2 record: how the with-query path went (plan 25 §5.11). It sits
// beside structuredContent's lexical_only, which says the same thing in
// one bit, and adds why:
//
//	{"app.memax/retrieval": {"vector": "ok|off|timeout|error", "rerank": "ok|off|skipped|timeout|error",
//	                         "lexical_only": true, "embed_ms": 41}}
//
// vector "timeout" is the 120 ms embedding deadline missed: the answer is
// lexical only. rerank "timeout" kept the fused order. The key follows
// MCP's _meta naming (a reverse-DNS prefix and a name), so clients that
// don't know it ignore it.
const MetaRetrieval = "app.memax/retrieval"

// withRetrieval puts the retrieval in res's _meta, when a query ran.
func withRetrieval(res *mcp.CallToolResult, r *v2recall.Retrieval, lexicalOnly bool) *mcp.CallToolResult {
	if res == nil || r == nil {
		return res
	}
	if res.Meta == nil {
		res.Meta = mcp.Meta{}
	}
	res.Meta[MetaRetrieval] = map[string]any{
		"vector": r.Vector, "rerank": r.Rerank, "lexical_only": lexicalOnly, "embed_ms": r.EmbedMS,
	}
	return res
}
