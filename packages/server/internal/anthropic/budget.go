package anthropic

import (
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"
)

const (
	defaultContextWindow   = 200000
	sonnetContextWindow    = 1000000
	fallbackContextWindow  = 64000
	defaultSafetyMargin    = 2048
	truncationMarker       = "\n\n[truncated]"
	middleTruncationMarker = "\n\n[truncated middle]\n\n"
	documentTokenReserve   = 32768
	imageTokenReserve      = 8192
	genericBlockReserve    = 4096
)

type ModelSpec struct {
	Name          string
	ContextWindow int
	SafetyMargin  int
}

type PromptBudget struct {
	Model             string
	ContextWindow     int
	SafetyMargin      int
	MaxOutputTokens   int
	InputBudgetTokens int
	InputTokens       int
	SystemTokens      int
	PromptTokens      int
	TrimmedSystem     bool
	TrimmedPrompt     bool
}

type MessageBudget struct {
	Model                 string
	ContextWindow         int
	SafetyMargin          int
	MaxOutputTokens       int
	InputBudgetTokens     int
	TextBudgetTokens      int
	TextTokens            int
	ReservedNonTextTokens int
	TrimmedTextBlocks     int
	TextBlockCount        int
	MessageCount          int
	DocumentBlockCount    int
	ImageBlockCount       int
	OtherBlockCount       int
	ApproxPayloadBytes    int
}

func ResolveModelSpec(model string) ModelSpec {
	name := strings.TrimSpace(model)
	if name == "" {
		name = DefaultModel
	}
	lower := strings.ToLower(name)
	spec := ModelSpec{
		Name:          name,
		ContextWindow: fallbackContextWindow,
		SafetyMargin:  defaultSafetyMargin,
	}
	switch {
	case strings.Contains(lower, "claude-sonnet-4-6"):
		spec.ContextWindow = sonnetContextWindow
	case strings.Contains(lower, "claude-haiku"),
		strings.Contains(lower, "claude-sonnet"),
		strings.Contains(lower, "claude-opus"),
		strings.Contains(lower, "anthropic/claude"):
		spec.ContextWindow = defaultContextWindow
	}
	return spec
}

func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	runeCount := utf8.RuneCountInString(text)
	if runeCount == 0 {
		return 0
	}
	ascii := 0
	nonASCII := 0
	dense := 0
	whitespace := 0
	for _, r := range text {
		switch {
		case r <= 127:
			ascii++
			if strings.ContainsRune("{}[]()<>_=:/\\|-`*#", r) {
				dense++
			}
			if r == ' ' || r == '\n' || r == '\t' {
				whitespace++
			}
		default:
			nonASCII++
		}
	}
	asciiEstimate := ascii / 4
	if dense > runeCount/8 {
		asciiEstimate = ascii / 3
	}
	if whitespace > runeCount/3 {
		asciiEstimate = maxInt(1, asciiEstimate-whitespace/20)
	}
	return maxInt(1, asciiEstimate+nonASCII)
}

func FitCompleteRequest(req CompleteRequest) (CompleteRequest, PromptBudget) {
	if req.Model == "" {
		req.Model = DefaultModel
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = 200
	}

	spec := ResolveModelSpec(req.Model)
	inputBudget := spec.ContextWindow - req.MaxTokens - spec.SafetyMargin
	if inputBudget < 1024 {
		inputBudget = 1024
	}

	budget := PromptBudget{
		Model:             req.Model,
		ContextWindow:     spec.ContextWindow,
		SafetyMargin:      spec.SafetyMargin,
		MaxOutputTokens:   req.MaxTokens,
		InputBudgetTokens: inputBudget,
		SystemTokens:      EstimateTokens(req.System),
		PromptTokens:      EstimateTokens(req.Prompt),
	}
	budget.InputTokens = budget.SystemTokens + budget.PromptTokens
	if budget.InputTokens <= budget.InputBudgetTokens {
		return req, budget
	}

	maxSystemTokens := maxInt(256, budget.InputBudgetTokens/5)
	if budget.SystemTokens > maxSystemTokens {
		req.System = truncateToTokens(req.System, maxSystemTokens)
		budget.TrimmedSystem = true
		budget.SystemTokens = EstimateTokens(req.System)
	}

	availablePrompt := budget.InputBudgetTokens - budget.SystemTokens
	if availablePrompt < 256 {
		targetSystem := minInt(budget.SystemTokens, maxInt(128, budget.InputBudgetTokens/8))
		if targetSystem < budget.SystemTokens {
			req.System = truncateToTokens(req.System, targetSystem)
			budget.TrimmedSystem = true
			budget.SystemTokens = EstimateTokens(req.System)
		}
		availablePrompt = budget.InputBudgetTokens - budget.SystemTokens
	}
	if availablePrompt < 128 {
		availablePrompt = 128
	}
	if budget.PromptTokens > availablePrompt {
		req.Prompt = truncateToTokens(req.Prompt, availablePrompt)
		budget.TrimmedPrompt = true
		budget.PromptTokens = EstimateTokens(req.Prompt)
	}
	budget.InputTokens = budget.SystemTokens + budget.PromptTokens
	logPromptBudget(budget)
	return req, budget
}

func FitMessages(model string, maxTokens int, messages []map[string]any) ([]map[string]any, MessageBudget) {
	if model == "" {
		model = DefaultModel
	}
	if maxTokens <= 0 {
		maxTokens = 200
	}

	spec := ResolveModelSpec(model)
	inputBudget := spec.ContextWindow - maxTokens - spec.SafetyMargin
	if inputBudget < 1024 {
		inputBudget = 1024
	}

	cloned := cloneMessages(messages)
	budget := MessageBudget{
		Model:             model,
		ContextWindow:     spec.ContextWindow,
		SafetyMargin:      spec.SafetyMargin,
		MaxOutputTokens:   maxTokens,
		InputBudgetTokens: inputBudget,
		MessageCount:      len(cloned),
	}

	textLocations := make([][]int, 0)
	textContents := make([]string, 0)

	for msgIdx, msg := range cloned {
		content, ok := msg["content"]
		if !ok {
			continue
		}
		switch typed := content.(type) {
		case string:
			budget.TextBlockCount++
			text := typed
			budget.TextTokens += EstimateTokens(text)
			textLocations = append(textLocations, []int{msgIdx, -1})
			textContents = append(textContents, text)
		case []map[string]any:
			for blockIdx, block := range typed {
				budget.collectBlockMetrics(block)
				if text, ok := block["text"].(string); ok && strings.EqualFold(stringValue(block["type"]), "text") {
					budget.TextBlockCount++
					budget.TextTokens += EstimateTokens(text)
					textLocations = append(textLocations, []int{msgIdx, blockIdx})
					textContents = append(textContents, text)
				}
			}
		case []any:
			for blockIdx, raw := range typed {
				block, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				budget.collectBlockMetrics(block)
				if text, ok := block["text"].(string); ok && strings.EqualFold(stringValue(block["type"]), "text") {
					budget.TextBlockCount++
					budget.TextTokens += EstimateTokens(text)
					textLocations = append(textLocations, []int{msgIdx, blockIdx})
					textContents = append(textContents, text)
				}
			}
		}
	}

	budget.TextBudgetTokens = inputBudget - budget.ReservedNonTextTokens
	if budget.TextBudgetTokens < 256 {
		budget.TextBudgetTokens = 256
	}
	if budget.TextTokens <= budget.TextBudgetTokens {
		return cloned, budget
	}

	remainingBudget := budget.TextBudgetTokens
	remainingBlocks := len(textContents)
	for i, text := range textContents {
		if remainingBlocks <= 0 {
			break
		}
		estimated := EstimateTokens(text)
		allocation := remainingBudget / remainingBlocks
		if allocation < 96 {
			allocation = 96
		}
		if estimated > allocation {
			text = TruncateMiddleToTokens(text, allocation)
			budget.TrimmedTextBlocks++
		}
		loc := textLocations[i]
		cloned = setMessageText(cloned, loc[0], loc[1], text)
		used := EstimateTokens(text)
		if used > remainingBudget {
			used = remainingBudget
		}
		remainingBudget -= used
		remainingBlocks--
	}

	budget.TextTokens = recomputeMessageTextTokens(cloned)
	logMessageBudget(budget)
	return cloned, budget
}

func truncateToTokens(text string, maxTokens int) string {
	if maxTokens <= 0 || text == "" {
		return ""
	}
	if EstimateTokens(text) <= maxTokens {
		return text
	}
	markerTokens := EstimateTokens(truncationMarker)
	target := maxTokens - markerTokens
	if target <= 32 {
		target = maxTokens
	}
	runes := []rune(text)
	low, high := 0, len(runes)
	best := ""
	for low <= high {
		mid := (low + high) / 2
		candidate := string(runes[:mid])
		if EstimateTokens(candidate) <= target {
			best = candidate
			low = mid + 1
		} else {
			high = mid - 1
		}
	}
	best = strings.TrimSpace(best)
	if best == "" {
		best = strings.TrimSpace(string(runes[:minInt(len(runes), 64)]))
	}
	if maxTokens > markerTokens && best != text {
		return best + truncationMarker
	}
	return best
}

func TruncateMiddleToTokens(text string, maxTokens int) string {
	if maxTokens <= 0 || text == "" {
		return ""
	}
	if EstimateTokens(text) <= maxTokens {
		return text
	}
	runes := []rune(text)
	markerTokens := EstimateTokens(middleTruncationMarker)
	halfBudget := (maxTokens - markerTokens) / 2
	if halfBudget < 32 {
		return truncateToTokens(text, maxTokens)
	}
	head := truncateToTokens(string(runes), halfBudget)
	tail := truncateTailToTokens(string(runes), halfBudget)
	head = strings.TrimSuffix(head, truncationMarker)
	tail = strings.TrimPrefix(tail, truncationMarker)
	return strings.TrimSpace(head) + middleTruncationMarker + strings.TrimSpace(tail)
}

func HasTruncationMarker(text string) bool {
	if text == "" {
		return false
	}
	return strings.Contains(text, truncationMarker) || strings.Contains(text, middleTruncationMarker)
}

func truncateTailToTokens(text string, maxTokens int) string {
	if maxTokens <= 0 || text == "" {
		return ""
	}
	if EstimateTokens(text) <= maxTokens {
		return text
	}
	runes := []rune(text)
	low, high := 0, len(runes)
	best := ""
	for low <= high {
		mid := (low + high) / 2
		candidate := string(runes[len(runes)-mid:])
		if EstimateTokens(candidate) <= maxTokens-EstimateTokens(truncationMarker) {
			best = candidate
			low = mid + 1
		} else {
			high = mid - 1
		}
	}
	best = strings.TrimSpace(best)
	if best == "" {
		best = strings.TrimSpace(string(runes[maxInt(0, len(runes)-64):]))
	}
	return truncationMarker + best
}

func FitQuotedSection(label string, content string, maxTokens int) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	overhead := EstimateTokens(label) + 16
	bodyBudget := maxTokens - overhead
	if bodyBudget < 64 {
		bodyBudget = 64
	}
	return truncateToTokens(content, bodyBudget)
}

func logPromptBudget(b PromptBudget) {
	if !b.TrimmedPrompt && !b.TrimmedSystem {
		return
	}
	slog.Warn(
		"anthropic prompt trimmed to fit model context",
		"model", b.Model,
		"context_window", b.ContextWindow,
		"input_budget_tokens", b.InputBudgetTokens,
		"input_tokens", b.InputTokens,
		"system_tokens", b.SystemTokens,
		"prompt_tokens", b.PromptTokens,
		"trimmed_system", b.TrimmedSystem,
		"trimmed_prompt", b.TrimmedPrompt,
	)
}

func logMessageBudget(b MessageBudget) {
	if b.TrimmedTextBlocks == 0 {
		return
	}
	slog.Warn(
		"anthropic multimodal prompt trimmed to fit model context",
		"model", b.Model,
		"context_window", b.ContextWindow,
		"input_budget_tokens", b.InputBudgetTokens,
		"text_budget_tokens", b.TextBudgetTokens,
		"text_tokens", b.TextTokens,
		"reserved_non_text_tokens", b.ReservedNonTextTokens,
		"text_block_count", b.TextBlockCount,
		"trimmed_text_blocks", b.TrimmedTextBlocks,
		"document_block_count", b.DocumentBlockCount,
		"image_block_count", b.ImageBlockCount,
		"other_block_count", b.OtherBlockCount,
		"approx_payload_bytes", b.ApproxPayloadBytes,
	)
}

func BudgetError(model string, maxOutput int, prompt string, system string) error {
	req := CompleteRequest{Model: model, MaxTokens: maxOutput, Prompt: prompt, System: system}
	_, budget := FitCompleteRequest(req)
	if budget.InputTokens <= budget.InputBudgetTokens {
		return nil
	}
	return fmt.Errorf("prompt exceeds model budget: input=%d budget=%d", budget.InputTokens, budget.InputBudgetTokens)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (b *MessageBudget) collectBlockMetrics(block map[string]any) {
	blockType := strings.ToLower(stringValue(block["type"]))
	switch blockType {
	case "document":
		b.DocumentBlockCount++
		b.ReservedNonTextTokens += documentTokenReserve
		b.ApproxPayloadBytes += estimateSourceBytes(block["source"])
	case "image":
		b.ImageBlockCount++
		b.ReservedNonTextTokens += imageTokenReserve
		b.ApproxPayloadBytes += estimateSourceBytes(block["source"])
	case "text":
		// handled separately
	default:
		b.OtherBlockCount++
		b.ReservedNonTextTokens += genericBlockReserve
	}
}

func estimateSourceBytes(source any) int {
	src, ok := source.(map[string]any)
	if !ok {
		return 0
	}
	data, _ := src["data"].(string)
	if data == "" {
		return 0
	}
	return len(data) * 3 / 4
}

func cloneMessages(messages []map[string]any) []map[string]any {
	if len(messages) == 0 {
		return nil
	}
	cloned := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		cloned = append(cloned, cloneMap(msg))
	}
	return cloned
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	out := make(map[string]any, len(input))
	for k, v := range input {
		out[k] = cloneValue(v)
	}
	return out
}

func cloneSlice(input []any) []any {
	if input == nil {
		return nil
	}
	out := make([]any, len(input))
	for i, v := range input {
		out[i] = cloneValue(v)
	}
	return out
}

func cloneValue(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		return cloneSlice(typed)
	case []map[string]any:
		out := make([]map[string]any, len(typed))
		for i, item := range typed {
			out[i] = cloneMap(item)
		}
		return out
	default:
		return v
	}
}

func setMessageText(messages []map[string]any, msgIdx int, blockIdx int, text string) []map[string]any {
	if msgIdx < 0 || msgIdx >= len(messages) {
		return messages
	}
	msg := messages[msgIdx]
	if blockIdx < 0 {
		msg["content"] = text
		return messages
	}
	switch content := msg["content"].(type) {
	case []any:
		if blockIdx >= len(content) {
			return messages
		}
		block, ok := content[blockIdx].(map[string]any)
		if !ok {
			return messages
		}
		block["text"] = text
	case []map[string]any:
		if blockIdx >= len(content) {
			return messages
		}
		content[blockIdx]["text"] = text
		msg["content"] = content
	}
	return messages
}

func recomputeMessageTextTokens(messages []map[string]any) int {
	total := 0
	for _, msg := range messages {
		switch content := msg["content"].(type) {
		case string:
			total += EstimateTokens(content)
		case []any:
			for _, raw := range content {
				block, ok := raw.(map[string]any)
				if !ok || !strings.EqualFold(stringValue(block["type"]), "text") {
					continue
				}
				total += EstimateTokens(stringValue(block["text"]))
			}
		case []map[string]any:
			for _, block := range content {
				if !strings.EqualFold(stringValue(block["type"]), "text") {
					continue
				}
				total += EstimateTokens(stringValue(block["text"]))
			}
		}
	}
	return total
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}
