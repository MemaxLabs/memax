package decay

import (
	"math"
	"testing"
)

// within checks that two floats are within epsilon — decay
// arithmetic uses math.Log/math.Pow, so exact equality is fragile.
func within(a, b, eps float64) bool {
	return math.Abs(a-b) <= eps
}

func TestMultiplierStableStabilityNoDecay(t *testing.T) {
	t.Parallel()

	// "stable" stability has half-life 0 → never decays.
	// Multiplier should equal the access-boost factor alone.
	m0 := Multiplier(0, 0, "stable")
	if !within(m0, 1.0, 0.001) {
		t.Errorf("stable, zero accesses: got %v, want 1.0", m0)
	}

	// 1000 days later, no accesses: should still be 1.0 (no decay).
	m1000 := Multiplier(1000, 0, "stable")
	if !within(m1000, 1.0, 0.001) {
		t.Errorf("stable, 1000 days: got %v, want 1.0 (no decay)", m1000)
	}
}

func TestMultiplierVolatileDecays(t *testing.T) {
	t.Parallel()

	// Volatile half-life is 14 days. At exactly 14 days with zero
	// accesses: decay factor = 2^(-14/14) = 0.5, access boost = 1.
	// Result = 0.5.
	got := Multiplier(14, 0, "volatile")
	if !within(got, 0.5, 0.01) {
		t.Errorf("volatile @ 14d: got %v, want ~0.5", got)
	}
}

func TestMultiplierUnknownStabilityFallsBackToEvolving(t *testing.T) {
	t.Parallel()

	// Unknown stability uses evolving's half-life (180 days).
	// At 180 days with 0 accesses: decay = 0.5 * 1.0 = 0.5.
	got := Multiplier(180, 0, "nonsense")
	want := Multiplier(180, 0, "evolving")
	if !within(got, want, 0.001) {
		t.Errorf("unknown stability: got %v, want %v (evolving fallback)", got, want)
	}
}

func TestMultiplierFloorPreventsZero(t *testing.T) {
	t.Parallel()

	// Extreme age should clamp decay factor at 0.3 (floor).
	// With 0 accesses, access boost = 1.0. Result = 0.3.
	got := Multiplier(1_000_000, 0, "volatile")
	if !within(got, 0.3, 0.001) {
		t.Errorf("extreme age volatile: got %v, want 0.3 (floor)", got)
	}
}

func TestMultiplierAccessBoostAmplifies(t *testing.T) {
	t.Parallel()

	// Fresh memory, many accesses: access boost pushes above 1.0.
	// Cap is 1.5.
	m := Multiplier(0, 1_000_000, "evolving")
	if m > 1.5+0.001 {
		t.Errorf("access boost exceeded cap: got %v, want <= 1.5", m)
	}
	if m <= 1.0 {
		t.Errorf("access boost should amplify fresh memory, got %v", m)
	}
}

func TestMultiplierOutputCappedAtMax(t *testing.T) {
	t.Parallel()

	// The output hard cap is 1.5 regardless of inputs.
	m := Multiplier(0, math.MaxInt64, "stable")
	if m > 1.5+0.001 {
		t.Errorf("output cap exceeded: got %v, want <= 1.5", m)
	}
}

func TestMultiplierReinforcementExtendsHalfLife(t *testing.T) {
	t.Parallel()

	// 14 days, stability=volatile:
	//   - 0 accesses  → decay ~= 0.5
	//   - 10 accesses → reinforcement extends effective half-life,
	//     so decay factor is closer to 1.0
	zero := Multiplier(14, 0, "volatile")
	many := Multiplier(14, 10, "volatile")
	if many <= zero {
		t.Errorf("reinforcement should EXTEND half-life, got zero=%v many=%v", zero, many)
	}
}

func TestMultiplierNonNegative(t *testing.T) {
	t.Parallel()

	// Sanity: exhaustive small-domain check that the function
	// never returns < 0 or NaN. The clamp-at-0 safety guard on
	// line 67-69 should never fire in practice but the test
	// confirms it's bulletproof for reasonable inputs.
	for _, days := range []float64{0, 1, 30, 180, 365, 10_000} {
		for _, acc := range []int{0, 1, 10, 1_000} {
			for _, s := range []string{"volatile", "evolving", "stable", "garbage"} {
				m := Multiplier(days, acc, s)
				if m < 0 {
					t.Errorf("negative multiplier: days=%v acc=%d stab=%s got=%v", days, acc, s, m)
				}
				if math.IsNaN(m) {
					t.Errorf("NaN multiplier: days=%v acc=%d stab=%s", days, acc, s)
				}
			}
		}
	}
}
