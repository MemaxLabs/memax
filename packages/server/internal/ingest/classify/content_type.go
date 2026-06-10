package classify

import (
	"regexp"
	"strings"
)

// Track represents the ingestion pipeline to use.
type Track string

const (
	TrackDocument   Track = "document"
	TrackExtraction Track = "extraction"
	TrackLink       Track = "link"
)

// speakerRe matches lines that look like speaker turns: "Alice:", "Bob:", "User:", "> quote"
var speakerRe = regexp.MustCompile(`(?m)^(?:\s*(?:[A-Z][a-zA-Z0-9_\- ]{0,30})\s*:|>\s)`)

// DetectTrack analyzes content and returns the appropriate ingestion track.
// This is a fast, deterministic classifier — no LLM call.
func DetectTrack(content string, contentType string) Track {
	trimmed := strings.TrimSpace(content)

	// Rule 1: URLs → link track
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return TrackLink
	}

	// Rule 2: Explicit content type
	ct := strings.ToLower(contentType)
	if ct == "chat" || ct == "transcript" {
		return TrackExtraction
	}

	lines := strings.Split(trimmed, "\n")
	totalLines := len(lines)
	if totalLines == 0 {
		return TrackDocument
	}

	// Count speaker-like lines and markdown features
	speakerLines := 0
	headingLines := 0
	codeBlockLines := 0
	inCodeBlock := false

	for _, line := range lines {
		trimLine := strings.TrimSpace(line)

		// Track code blocks
		if strings.HasPrefix(trimLine, "```") {
			inCodeBlock = !inCodeBlock
			codeBlockLines++
			continue
		}
		if inCodeBlock {
			codeBlockLines++
			continue
		}

		// Markdown headings
		if strings.HasPrefix(trimLine, "#") {
			headingLines++
			continue
		}

		// Speaker patterns
		if speakerRe.MatchString(line) {
			speakerLines++
		}
	}

	// Rule 3: >30% of lines look like Q&A / speaker turns → extraction
	if totalLines > 3 && float64(speakerLines)/float64(totalLines) > 0.30 {
		return TrackExtraction
	}

	// Rule 4: >5 speaker turns → extraction
	if speakerLines > 5 {
		return TrackExtraction
	}

	// Rule 5: Markdown heading hierarchy or code blocks → document
	if headingLines > 0 || codeBlockLines > 0 {
		return TrackDocument
	}

	// Rule 6: Default → document (safer, preserves original)
	return TrackDocument
}
