package netsim

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"time"
)

// LatencyEnv names the environment variable that turns on the wall-clock
// latency tests (MEMAX_LATENCY=1). They are opt-in: wall clock is noisy on
// shared CI machines, and the round-trip guards cover regressions there.
const LatencyEnv = "MEMAX_LATENCY"

// LatencyOn reports whether the wall-clock latency tests should run.
func LatencyOn() bool { return strings.TrimSpace(os.Getenv(LatencyEnv)) == "1" }

// ProductionRTT is the measured round trip from Fly sjc to Neon
// us-west-2 (median 23.8 ms over 15 `SELECT 1`s, Oct 7, 2026).
const ProductionRTT = 24 * time.Millisecond

// Sample collects one operation's timings and round trips.
type Sample struct {
	Name  string
	RTT   time.Duration
	took  []time.Duration
	trips []int
}

// Add records one run.
func (s *Sample) Add(took time.Duration, trips int) {
	s.took = append(s.took, took)
	s.trips = append(s.trips, trips)
}

// P is the q-quantile of the timings (nearest rank).
func (s *Sample) P(q float64) time.Duration {
	if len(s.took) == 0 {
		return 0
	}
	t := slices.Clone(s.took)
	slices.Sort(t)
	i := int(q*float64(len(t))+0.5) - 1
	return t[max(0, min(i, len(t)-1))]
}

// Trips is the median round-trip count.
func (s *Sample) Trips() int {
	if len(s.trips) == 0 {
		return 0
	}
	t := slices.Clone(s.trips)
	slices.Sort(t)
	return t[len(t)/2]
}

// Row is the sample as a Markdown table row: operation, network, p50,
// p95, round trips.
func (s *Sample) Row() string {
	return fmt.Sprintf("| %-40s | %5s | %7.1f | %7.1f | %3d |", s.Name, fmtRTT(s.RTT), msOf(s.P(0.5)), msOf(s.P(0.95)), s.Trips())
}

// Header is the table's header for Row.
const Header = "| operation                                |   RTT | p50 ms  | p95 ms  | RTs |\n" +
	"|------------------------------------------|-------|---------|---------|-----|"

func fmtRTT(d time.Duration) string {
	if d == 0 {
		return "0"
	}
	return fmt.Sprintf("%dms", d.Milliseconds())
}

func msOf(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }
