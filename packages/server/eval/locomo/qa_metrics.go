package locomo

import (
	"math"
	"strings"
	"unicode"
)

// QAMetrics are LoCoMo answer quality metrics reported by the Mem0 paper.
type QAMetrics struct {
	F1    float64 `json:"f1"`
	BLEU1 float64 `json:"bleu1"`
	Judge float64 `json:"judge"`
}

// EvaluateAnswer computes lightweight lexical metrics for one answer.
func EvaluateAnswer(prediction, gold string, category int) QAMetrics {
	return QAMetrics{
		F1:    f1(prediction, gold, category),
		BLEU1: bleu1(prediction, gold),
	}
}

func f1(prediction, gold string, category int) float64 {
	if category == 1 {
		return multiAnswerF1(prediction, gold)
	}
	return tokenF1(prediction, gold)
}

func multiAnswerF1(prediction, gold string) float64 {
	preds := splitAnswers(prediction)
	golds := splitAnswers(gold)
	if len(preds) == 0 || len(golds) == 0 {
		return 0
	}
	sum := 0.0
	for _, g := range golds {
		best := 0.0
		for _, p := range preds {
			best = math.Max(best, tokenF1(p, g))
		}
		sum += best
	}
	return sum / float64(len(golds))
}

func tokenF1(prediction, gold string) float64 {
	predTokens := normalizeTokens(prediction)
	goldTokens := normalizeTokens(gold)
	if len(predTokens) == 0 || len(goldTokens) == 0 {
		return 0
	}
	common := tokenOverlap(predTokens, goldTokens)
	if common == 0 {
		return 0
	}
	precision := float64(common) / float64(len(predTokens))
	recall := float64(common) / float64(len(goldTokens))
	return 2 * precision * recall / (precision + recall)
}

func bleu1(prediction, gold string) float64 {
	predTokens := normalizeTokens(prediction)
	goldTokens := normalizeTokens(gold)
	if len(predTokens) == 0 || len(goldTokens) == 0 {
		return 0
	}
	precision := float64(tokenOverlap(predTokens, goldTokens)) / float64(len(predTokens))
	brevityPenalty := 1.0
	if len(predTokens) < len(goldTokens) {
		brevityPenalty = math.Exp(1 - float64(len(goldTokens))/float64(len(predTokens)))
	}
	return brevityPenalty * precision
}

func tokenOverlap(a, b []string) int {
	counts := make(map[string]int, len(a))
	for _, tok := range a {
		counts[tok]++
	}
	common := 0
	for _, tok := range b {
		if counts[tok] > 0 {
			common++
			counts[tok]--
		}
	}
	return common
}

func normalizeTokens(s string) []string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	words := strings.Fields(b.String())
	out := make([]string, 0, len(words))
	for _, word := range words {
		if word == "a" || word == "an" || word == "the" || word == "and" {
			continue
		}
		out = append(out, stemToken(word))
	}
	return out
}

func splitAnswers(s string) []string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ';'
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 && strings.TrimSpace(s) != "" {
		return []string{strings.TrimSpace(s)}
	}
	return out
}

func stemToken(s string) string {
	for _, suffix := range []string{"ing", "ed", "es", "s"} {
		if len(s) > len(suffix)+2 && strings.HasSuffix(s, suffix) {
			return strings.TrimSuffix(s, suffix)
		}
	}
	return s
}
