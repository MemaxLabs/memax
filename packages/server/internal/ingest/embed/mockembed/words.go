package mockembed

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"math"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Words is a deterministic embedder with a little meaning: a statement's
// vector is the normalised sum of its content words, each hashed onto a
// few signed dimensions, after a crude stemmer and an optional synonym
// table. Two statements are as close as the (stemmed, synonym-folded)
// words they share, so tests and the fake mode of the evals can ask
// "does the vector lane find what the lexical lanes missed" without a
// network: a synonym group ("postgres", "database", "pg") is the one kind
// of meaning the lexical lanes can't see.
//
// It is not a language model. Quality numbers measured with it are a
// check of the plumbing (fusion, floors, deadlines), never of retrieval;
// the live mode of each eval uses Voyage.
type Words struct {
	mu       sync.Mutex
	synonyms map[string]string
	delay    time.Duration
	calls    int
}

// NewWords returns a Words embedder. Each group lists words that mean the
// same thing; the first is the one they fold to.
func NewWords(groups ...[]string) *Words {
	w := &Words{synonyms: map[string]string{}}
	for _, g := range groups {
		for _, word := range g[1:] {
			w.synonyms[stem(strings.ToLower(word))] = stem(strings.ToLower(g[0]))
		}
	}
	return w
}

// SetDelay makes every call wait d, or until its context is done.
func (w *Words) SetDelay(d time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.delay = d
}

// CallCount returns how many texts were embedded.
func (w *Words) CallCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.calls
}

// Embed implements embed.Embedder.
func (w *Words) Embed(texts []string, inputType string) ([][]float64, error) {
	return w.EmbedContext(context.Background(), texts, inputType)
}

// EmbedContext implements embed.Embedder.
func (w *Words) EmbedContext(ctx context.Context, texts []string, _ string) ([][]float64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	w.mu.Lock()
	w.calls += len(texts)
	delay := w.delay
	w.mu.Unlock()
	if err := wait(ctx, delay); err != nil {
		return nil, err
	}
	out := make([][]float64, len(texts))
	for i, t := range texts {
		out[i] = w.vector(t)
	}
	return out, nil
}

// Dimensions implements embed.Embedder.
func (w *Words) Dimensions() int { return Dimensions }

// Terms are the words a text is embedded from, after stemming and
// synonyms (exported for tests that reason about overlaps).
func (w *Words) Terms(text string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len(f) < 2 || stopwords[f] {
			continue
		}
		s := stem(f)
		if syn, ok := w.synonyms[s]; ok {
			s = syn
		}
		out = append(out, s)
	}
	return out
}

func (w *Words) vector(text string) []float64 {
	v := make([]float64, Dimensions)
	for _, term := range w.Terms(text) {
		h := sha256.Sum256([]byte(term))
		// Three signed dimensions per term keep collisions rare at 1024.
		for k := 0; k < 3; k++ {
			idx := binary.BigEndian.Uint32(h[k*4:]) % Dimensions
			sign := 1.0
			if h[12+k]&1 == 1 {
				sign = -1
			}
			v[idx] += sign
		}
	}
	var sum float64
	for _, x := range v {
		sum += x * x
	}
	if sum == 0 {
		// No content words: a fixed direction, far from everything else.
		v[0] = 1
		return v
	}
	inv := 1 / math.Sqrt(sum)
	for i := range v {
		v[i] *= inv
	}
	return v
}

// stem strips the commonest English suffixes, so "deploys", "deployed"
// and "deploying" fold to "deploy".
func stem(w string) string {
	for _, suf := range []string{"ing", "ed", "es", "s"} {
		if len(w) > len(suf)+2 && strings.HasSuffix(w, suf) {
			return strings.TrimSuffix(w, suf)
		}
	}
	return w
}

var stopwords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true, "be": true, "by": true, "do": true,
	"for": true, "from": true, "in": true, "is": true, "it": true, "of": true, "on": true, "or": true, "the": true,
	"this": true, "that": true, "to": true, "we": true, "with": true, "our": true, "us": true, "all": true,
}
