package forget_test

import (
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/forget"
	"github.com/MemaxLabs/memax/packages/server/internal/ledger"
)

func TestHonestyFromEnv(t *testing.T) {
	t.Parallel()
	env := func(kv map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) { v, ok := kv[k]; return v, ok }
	}
	for _, tc := range []struct {
		name string
		env  map[string]string
		want ledger.ForgetHonesty
	}{
		{"nothing configured", nil, ledger.ForgetHonesty{BackupDays: 7}},
		{"a longer restore window", map[string]string{"V2_BACKUP_RETENTION_DAYS": "30"}, ledger.ForgetHonesty{BackupDays: 30}},
		{"a bad window keeps the default", map[string]string{"V2_BACKUP_RETENTION_DAYS": "-1"}, ledger.ForgetHonesty{BackupDays: 7}},
		{"OpenRouter routes zero retention", map[string]string{
			"VOYAGE_API_KEY": "v", "ANTHROPIC_API_KEY": "k", "ANTHROPIC_BASE_URL": "https://openrouter.ai/api",
		}, ledger.ForgetHonesty{BackupDays: 7,
			Embeddings: ledger.Processor{Name: "Voyage AI", Purpose: "embeddings"},
			Judge:      ledger.Processor{Name: "OpenRouter", Purpose: "judge", ZeroRetention: true},
			Ask:        ledger.Processor{Name: "OpenRouter", Purpose: "ask", ZeroRetention: true}}},
		{"ZDR off is said", map[string]string{
			"ANTHROPIC_API_KEY": "k", "ANTHROPIC_BASE_URL": "https://openrouter.ai/api", "JUDGE_ZDR": "false", "ASK_MODEL": "off",
		}, ledger.ForgetHonesty{BackupDays: 7,
			Judge: ledger.Processor{Name: "OpenRouter", Purpose: "judge"}}},
		{"Anthropic directly promises nothing", map[string]string{
			"ANTHROPIC_API_KEY": "k", "V2_EMBED_MODEL": "off", "VOYAGE_API_KEY": "v",
		}, ledger.ForgetHonesty{BackupDays: 7,
			Judge: ledger.Processor{Name: "Anthropic", Purpose: "judge"},
			Ask:   ledger.Processor{Name: "Anthropic", Purpose: "ask"}}},
		{"every judge tier off", map[string]string{
			"ANTHROPIC_API_KEY": "k", "JUDGE_MODEL": "off", "JUDGE_FALLBACK_MODEL": "off", "JUDGE_STRONG_MODEL": "off", "ASK_MODEL": "off",
		}, ledger.ForgetHonesty{BackupDays: 7}},
	} {
		if got := forget.HonestyFromEnv(env(tc.env)); got != tc.want {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
}
