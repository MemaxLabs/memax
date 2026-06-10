package intent

import (
	"regexp"
	"strings"
	"time"
)

// IntentType represents the kind of answer the user is looking for.
type IntentType string

const (
	HowTo     IntentType = "how_to"
	Why       IntentType = "why"
	WhatIs    IntentType = "what_is"
	Where     IntentType = "where"
	Debug     IntentType = "debug"
	Reference IntentType = "reference"
	Temporal  IntentType = "temporal"
	General   IntentType = "general"
)

// Result holds the classification output for a query.
type Result struct {
	Intent            IntentType `json:"intent_type"`
	KindHints         []string   `json:"kind_hints"`
	RecencyPreference string     `json:"recency_preference"` // "recent", "anytime", "historical", "temporal"
	TemporalStart     *time.Time `json:"temporal_start,omitempty"`
	TemporalEnd       *time.Time `json:"temporal_end,omitempty"`
}

// Classify determines the intent of a query using regex fast-path patterns.
// No LLM call needed — this runs in microseconds.
func Classify(query string) *Result {
	q := strings.ToLower(strings.TrimSpace(query))

	// Temporal fast-path — check before semantic intent patterns
	if start, end, ok := extractTemporalBounds(q, time.Now()); ok {
		return &Result{
			Intent:            Temporal,
			RecencyPreference: "temporal",
			TemporalStart:     start,
			TemporalEnd:       end,
		}
	}

	switch {
	case matches(q, `^how (do|does|can|should|to) `):
		return &Result{Intent: HowTo, KindHints: []string{"procedural", "semantic"}, RecencyPreference: "anytime"}
	case matches(q, `^why (did|do|does|is|are|was|were|don't|can't|won't|isn't|aren't) `):
		return &Result{Intent: Why, KindHints: []string{"rationale", "semantic"}, RecencyPreference: "anytime"}
	case matches(q, `^what (is|are|does|was|were) `):
		return &Result{Intent: WhatIs, KindHints: []string{"semantic"}, RecencyPreference: "anytime"}
	case matches(q, `^where (is|are|does|do|can|should) `):
		return &Result{Intent: Where, KindHints: []string{"semantic"}, RecencyPreference: "anytime"}
	case matches(q, `(error|bug|fix|crash|fail|broken|issue|debug|exception|stack.?trace|panic|segfault|404|500|timeout|outage|incident|downtime)`):
		return &Result{Intent: Debug, KindHints: []string{"procedural", "episodic"}, RecencyPreference: "recent"}
	case matches(q, `(config|setting|port|endpoint|url|env|variable|flag|option)`):
		return &Result{Intent: Reference, KindHints: []string{"semantic"}, RecencyPreference: "anytime"}
	// Comparison queries — "memax vs mem0", "对比", "比较", "竞品".
	// Treated as rationale intent so decision/competitive analyses rank higher.
	// Guard: skip if query also includes incident/status words.
	case (matches(q, `\bvs\b|\bversus\b`) || strings.Contains(q, "对比") || strings.Contains(q, "比较") || strings.Contains(q, "竞品")) &&
		!matches(q, `(outage|incident|downtime|error|crash|fail|broken|issue)`):
		return &Result{Intent: Why, KindHints: []string{"rationale"}, RecencyPreference: "anytime"}
	// Decision/technology selection queries — "email provider", "选型",
	// "what do we use for X". Must come after debug/reference to avoid
	// false-matching "the provider returned 500" as a decision query.
	// CJK terms use strings.Contains because Go \b doesn't match at
	// CJK character boundaries.
	// Decision terms are broad nouns that imply vendor/tool selection.
	// Guard: skip if the query also contains incident/status words
	// (e.g., "platform outage" is debug, not a decision query).
	case (matches(q, `\b(provider|vendor|supplier|platform|agency|contractor)\b`) || strings.Contains(q, "选型")) &&
		!matches(q, `(outage|incident|downtime|error|crash|fail|broken|issue)`):
		return &Result{Intent: Why, KindHints: []string{"rationale"}, RecencyPreference: "anytime"}
	case matches(q, `(what|which)\b.+\b(use|using|pick|chose|choose|selected|went with|switched to)\b`):
		return &Result{Intent: Why, KindHints: []string{"rationale"}, RecencyPreference: "anytime"}
	case matches(q, `用什么|选了什么|怎么选`):
		return &Result{Intent: Why, KindHints: []string{"rationale"}, RecencyPreference: "anytime"}
	default:
		return &Result{Intent: General, KindHints: nil, RecencyPreference: "anytime"}
	}
}

// FromDistillerNeed maps a distiller information_need string to an IntentType.
func FromDistillerNeed(need string) IntentType {
	switch need {
	case "debugging":
		return Debug
	case "reference":
		return Reference
	case "explanation":
		return WhatIs
	case "procedure":
		return HowTo
	case "decision_rationale":
		return Why
	default:
		return General
	}
}

// compiled pattern cache to avoid recompilation on every call
var patternCache = make(map[string]*regexp.Regexp)

func matches(text string, pattern string) bool {
	re, ok := patternCache[pattern]
	if !ok {
		var err error
		re, err = regexp.Compile(pattern)
		if err != nil {
			return false
		}
		patternCache[pattern] = re
	}
	return re.MatchString(text)
}
