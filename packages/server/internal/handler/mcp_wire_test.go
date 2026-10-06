package handler

import "encoding/json"

// The JSON shapes the V1 MCP tests decode responses into. V1's
// hand-rolled server defined them; the go-sdk server writes the same
// shapes, so the V1 suite (mcp_test.go) runs unchanged against it.

type mcpTool struct {
	Name         string          `json:"name"`
	Title        string          `json:"title"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema"`
	Annotations  map[string]any  `json:"annotations,omitempty"`
	Meta         map[string]any  `json:"_meta,omitempty"`
}

type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type mcpToolResult struct {
	Content           []mcpContent    `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	IsError           bool            `json:"isError,omitempty"`
}
