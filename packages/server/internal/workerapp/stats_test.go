package workerapp

import (
	"sync"
	"testing"
	"time"
)

func TestStatsZeroValues(t *testing.T) {
	t.Parallel()
	started := time.Now()
	s := NewStats(started)
	if s.JobsCompleted() != 0 {
		t.Errorf("initial JobsCompleted = %d", s.JobsCompleted())
	}
	if s.JobsFailed() != 0 {
		t.Errorf("initial JobsFailed = %d", s.JobsFailed())
	}
	if !s.StartedAt().Equal(started) {
		t.Errorf("StartedAt = %v, want %v", s.StartedAt(), started)
	}
	// LastJobAt starts at zero value — no jobs recorded yet.
	if !s.LastJobAt().IsZero() {
		t.Errorf("LastJobAt should be zero, got %v", s.LastJobAt())
	}
}

func TestStatsRecordCompletedAndFailed(t *testing.T) {
	t.Parallel()
	s := NewStats(time.Now())

	before := time.Now()
	s.RecordCompleted()
	s.RecordCompleted()
	s.RecordCompleted()
	s.RecordFailed()
	after := time.Now()

	if s.JobsCompleted() != 3 {
		t.Errorf("JobsCompleted = %d, want 3", s.JobsCompleted())
	}
	if s.JobsFailed() != 1 {
		t.Errorf("JobsFailed = %d, want 1", s.JobsFailed())
	}
	last := s.LastJobAt()
	if last.Before(before) || last.After(after) {
		t.Errorf("LastJobAt = %v, outside [%v, %v]", last, before, after)
	}
}

func TestStatsConcurrentRecording(t *testing.T) {
	t.Parallel()
	s := NewStats(time.Now())
	const N = 500
	var wg sync.WaitGroup
	wg.Add(2 * N)
	for i := 0; i < N; i++ {
		go func() { defer wg.Done(); s.RecordCompleted() }()
		go func() { defer wg.Done(); s.RecordFailed() }()
	}
	wg.Wait()
	if s.JobsCompleted() != int64(N) {
		t.Errorf("JobsCompleted = %d, want %d", s.JobsCompleted(), N)
	}
	if s.JobsFailed() != int64(N) {
		t.Errorf("JobsFailed = %d, want %d", s.JobsFailed(), N)
	}
}
