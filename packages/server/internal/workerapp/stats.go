package workerapp

import (
	"sync/atomic"
	"time"
)

// Stats tracks job metrics for the worker health endpoint.
type Stats struct {
	jobsCompleted atomic.Int64
	jobsFailed    atomic.Int64
	lastJobAt     atomic.Value // time.Time
	startedAt     time.Time
}

func NewStats(startedAt time.Time) *Stats {
	stats := &Stats{startedAt: startedAt}
	stats.lastJobAt.Store(time.Time{})
	return stats
}

func (s *Stats) RecordCompleted() {
	s.jobsCompleted.Add(1)
	s.lastJobAt.Store(time.Now())
}

func (s *Stats) RecordFailed() {
	s.jobsFailed.Add(1)
	s.lastJobAt.Store(time.Now())
}

func (s *Stats) JobsCompleted() int64 {
	return s.jobsCompleted.Load()
}

func (s *Stats) JobsFailed() int64 {
	return s.jobsFailed.Load()
}

func (s *Stats) LastJobAt() time.Time {
	lastJob, _ := s.lastJobAt.Load().(time.Time)
	return lastJob
}

func (s *Stats) StartedAt() time.Time {
	return s.startedAt
}
