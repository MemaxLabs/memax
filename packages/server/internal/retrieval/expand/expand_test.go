package expand

import (
	"testing"
)

func TestExpandReformulations(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string // expected first reformulation, or "" for none
	}{
		{"strips what is", "What is the pricing model?", "pricing model"},
		{"strips how do we", "How do we handle auth?", "handle auth"},
		{"no prefix → no reformulation", "memax email provider", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Expand(tt.query)
			if tt.want == "" {
				if len(result.Reformulations) != 0 {
					t.Errorf("expected no reformulations, got %v", result.Reformulations)
				}
			} else {
				if len(result.Reformulations) == 0 || result.Reformulations[0] != tt.want {
					t.Errorf("reformulations = %v, want [%q]", result.Reformulations, tt.want)
				}
			}
		})
	}
}

func TestCrossLingualKeywords(t *testing.T) {
	tests := []struct {
		name        string
		query       string
		wantContain []string // keywords that must be present
		wantAbsent  []string // keywords that must NOT be present
	}{
		// --- CJK → English ---
		{
			name:        "邮件 → email",
			query:       "memax 的邮件方案",
			wantContain: []string{"email", "solution"},
		},
		{
			name:        "数据库 → database",
			query:       "数据库选型",
			wantContain: []string{"database", "selection"},
		},
		{
			name:        "缓存 → cache",
			query:       "memax 用什么做缓存",
			wantContain: []string{"cache"},
		},
		{
			name:        "multiple CJK terms",
			query:       "部署架构文档",
			wantContain: []string{"deploy", "architecture", "documentation"},
		},

		// --- English → CJK ---
		{
			name:        "email → 邮件",
			query:       "email provider selection",
			wantContain: []string{"邮件"},
		},
		{
			name:        "database → 数据库",
			query:       "database migration guide",
			wantContain: []string{"数据库", "迁移"},
		},
		{
			name:        "cache → 缓存",
			query:       "cache invalidation strategy",
			wantContain: []string{"缓存"},
		},

		// --- No false positives ---
		{
			name:       "term already in query not duplicated (CJK→En)",
			query:      "email 邮件配置",
			wantAbsent: []string{"email"}, // "email" already in query
		},
		{
			name:       "term already in query not duplicated (En→CJK)",
			query:      "邮件 email config",
			wantAbsent: []string{"邮件"}, // "邮件" already in query
		},
		{
			name:        "no CJK or mapped English → no keywords",
			query:       "hello world",
			wantContain: nil,
		},
		{
			name:       "partial English match rejected (testing ≠ test)",
			query:      "testing framework",
			wantAbsent: []string{"测试"}, // "testing" should not match "test"
		},

		// --- Bidirectional in same query ---
		{
			name:        "mixed query gets both directions",
			query:       "部署 config for server",
			wantContain: []string{"deploy", "配置", "服务器"},
		},

		// --- Cap enforcement + determinism ---
		{
			name:  "capped at 5 terms",
			query: "邮件数据库缓存部署服务器队列监控日志",
			// 8 CJK terms → 8 English translations, but capped at 5.
			// Output order must match zhToEnPairs definition order.
			wantContain: []string{"email", "database", "cache", "deploy", "server"},
			wantAbsent:  []string{"queue", "monitoring", "logging"},
		},

		// --- Edge cases ---
		{
			name:        "empty query",
			query:       "",
			wantContain: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Expand(tt.query)
			keywords := result.Keywords

			for _, want := range tt.wantContain {
				found := false
				for _, kw := range keywords {
					if kw == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("keywords %v missing expected term %q", keywords, want)
				}
			}

			for _, absent := range tt.wantAbsent {
				for _, kw := range keywords {
					if kw == absent {
						t.Errorf("keywords %v should not contain %q", keywords, absent)
						break
					}
				}
			}

			if len(keywords) > maxCrossLingualTerms {
				t.Errorf("keywords length %d exceeds cap %d: %v", len(keywords), maxCrossLingualTerms, keywords)
			}
		})
	}
}

func TestLatinWords(t *testing.T) {
	tests := []struct {
		query string
		want  []string
	}{
		{"email provider", []string{"email", "provider"}},
		{"memax的邮件方案", []string{"memax"}},
		{"a b email", []string{"email"}}, // single chars skipped
		{"deploy-config", []string{"deploy-config"}},
		{"", nil},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got := latinWords(tt.query)
			if len(got) != len(tt.want) {
				t.Fatalf("latinWords(%q) = %v, want %v", tt.query, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("word[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
