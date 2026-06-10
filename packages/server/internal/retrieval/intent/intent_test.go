package intent

import "testing"

func TestClassify(t *testing.T) {
	tests := []struct {
		name              string
		query             string
		wantIntent        IntentType
		wantRecency       string
		wantHintsContains string // at least one kind hint should contain this
	}{
		{name: "how_to", query: "how do I deploy to fly.io", wantIntent: HowTo, wantRecency: "anytime", wantHintsContains: "procedural"},
		{name: "how_to_should", query: "how should I structure the project", wantIntent: HowTo, wantRecency: "anytime"},
		{name: "why", query: "why did we choose PostgreSQL", wantIntent: Why, wantRecency: "anytime", wantHintsContains: "rationale"},
		{name: "why_cant", query: "why can't I access the database", wantIntent: Why, wantRecency: "anytime", wantHintsContains: "semantic"},
		{name: "what_is", query: "what is the memory model", wantIntent: WhatIs, wantRecency: "anytime", wantHintsContains: "semantic"},
		{name: "where", query: "where is the config file", wantIntent: Where, wantRecency: "anytime", wantHintsContains: "semantic"},
		{name: "debug_error", query: "getting a timeout error in production", wantIntent: Debug, wantRecency: "recent"},
		{name: "debug_crash", query: "the server keeps crashing on startup", wantIntent: Debug, wantRecency: "recent"},
		{name: "debug_500", query: "users seeing 500 responses", wantIntent: Debug, wantRecency: "recent"},
		{name: "reference_config", query: "database connection config", wantIntent: Reference, wantRecency: "anytime"},
		{name: "reference_env", query: "what env variable for the API key", wantIntent: Reference, wantRecency: "anytime"},
		{name: "comparison_vs", query: "memax vs mem0", wantIntent: Why, wantRecency: "anytime", wantHintsContains: "rationale"},
		{name: "comparison_versus", query: "redis versus memcached", wantIntent: Why, wantRecency: "anytime", wantHintsContains: "rationale"},
		{name: "comparison_对比", query: "memax 对比 mem0", wantIntent: Why, wantRecency: "anytime", wantHintsContains: "rationale"},
		{name: "comparison_比较", query: "memax 比较 mem0", wantIntent: Why, wantRecency: "anytime", wantHintsContains: "rationale"},
		{name: "comparison_竞品", query: "memax 竞品分析", wantIntent: Why, wantRecency: "anytime", wantHintsContains: "rationale"},
		{name: "decision_provider", query: "email provider in the memax hub", wantIntent: Why, wantRecency: "anytime", wantHintsContains: "rationale"},
		{name: "decision_选型", query: "memax 用什么 email 选型？", wantIntent: Why, wantRecency: "anytime", wantHintsContains: "rationale"},
		{name: "decision_选型_embedded", query: "向量数据库选型", wantIntent: Why, wantRecency: "anytime", wantHintsContains: "rationale"},
		{name: "decision_what_use", query: "what database do we use", wantIntent: Why, wantRecency: "anytime", wantHintsContains: "rationale"},
		{name: "decision_which_chose", query: "which queue system did we choose", wantIntent: Why, wantRecency: "anytime", wantHintsContains: "rationale"},
		{name: "decision_用什么", query: "memax 用什么做缓存", wantIntent: Why, wantRecency: "anytime", wantHintsContains: "rationale"},
		{name: "decision_cloud_provider", query: "cloud provider for deployment", wantIntent: Why, wantRecency: "anytime", wantHintsContains: "rationale"},
		{name: "decision_vendor", query: "which vendor did we choose for payroll", wantIntent: Why, wantRecency: "anytime", wantHintsContains: "rationale"},
		{name: "decision_platform", query: "what platform are we using for analytics", wantIntent: Why, wantRecency: "anytime", wantHintsContains: "rationale"},
		{name: "decision_agency", query: "the design agency we hired", wantIntent: Why, wantRecency: "anytime", wantHintsContains: "rationale"},
		{name: "debug_provider_error", query: "the provider API returned 500 error", wantIntent: Debug, wantRecency: "recent"},
		{name: "debug_platform_outage", query: "platform outage last night", wantIntent: Debug, wantRecency: "recent"},
		{name: "debug_vendor_incident", query: "vendor incident report from Monday", wantIntent: Debug, wantRecency: "recent"},
		{name: "debug_agency_downtime", query: "agency platform downtime", wantIntent: Debug, wantRecency: "recent"},
		{name: "general", query: "tell me about the project", wantIntent: General, wantRecency: "anytime"},
		{name: "general_empty_ish", query: "hello", wantIntent: General, wantRecency: "anytime"},
		{name: "case_insensitive", query: "How Do I run tests", wantIntent: HowTo, wantRecency: "anytime"},
		{name: "whitespace", query: "  how to build  ", wantIntent: HowTo, wantRecency: "anytime"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Classify(tt.query)
			if result.Intent != tt.wantIntent {
				t.Errorf("Classify(%q).Intent = %q, want %q", tt.query, result.Intent, tt.wantIntent)
			}
			if result.RecencyPreference != tt.wantRecency {
				t.Errorf("Classify(%q).RecencyPreference = %q, want %q", tt.query, result.RecencyPreference, tt.wantRecency)
			}
			if tt.wantHintsContains != "" {
				found := false
				for _, h := range result.KindHints {
					if h == tt.wantHintsContains {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Classify(%q).KindHints = %v, want to contain %q", tt.query, result.KindHints, tt.wantHintsContains)
				}
			}
		})
	}
}

func TestFromDistillerNeed(t *testing.T) {
	tests := []struct {
		need string
		want IntentType
	}{
		{"debugging", Debug},
		{"reference", Reference},
		{"explanation", WhatIs},
		{"procedure", HowTo},
		{"decision_rationale", Why},
		{"general", General},
		{"unknown_value", General},
		{"", General},
	}
	for _, tt := range tests {
		t.Run(tt.need, func(t *testing.T) {
			got := FromDistillerNeed(tt.need)
			if got != tt.want {
				t.Errorf("FromDistillerNeed(%q) = %q, want %q", tt.need, got, tt.want)
			}
		})
	}
}
