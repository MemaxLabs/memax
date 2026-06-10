package categorize

import (
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/model"
)

func TestSanitizeClassifyResultNormalizesFieldsIndependently(t *testing.T) {
	result := sanitizeClassifyResult(&ClassifyResult{
		Kind:      " RATIONALE ",
		Stability: "forever",
		Tags:      []string{" Postgres ", "", "TRADEOFF"},
		TopicID:   " topic-1 ",
	}, "ADR")

	if result.Kind != model.MemoryKindRationale {
		t.Fatalf("kind = %q, want %q", result.Kind, model.MemoryKindRationale)
	}
	if result.Stability != model.MemoryStabilityEvolving {
		t.Fatalf("stability = %q, want %q", result.Stability, model.MemoryStabilityEvolving)
	}
	if got, want := result.Tags, []string{"postgres", "tradeoff"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("tags = %#v, want %#v", got, want)
	}
	if result.TopicID != "topic-1" {
		t.Fatalf("topic_id = %q, want topic-1", result.TopicID)
	}
}

func TestSanitizeClassifyResultDefaultsInvalidKindWithoutLosingStability(t *testing.T) {
	result := sanitizeClassifyResult(&ClassifyResult{
		Kind:      "notes",
		Stability: "stable",
	}, "Reference")

	if result.Kind != model.MemoryKindSemantic {
		t.Fatalf("kind = %q, want %q", result.Kind, model.MemoryKindSemantic)
	}
	if result.Stability != model.MemoryStabilityStable {
		t.Fatalf("stability = %q, want %q", result.Stability, model.MemoryStabilityStable)
	}
}
