package forget

import (
	"strconv"
	"strings"

	"github.com/MemaxLabs/memax/packages/server/internal/ask"
	"github.com/MemaxLabs/memax/packages/server/internal/judge"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
	"github.com/MemaxLabs/memax/packages/server/internal/v2index"
)

// HonestyFromEnv reads what tombstones say about the copies Memax can't
// reach (plan 25 §5.13 step 3) from the same configuration the processes
// run on, through lookup (os.LookupEnv). Call it once, in the API's
// composition root.
//
//	V2_BACKUP_RETENTION_DAYS  the database's point-in-time restore window (default 7, Neon's)
//
// and, for the outside processors that saw a memory's words: Voyage AI
// when V2 embeddings are on (VOYAGE_API_KEY, V2_EMBED_MODEL); the judge's
// and Ask's provider when ANTHROPIC_API_KEY is set and their tiers aren't
// off, OpenRouter when ANTHROPIC_BASE_URL points there (whose ZDR routing,
// JUDGE_ZDR and ASK_ZDR, is the only zero-retention promise Memax makes),
// Anthropic otherwise.
func HonestyFromEnv(lookup func(string) (string, bool)) ledger.ForgetHonesty {
	get := func(key string) string {
		v, _ := lookup(key)
		return strings.TrimSpace(v)
	}
	h := ledger.ForgetHonesty{BackupDays: ledger.DefaultBackupDays}
	if n, err := strconv.Atoi(get("V2_BACKUP_RETENTION_DAYS")); err == nil && n > 0 {
		h.BackupDays = n
	}
	if v2index.ConfigFromEnv(lookup).Enabled() {
		h.Embeddings = ledger.Processor{Name: "Voyage AI", Purpose: "embeddings"}
	}
	if get("ANTHROPIC_API_KEY") == "" {
		return h
	}
	provider, routesZDR := "Anthropic", false
	if strings.Contains(strings.ToLower(get("ANTHROPIC_BASE_URL")), "openrouter.ai") {
		provider, routesZDR = "OpenRouter", true
	}
	if jc := judge.ConfigFromEnv(lookup); jc.Primary.Model != "" || jc.Fallback.Model != "" || jc.Strong.Model != "" {
		h.Judge = ledger.Processor{Name: provider, Purpose: "judge", ZeroRetention: routesZDR && jc.ZeroDataRetention}
	}
	if ac := ask.ConfigFromEnv(lookup); ac.Model != "" {
		h.Ask = ledger.Processor{Name: provider, Purpose: "ask", ZeroRetention: routesZDR && ac.ZeroDataRetention}
	}
	return h
}
