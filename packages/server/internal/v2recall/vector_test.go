package v2recall

import (
	"context"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed/mockembed"
)

// An embedding started early (Embed) can be done before its deadline and
// asked for after it, when resolving who is asking took longer: it still
// counts. A select between it and the expired timer would pick at random.
func TestEmbeddingDoneBeforeItsDeadlineCountsAfterIt(t *testing.T) {
	t.Parallel()
	v := &Vectors{query: mockembed.NewWords(), document: mockembed.NewWords(),
		cfg: VectorConfig{Model: "voyage-4", QueryDeadline: 10 * time.Millisecond}, cache: newVectorCache(64, time.Minute)}
	for i := range 40 {
		p := v.startQuery(context.Background(), "which queue runs background jobs "+string(rune('a'+i%26)))
		<-p.done
		time.Sleep(15 * time.Millisecond) // past the deadline
		vec, status, _, err := p.wait()
		if status != StageOK || vec == nil || err != nil {
			t.Fatalf("run %d: %s (%v), want the finished embedding", i, status, err)
		}
	}

	// One that isn't done by its deadline still answers lexically.
	slow := mockembed.NewWords()
	slow.SetDelay(time.Second)
	v.query = slow
	start := time.Now()
	if _, status, _, _ := v.startQuery(context.Background(), "slow").wait(); status != StageTimeout {
		t.Fatalf("a slow embedding: %s, want %s", status, StageTimeout)
	}
	if took := time.Since(start); took > 200*time.Millisecond {
		t.Errorf("waited %v for a 10 ms deadline", took)
	}
}
