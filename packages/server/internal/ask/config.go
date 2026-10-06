package ask

import (
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// Config configures Ask. The model tier is explicit configuration (plan
// 25 §4.3, the judge's pattern), never inferred from a model's name.
type Config struct {
	// Model is the answer tier's slug, as the gateway knows it. Empty
	// turns synthesis off: Ask then answers with the matching kept
	// memories and no words of its own.
	Model string
	// ZeroDataRetention routes every call to zero-retention providers only
	// (OpenRouter provider.zdr, D14).
	ZeroDataRetention bool
	// Timeout bounds the whole answer, first token to last.
	Timeout time.Duration
	// MaxTokens bounds the answer (three short sentences need far less).
	MaxTokens int
	// Sources is how many kept memories the model is given.
	Sources int
	// RetrievalTimeout bounds the search (plan §5.11: search p95 < 500 ms).
	RetrievalTimeout time.Duration
	// MonthlyLimit caps each person's asks a month when no plan says
	// otherwise (ASK_MONTHLY_LIMIT; 0: no cap, the free alpha's default).
	MonthlyLimit int
	Log          *slog.Logger
}

// The defaults.
const (
	// DefaultModel is the answer tier: DeepSeek V4.1 Flash on OpenRouter's
	// zero-retention hosts (§4.3), never DeepSeek's own API (D14).
	DefaultModel            = "deepseek/deepseek-v4.1-flash"
	DefaultTimeout          = 20 * time.Second
	DefaultMaxTokens        = 400
	DefaultSources          = 8
	DefaultRetrievalTimeout = 500 * time.Millisecond
)

func (c Config) withDefaults() Config {
	if c.Timeout <= 0 {
		c.Timeout = DefaultTimeout
	}
	if c.MaxTokens <= 0 {
		c.MaxTokens = DefaultMaxTokens
	}
	if c.Sources <= 0 {
		c.Sources = DefaultSources
	}
	if c.RetrievalTimeout <= 0 {
		c.RetrievalTimeout = DefaultRetrievalTimeout
	}
	if c.Log == nil {
		c.Log = slog.Default()
	}
	return c
}

// ConfigFromEnv reads Ask's configuration through lookup (os.LookupEnv).
// Call it once, at startup, in the API's composition root.
//
//	ASK_MODEL          the answer tier (default deepseek/deepseek-v4.1-flash; "off" answers with memories only)
//	ASK_ZDR            zero-data-retention routing (default true)
//	ASK_TIMEOUT_MS     one answer, first token to last (default 20000)
//	ASK_MONTHLY_LIMIT  asks a person may make a month (default none during the free alpha; 50 is Free's, D9)
//
// V1's /v1/ask reads ASK_MODEL too, as its strong tier; it ignores "off".
func ConfigFromEnv(lookup func(string) (string, bool)) Config {
	c := Config{Model: DefaultModel, ZeroDataRetention: true}
	if v, ok := lookup("ASK_MODEL"); ok {
		v = strings.TrimSpace(v)
		switch {
		case strings.EqualFold(v, "off"), strings.EqualFold(v, "none"):
			c.Model = ""
		case v != "":
			c.Model = v
		}
	}
	if v, ok := lookup("ASK_ZDR"); ok {
		if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
			c.ZeroDataRetention = b
		}
	}
	if v, ok := lookup("ASK_TIMEOUT_MS"); ok {
		if ms, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && ms > 0 {
			c.Timeout = time.Duration(ms) * time.Millisecond
		}
	}
	if v, ok := lookup("ASK_MONTHLY_LIMIT"); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			c.MonthlyLimit = n
		}
	}
	return c
}
