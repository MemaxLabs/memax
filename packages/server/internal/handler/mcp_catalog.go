package handler

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpCatalogJSON is the tool catalogue of both MCP profiles (mcp_tools.json).
// The CLI's local stdio server mirrors the agent profile, and
// scripts/check-mcp-parity.mjs fails lint when the two drift.
//
//go:embed mcp_tools.json
var mcpCatalogJSON []byte

// The canonical tool names. The ChatGPT profile's aliases map onto them
// (mcpCatalogTool.Canonical), so both profiles share one implementation.
const (
	toolRecall          = "memax_recall"
	toolSearch          = "memax_search"
	toolPush            = "memax_push"
	toolGet             = "memax_get"
	toolList            = "memax_list"
	toolHubs            = "memax_hubs"
	toolHubMembers      = "memax_hub_members"
	toolForget          = "memax_forget"
	toolCapture         = "memax_capture"
	toolRequestDecision = "memax_request_decision"
	toolTopics          = "memax_topics"
)

// MCPWriteTools are the canonical tools that change the record. A
// credential whose scope can't write gets 403 insufficient_scope for them
// in spaces on the V2 record (mcpStepUp).
var mcpWriteTools = map[string]bool{
	toolPush: true, toolForget: true, toolCapture: true, toolRequestDecision: true,
}

type mcpCatalogTool struct {
	Name         string          `json:"name"`
	Canonical    string          `json:"canonical,omitempty"`
	Title        string          `json:"title"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
	Annotations  struct {
		Title           string `json:"title"`
		ReadOnlyHint    bool   `json:"readOnlyHint"`
		DestructiveHint *bool  `json:"destructiveHint,omitempty"`
		IdempotentHint  bool   `json:"idempotentHint"`
		OpenWorldHint   *bool  `json:"openWorldHint,omitempty"`
	} `json:"annotations"`
	Meta map[string]any `json:"_meta,omitempty"`
}

type mcpCatalogProfile struct {
	Server       string           `json:"server"`
	Instructions string           `json:"instructions"`
	Tools        []mcpCatalogTool `json:"tools"`
}

type mcpCatalog struct {
	Item     json.RawMessage              `json:"item"`
	Profiles map[string]mcpCatalogProfile `json:"profiles"`
}

var (
	catalogOnce sync.Once
	catalog     *mcpCatalog
	catalogErr  error
)

// loadMCPCatalog parses mcp_tools.json once: "#item" references expand to
// the shared memory item schema, and an outputSchema that names a tool
// ("memax_recall") is that tool's output schema, so every served schema is
// self-contained.
func loadMCPCatalog() (*mcpCatalog, error) {
	catalogOnce.Do(func() {
		var c mcpCatalog
		if err := json.Unmarshal(mcpCatalogJSON, &c); err != nil {
			catalogErr = fmt.Errorf("mcp_tools.json: %w", err)
			return
		}
		var item any
		if err := json.Unmarshal(c.Item, &item); err != nil {
			catalogErr = fmt.Errorf("mcp_tools.json item: %w", err)
			return
		}
		agentOutputs := map[string]json.RawMessage{}
		for _, name := range []string{"agent", "chatgpt"} {
			p := c.Profiles[name]
			for i := range p.Tools {
				t := &p.Tools[i]
				if t.Canonical == "" {
					t.Canonical = t.Name
				}
				var named string
				if json.Unmarshal(t.OutputSchema, &named) == nil && named != "" {
					t.OutputSchema = agentOutputs[named]
					if t.OutputSchema == nil {
						catalogErr = fmt.Errorf("mcp_tools.json: %s names output schema %q, which isn't defined", t.Name, named)
						return
					}
				}
				if len(t.OutputSchema) > 0 {
					expanded, err := expandItemRefs(t.OutputSchema, item)
					if err != nil {
						catalogErr = fmt.Errorf("mcp_tools.json %s: %w", t.Name, err)
						return
					}
					t.OutputSchema = expanded
					if name == "agent" {
						agentOutputs[t.Name] = expanded
					}
				}
			}
			c.Profiles[name] = p
		}
		catalog = &c
	})
	return catalog, catalogErr
}

// expandItemRefs replaces every {"$ref": "#item"} in schema with item.
func expandItemRefs(schema json.RawMessage, item any) (json.RawMessage, error) {
	var v any
	if err := json.Unmarshal(schema, &v); err != nil {
		return nil, err
	}
	var walk func(any) any
	walk = func(n any) any {
		switch x := n.(type) {
		case map[string]any:
			if ref, ok := x["$ref"].(string); ok && ref == "#item" && len(x) == 1 {
				return item
			}
			for k, child := range x {
				x[k] = walk(child)
			}
		case []any:
			for i, child := range x {
				x[i] = walk(child)
			}
		}
		return n
	}
	return json.Marshal(walk(v))
}

// mcpProfileTools returns a profile's tools as the go-sdk serves them.
func mcpProfileTools(profile string) ([]*mcp.Tool, error) {
	c, err := loadMCPCatalog()
	if err != nil {
		return nil, err
	}
	p, ok := c.Profiles[profile]
	if !ok {
		return nil, fmt.Errorf("mcp_tools.json has no %q profile", profile)
	}
	out := make([]*mcp.Tool, 0, len(p.Tools))
	for _, t := range p.Tools {
		tool := &mcp.Tool{
			Name:        t.Name,
			Title:       t.Title,
			Description: t.Description,
			InputSchema: t.InputSchema,
			Annotations: &mcp.ToolAnnotations{
				Title:           t.Annotations.Title,
				ReadOnlyHint:    t.Annotations.ReadOnlyHint,
				DestructiveHint: t.Annotations.DestructiveHint,
				IdempotentHint:  t.Annotations.IdempotentHint,
				OpenWorldHint:   t.Annotations.OpenWorldHint,
			},
		}
		if len(t.OutputSchema) > 0 {
			tool.OutputSchema = t.OutputSchema
		}
		if len(t.Meta) > 0 {
			tool.Meta = mcp.Meta(t.Meta)
		}
		out = append(out, tool)
	}
	return out, nil
}

// mcpCanonicalName maps a tool name as called onto the implementation it
// shares (save_memory → memax_push). Unknown names come back unchanged.
func mcpCanonicalName(profile, name string) string {
	c, err := loadMCPCatalog()
	if err != nil {
		return name
	}
	for _, t := range c.Profiles[profile].Tools {
		if t.Name == name {
			return t.Canonical
		}
	}
	return name
}

// mcpProfileMeta returns a profile's server name and instructions.
func mcpProfileMeta(profile string) (name, instructions string) {
	c, err := loadMCPCatalog()
	if err != nil {
		return "memax", ""
	}
	p := c.Profiles[profile]
	return p.Server, strings.TrimSpace(p.Instructions)
}

// MCPOutputSchema returns the served output schema of a tool in a profile
// ("agent" or "chatgpt"), for tests that validate structured output.
func MCPOutputSchema(profile, name string) (json.RawMessage, error) {
	c, err := loadMCPCatalog()
	if err != nil {
		return nil, err
	}
	for _, t := range c.Profiles[profile].Tools {
		if t.Name == name {
			return t.OutputSchema, nil
		}
	}
	return nil, fmt.Errorf("no tool %q in profile %q", name, profile)
}
