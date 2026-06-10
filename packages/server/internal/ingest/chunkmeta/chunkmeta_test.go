package chunkmeta

import (
	"strings"
	"testing"
)

func TestTagsTextNormalizesStructuredTagsForLexicalSearch(t *testing.T) {
	got := TagsText([]string{"person:Ziyang", "product_ux", "product_ux", "repo/github"})

	for _, want := range []string{
		"person:ziyang",
		"person ziyang",
		"ziyang",
		"product ux",
		"repo github",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q to contain %q", got, want)
		}
	}

	if strings.Count(got, "product_ux") != 1 {
		t.Fatalf("expected duplicate tags to be deduplicated, got %q", got)
	}
}

func TestTagsTextSkipsEmptyTags(t *testing.T) {
	got := TagsText([]string{"", "  ", "\t"})
	if got != "" {
		t.Fatalf("expected empty output, got %q", got)
	}
}

func TestEmbeddingTagsSuffix(t *testing.T) {
	tests := []struct {
		name string
		tags []string
		want string
	}{
		{
			name: "basic tags",
			tags: []string{"email", "resend", "选型", "infrastructure"},
			want: "Tags: email, resend, 选型, infrastructure",
		},
		{
			name: "deduplicates and lowercases",
			tags: []string{"Email", "email", "RESEND", "resend"},
			want: "Tags: email, resend",
		},
		{
			name: "skips short tags (< 2 runes)",
			tags: []string{"a", "选", "db", "ok"},
			// "a" (1 rune) skipped, "选" (1 rune) skipped, "db" and "ok" kept
			want: "Tags: db, ok",
		},
		{
			name: "caps at 10 tags",
			tags: []string{"t1", "t2", "t3", "t4", "t5", "t6", "t7", "t8", "t9", "t10", "t11", "t12"},
			want: "Tags: t1, t2, t3, t4, t5, t6, t7, t8, t9, t10",
		},
		{
			name: "empty tags returns empty string",
			tags: nil,
			want: "",
		},
		{
			name: "only short/empty tags returns empty string",
			tags: []string{"", "a", " "},
			want: "",
		},
		{
			name: "trims whitespace",
			tags: []string{"  email  ", " auth "},
			want: "Tags: email, auth",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EmbeddingTagsSuffix(tt.tags)
			if got != tt.want {
				t.Errorf("EmbeddingTagsSuffix(%v) = %q, want %q", tt.tags, got, tt.want)
			}
		})
	}
}

func TestMetadataTextNormalizesAuthorHubAndSource(t *testing.T) {
	got := MetadataText("Jiahao Ye", "memax-test", "memax-test", "api", "claude-code", "github.com/MemaxLabs/memax-internal")

	for _, want := range []string{
		"author:jiahao ye",
		"author jiahao ye",
		"jiahao ye",
		"hub:memax-test",
		"hub memax test",
		"agent claude code",
		"repo:github.com/memaxlabs/memax",
		"repo github.com memaxlabs memax",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q to contain %q", got, want)
		}
	}
	if strings.Contains(got, "source:api") || strings.Contains(got, "source api") {
		t.Fatalf("expected low-signal source api to be omitted, got %q", got)
	}
}
