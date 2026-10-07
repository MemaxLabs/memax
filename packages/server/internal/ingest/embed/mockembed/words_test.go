package mockembed_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed/mockembed"
)

func cosine(a, b []float64) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	return dot / math.Sqrt(na*nb)
}

// Words: shared (stemmed, synonym-folded) words make statements close,
// and nothing in common keeps them apart.
func TestWordsMeasuresSharedWords(t *testing.T) {
	t.Parallel()
	w := mockembed.NewWords([]string{"postgres", "database", "pg"})
	vs, err := w.Embed([]string{
		"We deploy the API to Fly machines",
		"The API deploys on Fly machines",
		"Background jobs run on River",
		"Migrations run against the database",
		"Migrations run against Postgres",
	}, "document")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name     string
		a, b     int
		min, max float64
	}{
		{"paraphrase", 0, 1, 0.9, 1.01},
		{"unrelated", 0, 2, -0.2, 0.2},
		{"synonym", 3, 4, 0.99, 1.01},
	} {
		if got := cosine(vs[c.a], vs[c.b]); got < c.min || got > c.max {
			t.Errorf("%s: cosine %.3f, want [%.2f, %.2f]", c.name, got, c.min, c.max)
		}
	}
}

// SetDelay holds a call until its deadline; SetError fails it.
func TestDelayAndError(t *testing.T) {
	t.Parallel()
	e := mockembed.New()
	e.SetDelay(200 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := e.EmbedContext(ctx, []string{"x"}, "query"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the deadline", err)
	}
	if took := time.Since(start); took > 150*time.Millisecond {
		t.Errorf("waited %v past a 20 ms deadline", took)
	}
	e.SetDelay(0)
	boom := errors.New("boom")
	e.SetError(boom)
	if _, err := e.EmbedContext(context.Background(), []string{"x"}, "query"); !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
	if e.RequestCount() != 2 {
		t.Errorf("requests = %d", e.RequestCount())
	}
}
