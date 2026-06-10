// Package mockembed provides a deterministic embed.Embedder double for
// tests. Unlike the production Voyage AI embedder it requires no API
// key, makes no network calls, and is fully reproducible: the same
// input text always produces the same 1024-dim vector.
//
// # Why this exists
//
// Several packages branch on embedding output (dreams similarity,
// retrieval, FindRelatedMemories) but can't unit-test those branches
// without either:
//   - A real Voyage API key and network access
//   - Hand-rolling a stub in each _test.go with its own determinism
//     story
//
// The handler integration tests this unblocks would otherwise need
// to short-circuit the embedder entirely, which hides the entire
// embedding→storage→search path from coverage.
//
// # Contract
//
// The vectors are deterministic (seeded from a SHA-256 of input text)
// and normalized to unit length, so cosine similarity between "same"
// texts is 1.0 and between unrelated texts drops toward ~0. Tests
// that need specific similarity relationships between pairs should
// use Ops to force exact outputs; everything else gets
// deterministic-but-arbitrary vectors that satisfy the shape the
// production pipeline expects.
package mockembed

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"math"
	"sync"
)

// Dimensions is the vector width the mock produces. Matches the
// production Voyage `voyage-3` model so chunks inserted through the
// mock pass the pgvector column's dimension CHECK.
const Dimensions = 1024

// Embedder is a deterministic embed.Embedder. The zero value is
// usable; prefer New() for clarity and to register a t.Cleanup
// no-op that makes future extensions (e.g. request counting
// snapshots) explicit.
type Embedder struct {
	mu sync.Mutex
	// overrides maps exact input text → forced output. Used when a
	// test needs specific similarity relationships — e.g. "the
	// organize-phase probe must return near-duplicate vectors for
	// these two titles."
	overrides map[string][]float64
	// calls counts how many inputs have been embedded. Tests assert
	// "was the embedder called at all?" via CallCount().
	calls int
}

// New returns a ready-to-use Embedder.
func New() *Embedder { return &Embedder{} }

// SetOverride forces a specific vector for a given input string.
// Subsequent calls containing that exact string return the override
// instead of the hash-derived vector. Overrides stack — the most
// recent SetOverride for a key wins.
func (e *Embedder) SetOverride(text string, vec []float64) {
	if len(vec) != Dimensions {
		panic("mockembed: override vector must be Dimensions long")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.overrides == nil {
		e.overrides = make(map[string][]float64)
	}
	dup := make([]float64, Dimensions)
	copy(dup, vec)
	e.overrides[text] = dup
}

// CallCount returns the total number of text inputs embedded across
// all Embed / EmbedContext calls. Tests use this to assert the
// embedder was (or was not) hit.
func (e *Embedder) CallCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

// Embed implements embed.Embedder. inputType is accepted but ignored
// — production Voyage differentiates "document" vs "query" types but
// the mock's deterministic mapping doesn't need to.
func (e *Embedder) Embed(texts []string, _ string) ([][]float64, error) {
	return e.EmbedContext(context.Background(), texts, "")
}

// EmbedContext implements embed.Embedder. Cancelled contexts return
// the ctx error before any work; this matches the production
// Embedder's semantics.
func (e *Embedder) EmbedContext(ctx context.Context, texts []string, _ string) ([][]float64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e.mu.Lock()
	e.calls += len(texts)
	ovr := e.overrides
	e.mu.Unlock()

	out := make([][]float64, len(texts))
	for i, t := range texts {
		if v, ok := ovr[t]; ok {
			dup := make([]float64, Dimensions)
			copy(dup, v)
			out[i] = dup
			continue
		}
		out[i] = vectorFor(t)
	}
	return out, nil
}

// Dimensions implements embed.Embedder.
func (e *Embedder) Dimensions() int { return Dimensions }

// vectorFor produces a deterministic, unit-length 1024-dim vector
// from a text's SHA-256. The algorithm:
//
//  1. hash = SHA-256(text)
//  2. Expand to Dimensions × 8 bytes via SHAKE-lite:
//     repeat SHA-256(hash || counter) until filled.
//  3. Interpret as float64 mantissas, map to [-1, 1).
//  4. Normalize to unit length.
//
// The SHAKE-lite step is trivial to implement without importing
// golang.org/x/crypto/sha3 — each 32-byte digest yields 4 float64
// values (8 bytes each); we need 256 rounds for a 1024-dim vector.
// Fast enough at test scale (microseconds per call).
func vectorFor(text string) []float64 {
	seed := sha256.Sum256([]byte(text))
	out := make([]float64, Dimensions)

	// Stream enough bytes from repeated hashing.
	var buf [Dimensions * 8]byte
	for counter := uint32(0); counter < (Dimensions/4)+1; counter++ {
		var digestInput [32 + 4]byte
		copy(digestInput[:32], seed[:])
		binary.BigEndian.PutUint32(digestInput[32:], counter)
		d := sha256.Sum256(digestInput[:])
		off := int(counter) * 32
		if off+32 > len(buf) {
			copy(buf[off:], d[:len(buf)-off])
			break
		}
		copy(buf[off:off+32], d[:])
	}

	for i := 0; i < Dimensions; i++ {
		u := binary.BigEndian.Uint64(buf[i*8 : i*8+8])
		// Map uint64 → [-1, 1) by dividing by 2^63.
		out[i] = float64(int64(u)) / (1 << 62)
	}

	// Unit-normalize — production embeddings are normalized and some
	// downstream math (cosine similarity) assumes it.
	var sumSq float64
	for _, v := range out {
		sumSq += v * v
	}
	if sumSq > 0 {
		inv := 1.0 / math.Sqrt(sumSq)
		for i := range out {
			out[i] *= inv
		}
	}
	return out
}
