package chunker

import (
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	targetChunkTokens = 512
	minChunkTokens    = 20
)

type ChunkResult struct {
	Content      string
	HeadingChain string
	Index        int
	TokenCount   int
}

// ChunkMarkdown splits markdown content into semantic chunks by heading hierarchy.
// Each chunk retains its heading chain for context.
func ChunkMarkdown(content string) []ChunkResult {
	lines := strings.Split(content, "\n")
	var chunks []ChunkResult
	var currentHeadings []string
	var currentContent strings.Builder
	chunkIndex := 0

	flush := func() {
		text := strings.TrimSpace(currentContent.String())
		if text == "" {
			return
		}
		chain := strings.Join(currentHeadings, " > ")
		tokens := estimateTokens(text)

		if tokens > targetChunkTokens {
			for _, p := range splitOversizedText(text, targetChunkTokens) {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				chunks = append(chunks, ChunkResult{
					Content:      p,
					HeadingChain: chain,
					Index:        chunkIndex,
					TokenCount:   estimateTokens(p),
				})
				chunkIndex++
			}
		} else if tokens >= minChunkTokens {
			chunks = append(chunks, ChunkResult{
				Content:      text,
				HeadingChain: chain,
				Index:        chunkIndex,
				TokenCount:   tokens,
			})
			chunkIndex++
		}
		currentContent.Reset()
	}

	inFence := false
	fenceMarker := ""
	for _, line := range lines {
		if marker, ok := codeFenceMarker(line); ok {
			if !inFence {
				inFence = true
				fenceMarker = marker
			} else if marker == fenceMarker {
				inFence = false
				fenceMarker = ""
			}
			currentContent.WriteString(line)
			currentContent.WriteString("\n")
			continue
		}

		if !inFence && isMarkdownHeading(line) {
			flush()
			level := headingLevel(strings.TrimLeft(line, " \t"))
			heading := strings.TrimSpace(strings.TrimLeft(line, " \t#"))

			if level <= len(currentHeadings) {
				currentHeadings = currentHeadings[:level-1]
			}
			currentHeadings = append(currentHeadings, heading)
			continue
		}

		currentContent.WriteString(line)
		currentContent.WriteString("\n")
	}
	flush()

	if len(chunks) == 0 && strings.TrimSpace(content) != "" {
		trimmed := strings.TrimSpace(content)
		for _, p := range splitOversizedText(trimmed, targetChunkTokens) {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			chunks = append(chunks, ChunkResult{
				Content:    p,
				Index:      chunkIndex,
				TokenCount: estimateTokens(p),
			})
			chunkIndex++
		}
	}

	return chunks
}

func isMarkdownHeading(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(trimmed, "#") {
		return false
	}
	level := headingLevel(trimmed)
	if level == 0 || level > 6 || level >= len(trimmed) {
		return false
	}
	return trimmed[level] == ' ' || trimmed[level] == '\t'
}

func headingLevel(line string) int {
	level := 0
	for _, c := range line {
		if c == '#' {
			level++
		} else {
			break
		}
	}
	return level
}

func codeFenceMarker(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if strings.HasPrefix(trimmed, "```") {
		return "```", true
	}
	if strings.HasPrefix(trimmed, "~~~") {
		return "~~~", true
	}
	return "", false
}

func splitOversizedText(text string, maxTokens int) []string {
	var out []string
	for _, paragraph := range splitByParagraphs(text) {
		paragraph = strings.TrimSpace(paragraph)
		if paragraph == "" {
			continue
		}
		if estimateTokens(paragraph) <= maxTokens {
			out = append(out, paragraph)
			continue
		}
		out = append(out, splitOversizedParagraph(paragraph, maxTokens)...)
	}
	return out
}

func splitByParagraphs(text string) []string {
	return strings.Split(text, "\n\n")
}

func splitOversizedParagraph(text string, maxTokens int) []string {
	sentences := splitBySentences(text)
	var out []string
	var current strings.Builder
	flush := func() {
		part := strings.TrimSpace(current.String())
		if part != "" {
			out = append(out, part)
		}
		current.Reset()
	}

	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}
		if estimateTokens(sentence) > maxTokens {
			flush()
			out = append(out, splitByTokenEstimate(sentence, maxTokens)...)
			continue
		}
		next := sentence
		if current.Len() > 0 {
			next = strings.TrimSpace(current.String()) + " " + sentence
		}
		if estimateTokens(next) > maxTokens {
			flush()
		}
		if current.Len() > 0 {
			current.WriteString(" ")
		}
		current.WriteString(sentence)
	}
	flush()
	return out
}

func splitBySentences(text string) []string {
	var out []string
	var current strings.Builder
	for _, r := range text {
		current.WriteRune(r)
		switch r {
		case '.', '!', '?', '。', '！', '？', '\n':
			part := strings.TrimSpace(current.String())
			if part != "" {
				out = append(out, part)
			}
			current.Reset()
		}
	}
	if part := strings.TrimSpace(current.String()); part != "" {
		out = append(out, part)
	}
	return out
}

func splitByTokenEstimate(text string, maxTokens int) []string {
	if maxTokens <= 0 {
		maxTokens = targetChunkTokens
	}
	var out []string
	var current strings.Builder
	for _, r := range text {
		current.WriteRune(r)
		if estimateTokens(current.String()) >= maxTokens {
			part := strings.TrimSpace(current.String())
			if part != "" {
				out = append(out, part)
			}
			current.Reset()
		}
	}
	if part := strings.TrimSpace(current.String()); part != "" {
		out = append(out, part)
	}
	return out
}

// estimateTokens gives a rough multilingual token count for chunk sizing.
func estimateTokens(s string) int {
	var tokens float64
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			continue
		case isCJK(r):
			tokens += 1
		default:
			tokens += 0.25
		}
	}
	if tokens == 0 && utf8.RuneCountInString(s) > 0 {
		return 1
	}
	return int(math.Ceil(tokens))
}

func isCJK(r rune) bool {
	return unicode.In(r,
		unicode.Han,
		unicode.Hangul,
		unicode.Hiragana,
		unicode.Katakana,
	)
}
