package ledger

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Prefix is the letter of a display ID: M-0219, N-0882, H-0093 …
type Prefix string

// The display-ID prefixes (plan 25 §5.3; G- per E8).
const (
	PrefixMemory   Prefix = "M"
	PrefixNote     Prefix = "N"
	PrefixHandoff  Prefix = "H"
	PrefixCompile  Prefix = "C"
	PrefixRead     Prefix = "R"
	PrefixDream    Prefix = "D"
	PrefixBrief    Prefix = "B"
	PrefixDecision Prefix = "G" // decision gates
)

var prefixes = []Prefix{PrefixMemory, PrefixNote, PrefixHandoff, PrefixCompile, PrefixRead, PrefixDream, PrefixBrief, PrefixDecision}

// FormatRef renders a display ID, zero-padded to at least four digits
// and growing beyond: M-0007, M-0219, M-12345.
func FormatRef(p Prefix, n int64) string {
	return fmt.Sprintf("%s-%04d", p, n)
}

// ParseRef parses a display ID. The prefix is case-insensitive ("m-219"
// works); the number must be positive.
func ParseRef(s string) (Prefix, int64, bool) {
	head, digits, ok := strings.Cut(strings.TrimSpace(s), "-")
	if !ok || digits == "" || len(digits) > 18 {
		return "", 0, false
	}
	p := Prefix(strings.ToUpper(head))
	known := false
	for _, x := range prefixes {
		known = known || x == p
	}
	if !known {
		return "", 0, false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return "", 0, false
		}
	}
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || n < 1 {
		return "", 0, false
	}
	return p, n, true
}

// allocateRef takes the tenant's next number for a prefix, in the
// caller's transaction. The counter row stays locked until the
// transaction ends, so concurrent writers in one tenant queue for it and
// numbers never repeat; a rolled-back transaction gives its number back.
func allocateRef(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, p Prefix) (int64, error) {
	var n int64
	err := tx.QueryRow(ctx, `
		INSERT INTO v2.id_counters AS c (tenant_id, prefix, next) VALUES ($1, $2, 2)
		ON CONFLICT (tenant_id, prefix) DO UPDATE SET next = c.next + 1
		RETURNING next - 1`, tenantID, string(p)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("ledger: allocate %s- number: %w", p, err)
	}
	return n, nil
}
