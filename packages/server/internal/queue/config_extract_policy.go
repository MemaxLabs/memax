package queue

import (
	"strings"

	ingesttitle "github.com/MemaxLabs/memax/packages/server/internal/ingest/title"
	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

// knowledgeItem is a discrete piece of knowledge extracted from a config file.
type knowledgeItem struct {
	title   string
	content string
	tags    []string
}

type configExtractionMode string

const (
	configExtractNever     configExtractionMode = "never"
	configExtractSelective configExtractionMode = "selective"
	configExtractAlways    configExtractionMode = "always"
)

type extractionDecision struct {
	mode   configExtractionMode
	reason string
}

// extractKnowledgeItems parses agent config content into discrete knowledge items.
// Extraction is intentionally selective: memory-like config files are always
// considered, while general instruction files must pass stronger heuristics so we
// only create durable, recall-worthy memories.
func extractKnowledgeItems(content, agent, filePath string) []knowledgeItem {
	decision := decideConfigExtraction(content, agent, filePath)
	if decision.mode == configExtractNever {
		return nil
	}

	lines := strings.Split(content, "\n")
	var items []knowledgeItem
	var currentHeading string
	var currentBlock []string

	flush := func() {
		if len(currentBlock) == 0 {
			return
		}
		text := strings.TrimSpace(strings.Join(currentBlock, "\n"))
		if !shouldKeepKnowledgeCandidate(text, currentHeading, decision.mode) {
			currentBlock = nil
			return
		}
		title := currentHeading
		if title == "" {
			title = ingesttitle.GenerateFromContent(text)
		}
		items = append(items, knowledgeItem{
			title:   title,
			content: text,
			tags:    []string{agent, filePath},
		})
		currentBlock = nil
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip YAML frontmatter
		if trimmed == "---" {
			continue
		}

		// New heading = new item boundary
		if strings.HasPrefix(trimmed, "## ") || strings.HasPrefix(trimmed, "# ") {
			flush()
			currentHeading = strings.TrimLeft(trimmed, "# ")
			continue
		}

		// Skip empty lines between items
		if trimmed == "" && len(currentBlock) == 0 {
			continue
		}

		currentBlock = append(currentBlock, line)
	}
	flush()

	return items
}

func decideConfigExtraction(content, agent, filePath string) extractionDecision {
	mode := classifyConfigExtractionMode(agent, filePath)
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return extractionDecision{mode: configExtractNever, reason: "empty"}
	}

	nonEmptyLines := 0
	for _, line := range strings.Split(trimmed, "\n") {
		if strings.TrimSpace(line) != "" {
			nonEmptyLines++
		}
	}

	if mode == configExtractAlways {
		if len(trimmed) < 40 || nonEmptyLines < 2 {
			return extractionDecision{mode: configExtractNever, reason: "trivial_memory_file"}
		}
		if isPointerOnlyConfig(trimmed) {
			return extractionDecision{mode: configExtractNever, reason: "pointer_only"}
		}
		return extractionDecision{mode: mode, reason: "memory_surface"}
	}

	if len(trimmed) < 120 && !hasDurableKnowledgeSignals(trimmed, "") {
		return extractionDecision{mode: configExtractNever, reason: "too_short"}
	}
	if nonEmptyLines < 4 && !hasDurableKnowledgeSignals(trimmed, "") {
		return extractionDecision{mode: configExtractNever, reason: "too_few_lines"}
	}
	if isPointerOnlyConfig(trimmed) {
		return extractionDecision{mode: configExtractNever, reason: "pointer_only"}
	}
	if isMostlyPresentationInstructions(trimmed) {
		return extractionDecision{mode: configExtractNever, reason: "presentation_only"}
	}

	return extractionDecision{mode: mode, reason: "eligible"}
}

func classifyConfigExtractionMode(agent, filePath string) configExtractionMode {
	path := strings.ToLower(strings.ReplaceAll(filePath, "\\", "/"))
	switch {
	case model.IsIdentityConfigPath(path):
		// Identity files (SOUL.md, persona files) define who the agent IS —
		// personality, tone, values — not durable project knowledge. Knowledge
		// extraction skips them entirely; they still sync verbatim, and the
		// persona pipeline (personal-agent sync, phase 2) will own extracting
		// them into first-class personas.
		return configExtractNever
	case agent == "claude-code" && path == "memory.md":
		return configExtractAlways
	case agent == "claude-code" && strings.HasPrefix(path, "memory/") && strings.HasSuffix(path, ".md"):
		return configExtractAlways
	case path == "":
		return configExtractNever
	default:
		return configExtractSelective
	}
}


func shouldKeepKnowledgeCandidate(text, heading string, mode configExtractionMode) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	if isPointerOnlyConfig(trimmed) {
		return false
	}
	if mode == configExtractAlways {
		return len(trimmed) >= 40
	}
	if len(trimmed) < 60 {
		return false
	}
	if isMostlyPresentationInstructions(trimmed) {
		return false
	}
	return hasDurableKnowledgeSignals(trimmed, heading)
}

func isPointerOnlyConfig(content string) bool {
	normalized := strings.ToLower(strings.TrimSpace(content))
	if normalized == "" {
		return false
	}
	pointerPhrases := []string{
		"see ",
		"refer to ",
		"moved to ",
		"moved into ",
		"delegated to ",
		"defined in ",
		"use ",
		"follow ",
		"read ",
	}
	hasPointerPhrase := false
	for _, phrase := range pointerPhrases {
		if strings.Contains(normalized, phrase) {
			hasPointerPhrase = true
			break
		}
	}
	if !hasPointerPhrase {
		return false
	}

	lines := strings.Split(normalized, "\n")
	nonEmpty := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonEmpty++
		}
	}
	if nonEmpty > 6 || len(normalized) > 280 {
		return false
	}

	referenceSignals := []string{
		".md", "agents.md", "claude.md", "gemini.md", "memory.md",
		".cursorrules", ".windsurfrules", ".github/", ".codex/", ".claude/", ".opencode/",
	}
	for _, signal := range referenceSignals {
		if strings.Contains(normalized, signal) {
			return true
		}
	}
	return false
}

func isMostlyPresentationInstructions(content string) bool {
	normalized := strings.ToLower(content)
	presentationSignals := []string{
		"be concise",
		"be helpful",
		"tone",
		"format your response",
		"format responses",
		"bullet points",
		"markdown",
		"response style",
		"step by step",
		"do not use emojis",
		"communicate",
		"writing style",
		"personality",
		"avoid fluff",
		"explain your reasoning",
	}
	durableSignals := []string{
		"architecture",
		"deploy",
		"deployment",
		"security",
		"auth",
		"oauth",
		"testing",
		"migration",
		"database",
		"postgres",
		"redis",
		"workflow",
		"convention",
		"repository",
		"project",
		"ci",
		"build",
		"env var",
		"environment variable",
		"runbook",
	}

	presentationCount := 0
	for _, signal := range presentationSignals {
		if strings.Contains(normalized, signal) {
			presentationCount++
		}
	}
	if presentationCount == 0 {
		return false
	}

	for _, signal := range durableSignals {
		if strings.Contains(normalized, signal) {
			return false
		}
	}

	return presentationCount >= 2
}

func hasDurableKnowledgeSignals(content, heading string) bool {
	text := strings.ToLower(content + "\n" + heading)
	signals := []string{
		"architecture",
		"deploy",
		"deployment",
		"runbook",
		"workflow",
		"convention",
		"policy",
		"repository",
		"repo",
		"project",
		"testing",
		"test strategy",
		"migration",
		"postgres",
		"redis",
		"security",
		"oauth",
		"api key",
		"environment variable",
		"env var",
		"build",
		"debug",
		"troubleshoot",
		"gotcha",
		"knowledge",
		"remember",
		"memory",
	}
	for _, signal := range signals {
		if strings.Contains(text, signal) {
			return true
		}
	}

	wordSignals := []string{"auth", "repo", "project", "workflow", "policy", "convention", "migration", "testing"}
	for _, signal := range wordSignals {
		if containsWord(text, signal) {
			return true
		}
	}

	// Technical identifiers also tend to be durable and useful in recall.
	if strings.ContainsAny(content, "/_.") && strings.Contains(content, "`") {
		return true
	}
	return false
}

func containsWord(text, word string) bool {
	for _, token := range strings.FieldsFunc(text, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	}) {
		if token == word {
			return true
		}
	}
	return false
}
