package decay

import "math"

// Stability half-lives in days. Determines how quickly memories lose
// retrieval weight over time. Based on Ebbinghaus forgetting curve.
// 0 = no decay (memory stays fully relevant forever).
var StabilityHalfLife = map[string]float64{
	"volatile": 14,
	"evolving": 180,
	"stable":   0,
}

// Multiplier calculates the decay-based retrieval multiplier for a memory.
// Combines three signals:
//   - Time decay (Ebbinghaus forgetting curve with stability-specific half-life)
//   - Access reinforcement (each recall extends the effective half-life)
//   - Access boost (frequently recalled memories get a small bonus)
//
// Returns a multiplier between 0.3 and 1.5 to apply to the retrieval score.
//
// Parameters:
//   - daysSinceAccess: days since the memory was last recalled/accessed
//   - accessCount: how many times the memory has been recalled
//   - stability: memory stability axis (volatile, evolving, stable)
func Multiplier(daysSinceAccess float64, accessCount int, stability string) float64 {
	const (
		alpha    = 1.0  // reinforcement strength (how much recalls extend half-life)
		beta     = 0.15 // access boost strength
		floor    = 0.3  // minimum decay — never fully forget
		maxBoost = 1.5  // cap on access boost
		maxOut   = 1.5  // cap on output multiplier
	)

	halfLife, ok := StabilityHalfLife[stability]
	if !ok {
		halfLife = StabilityHalfLife["evolving"]
	}

	// Step 1: Time decay with reinforcement
	var decayFactor float64
	if halfLife <= 0 {
		// No-decay stability.
		decayFactor = 1.0
	} else {
		// Reinforced half-life: each recall extends how long the memory stays strong
		// Logarithmic diminishing returns — 10th recall matters far less than 1st
		effectiveHL := halfLife * (1.0 + alpha*math.Log(1.0+float64(accessCount)))
		// Ebbinghaus: retention = 2^(-t/h)
		decayFactor = math.Pow(2.0, -daysSinceAccess/effectiveHL)
	}

	// Floor: even fully decayed memories retain 30% weight
	// (a highly relevant old doc can still beat a mediocre new one)
	decayFactor = math.Max(decayFactor, floor)

	// Step 2: Access reinforcement boost (small, capped)
	// Frequently recalled memories get a bonus on top of decay
	accessBoost := 1.0 + beta*math.Log(1.0+float64(accessCount))
	accessBoost = math.Min(accessBoost, maxBoost)

	// Step 3: Combine and clamp
	result := decayFactor * accessBoost
	if result > maxOut {
		result = maxOut
	}
	if result < 0 {
		result = 0
	}

	return result
}
