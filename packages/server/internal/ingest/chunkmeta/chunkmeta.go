package chunkmeta

import (
	"strings"
	"unicode"
)

// TagsText normalizes memory tags into plain lexical text for chunk search.
// Keep memories.tags as the source of truth; this is denormalized retrieval text.
func TagsText(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	seen := make(map[string]bool, len(tags)*2)
	parts := make([]string, 0, len(tags)*2)
	for _, tag := range tags {
		tag = strings.TrimSpace(strings.ToLower(tag))
		if tag == "" {
			continue
		}
		addTagPart(tag, seen, &parts)
		expanded := expandTagSeparators(tag)
		if expanded != tag {
			addTagPart(expanded, seen, &parts)
		}
		if _, value, ok := strings.Cut(tag, ":"); ok {
			addTagPart(expandTagSeparators(value), seen, &parts)
		}
	}
	return strings.Join(parts, " ")
}

// MetadataText normalizes structured memory metadata that should participate
// in lexical retrieval without being stored as user/LLM semantic tags.
func MetadataText(authorName, hubName, hubSlug, source, sourceAgent, projectRepo string) string {
	values := []struct {
		prefix string
		value  string
	}{
		{prefix: "author", value: authorName},
		{prefix: "hub", value: hubName},
		{prefix: "hub", value: hubSlug},
		{prefix: "source", value: source},
		{prefix: "agent", value: sourceAgent},
		{prefix: "repo", value: projectRepo},
	}
	seen := make(map[string]bool, len(values)*4)
	parts := make([]string, 0, len(values)*4)
	for _, item := range values {
		value := strings.TrimSpace(strings.ToLower(item.value))
		if value == "" {
			continue
		}
		if item.prefix == "source" && isLowSignalSource(value) {
			continue
		}
		addTagPart(value, seen, &parts)
		expanded := expandTagSeparators(value)
		if expanded != value {
			addTagPart(expanded, seen, &parts)
		}
		if item.prefix != "" {
			prefixed := item.prefix + ":" + value
			addTagPart(prefixed, seen, &parts)
			addTagPart(expandTagSeparators(prefixed), seen, &parts)
		}
	}
	return strings.Join(parts, " ")
}

// EmbeddingTagsSuffix returns a "Tags: ..." line suitable for appending to
// chunk 0's embedding text. Tags are lowercased, deduplicated, short tags
// (< 2 chars) are excluded, and output is capped at maxEmbedTags to avoid
// embedding drift from noisy LLM-derived tags.
//
// Returns empty string when no qualifying tags remain.
func EmbeddingTagsSuffix(tags []string) string {
	const maxEmbedTags = 10

	seen := make(map[string]bool, len(tags))
	clean := make([]string, 0, len(tags))
	for _, tag := range tags {
		t := strings.TrimSpace(strings.ToLower(tag))
		if len([]rune(t)) < 2 || seen[t] {
			continue
		}
		seen[t] = true
		clean = append(clean, t)
		if len(clean) >= maxEmbedTags {
			break
		}
	}
	if len(clean) == 0 {
		return ""
	}
	return "Tags: " + strings.Join(clean, ", ")
}

func isLowSignalSource(source string) bool {
	switch source {
	case "api", "web", "mcp":
		return true
	default:
		return false
	}
}

func addTagPart(value string, seen map[string]bool, parts *[]string) {
	value = strings.Join(strings.Fields(value), " ")
	if value == "" || seen[value] {
		return
	}
	seen[value] = true
	*parts = append(*parts, value)
}

func expandTagSeparators(tag string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == ':' || r == '-' || r == '_' || r == '/' || unicode.IsSpace(r):
			return ' '
		default:
			return r
		}
	}, tag)
}
