package handler

// Structured output of the MCP read tools (and memax_push), shared by the
// V1 tools here and the V2 tools in internal/mcpv2. Each type matches the
// tool's outputSchema in mcp_tools.json; the tests validate real results
// against those schemas. Every result also carries the same information as
// text, for clients that only read content.

// MCPItem is one memory in a result: a V2 statement or a V1 excerpt.
type MCPItem struct {
	ID        string  `json:"id"`
	Ref       string  `json:"ref,omitempty"`
	Record    string  `json:"record"`
	SpaceID   string  `json:"space_id,omitempty"`
	Space     string  `json:"space,omitempty"`
	Title     string  `json:"title,omitempty"`
	Text      string  `json:"text"`
	Summary   string  `json:"summary,omitempty"`
	Section   string  `json:"section,omitempty"`
	Kind      string  `json:"kind,omitempty"`
	State     string  `json:"state,omitempty"`
	Stability string  `json:"stability,omitempty"`
	Score     float64 `json:"score,omitempty"`
	Age       string  `json:"age,omitempty"`
	Source    string  `json:"source,omitempty"`
	URL       string  `json:"url,omitempty"`
}

// The records an item can come from.
const (
	MCPRecordV1 = "v1"
	MCPRecordV2 = "v2"
)

// MCPRecallOutput is memax_recall's structured result.
type MCPRecallOutput struct {
	Results   []MCPItem        `json:"results"`
	Proposals []MCPItem        `json:"proposals,omitempty"`
	Digest    []MCPSpaceDigest `json:"digest,omitempty"`
	Notices   []MCPNotice      `json:"notices,omitempty"`
	// Gates are the decisions this connection asked for: how each ended
	// (once), and without a query the ones still waiting.
	Gates       []MCPGate `json:"gates,omitempty"`
	Partial     bool      `json:"partial,omitempty"`
	LexicalOnly bool      `json:"lexical_only,omitempty"`
}

// MCPGate is a decision gate (G-) in a result: memax_request_decision's
// answer, and in recall how a gate the connection asked ended.
type MCPGate struct {
	ID       string `json:"id"`
	SpaceID  string `json:"space_id"`
	Space    string `json:"space,omitempty"`
	Question string `json:"question"`
	// Status is waiting, answered, withdrawn or expired.
	Status string `json:"status"`
	// Option and Answer are the chosen option (from 1) and its label, and
	// Memory the kept decision it became.
	Option    int    `json:"option,omitempty"`
	Answer    string `json:"answer,omitempty"`
	Memory    string `json:"memory,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
	URL       string `json:"url,omitempty"`
	// Message is memax_request_decision's sentence; recall leaves it out.
	Message string `json:"message,omitempty"`
}

// MCPSpaceDigest is one space's digest (memax_recall without a query).
type MCPSpaceDigest struct {
	SpaceID         string             `json:"space_id"`
	Space           string             `json:"space"`
	Sections        []MCPDigestSection `json:"sections"`
	ChangedSince    string             `json:"changed_since,omitempty"`
	Changed         int                `json:"changed,omitempty"`
	WaitingInReview int                `json:"waiting_in_review,omitempty"`
	ReviewURL       string             `json:"review_url,omitempty"`
	// Compiled is the space's latest compiled file, when it has one; its
	// sections are then left empty (the file is the curated context).
	Compiled *MCPCompiled `json:"compiled,omitempty"`
}

// MCPCompiled is a compile run's output (C-), as a digest serves it.
type MCPCompiled struct {
	Ref        string `json:"ref"`
	Target     string `json:"target"`
	CompiledAt string `json:"compiled_at"`
	Content    string `json:"content"`
	Truncated  bool   `json:"truncated,omitempty"`
}

// MCPDigestSection is one section of a digest.
type MCPDigestSection struct {
	Section  string    `json:"section"`
	Memories []MCPItem `json:"memories"`
}

// MCPNotice is something the agent should know on its next read: a
// forgotten memory (kind forgotten), a kept write the judge sent back to
// Review (returned), a change in its autonomy.
type MCPNotice struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	// SpaceID and Refs name what was forgotten (refs empty for a whole
	// space).
	SpaceID string   `json:"space_id,omitempty"`
	Refs    []string `json:"refs,omitempty"`
}

// MCPSearchOutput is memax_search's structured result.
type MCPSearchOutput struct {
	Results     []MCPItem `json:"results"`
	Partial     bool      `json:"partial,omitempty"`
	LexicalOnly bool      `json:"lexical_only,omitempty"`
}

// MCPListOutput is memax_list's structured result.
type MCPListOutput struct {
	Memories   []MCPItem `json:"memories"`
	NextCursor string    `json:"next_cursor,omitempty"`
	HasMore    bool      `json:"has_more,omitempty"`
	Total      int       `json:"total,omitempty"`
}

// MCPGetOutput is memax_get's structured result.
type MCPGetOutput struct {
	Memory MCPMemoryDetail `json:"memory"`
}

// MCPMemoryDetail is one memory in full.
type MCPMemoryDetail struct {
	ID        string       `json:"id"`
	Ref       string       `json:"ref,omitempty"`
	Record    string       `json:"record"`
	SpaceID   string       `json:"space_id,omitempty"`
	Space     string       `json:"space,omitempty"`
	Title     string       `json:"title,omitempty"`
	Text      string       `json:"text"`
	Summary   string       `json:"summary,omitempty"`
	Section   string       `json:"section,omitempty"`
	Kind      string       `json:"kind,omitempty"`
	State     string       `json:"state,omitempty"`
	Stability string       `json:"stability,omitempty"`
	Trust     string       `json:"trust,omitempty"`
	Version   int          `json:"version,omitempty"`
	Source    string       `json:"source,omitempty"`
	Tags      []string     `json:"tags,omitempty"`
	CreatedAt string       `json:"created_at,omitempty"`
	URL       string       `json:"url,omitempty"`
	Sources   []MCPSource  `json:"sources,omitempty"`
	Receipts  []MCPReceipt `json:"receipts,omitempty"`
}

// MCPSource is a memory's source.
type MCPSource struct {
	Kind     string `json:"kind"`
	Ref      string `json:"ref"`
	URI      string `json:"uri,omitempty"`
	External bool   `json:"external,omitempty"`
	Trust    string `json:"trust,omitempty"`
}

// MCPReceipt is one receipt in a memory's history. Receipts never hold the
// memory's words.
type MCPReceipt struct {
	Action    string `json:"action"`
	ActorKind string `json:"actor_kind"`
	Agent     string `json:"agent,omitempty"`
	Via       string `json:"via"`
	Assurance string `json:"assurance,omitempty"`
	Reason    string `json:"reason,omitempty"`
	At        string `json:"at"`
}

// MCPPushOutput is memax_push's structured result.
type MCPPushOutput struct {
	Status    string `json:"status"`
	ID        string `json:"id"`
	SpaceID   string `json:"space_id,omitempty"`
	ReviewURL string `json:"review_url,omitempty"`
	Assurance string `json:"assurance,omitempty"`
	Message   string `json:"message,omitempty"`
}

// The push statuses.
const (
	MCPPushSaved    = "saved"
	MCPPushKept     = "kept"
	MCPPushProposed = "proposed"
	MCPPushNote     = "note"
)
