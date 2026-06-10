package mockembed_test

import (
	"context"
	"testing"

	"github.com/MemaxLabs/memax/packages/server/internal/ingest/embed/mockembed"
)

func TestDeterministicOutputForSameInput(t *testing.T) {
	t.Parallel()
	e := mockembed.New()
	v1, err := e.Embed([]string{"hello"}, "document")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	v2, err := e.Embed([]string{"hello"}, "document")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(v1) != 1 || len(v2) != 1 {
		t.Fatalf("unexpected batch size")
	}
	if len(v1[0]) != mockembed.Dimensions {
		t.Fatalf("dim = %d, want %d", len(v1[0]), mockembed.Dimensions)
	}
	for i := range v1[0] {
		if v1[0][i] != v2[0][i] {
			t.Fatalf("same input produced different vectors at index %d", i)
			break
		}
	}
}

func TestDifferentInputsProduceDifferentVectors(t *testing.T) {
	t.Parallel()
	e := mockembed.New()
	vs, err := e.Embed([]string{"apple", "banana", "cherry"}, "document")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	// All three must differ in at least some index.
	same := func(a, b []float64) bool {
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}
	if same(vs[0], vs[1]) || same(vs[1], vs[2]) || same(vs[0], vs[2]) {
		t.Error("distinct inputs produced identical vectors")
	}
}

func TestVectorIsUnitLength(t *testing.T) {
	t.Parallel()
	e := mockembed.New()
	v, err := e.Embed([]string{"anything"}, "document")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	var sumSq float64
	for _, x := range v[0] {
		sumSq += x * x
	}
	// Allow floating-point slop.
	if sumSq < 0.9999 || sumSq > 1.0001 {
		t.Errorf("|v|² = %f, want ~1.0", sumSq)
	}
}

func TestSetOverrideForcesVector(t *testing.T) {
	t.Parallel()
	e := mockembed.New()
	forced := make([]float64, mockembed.Dimensions)
	forced[0] = 1.0 // arbitrary distinguishing signal
	e.SetOverride("special input", forced)

	v, err := e.Embed([]string{"special input", "other"}, "document")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if v[0][0] != 1.0 {
		t.Errorf("override not applied: %f", v[0][0])
	}
	// Non-overridden input still gets hash-derived vector (nonzero at
	// most indices).
	if v[1][0] == 0 && v[1][1] == 0 && v[1][2] == 0 {
		t.Error("non-overridden input looks too-zero — hash derivation may be broken")
	}
}

func TestSetOverrideRejectsWrongDimension(t *testing.T) {
	t.Parallel()
	e := mockembed.New()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for wrong-dim override")
		}
	}()
	e.SetOverride("x", []float64{1, 2, 3})
}

func TestCallCountTracksInputs(t *testing.T) {
	t.Parallel()
	e := mockembed.New()
	if e.CallCount() != 0 {
		t.Errorf("initial CallCount = %d", e.CallCount())
	}
	_, _ = e.Embed([]string{"a", "b", "c"}, "document")
	_, _ = e.Embed([]string{"d"}, "document")
	if e.CallCount() != 4 {
		t.Errorf("after 4 inputs, CallCount = %d", e.CallCount())
	}
}

func TestEmbedContextHonorsCancellation(t *testing.T) {
	t.Parallel()
	e := mockembed.New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := e.EmbedContext(ctx, []string{"x"}, "document")
	if err == nil {
		t.Error("canceled ctx should error")
	}
}

func TestDimensionsReturnsConstant(t *testing.T) {
	t.Parallel()
	e := mockembed.New()
	if e.Dimensions() != mockembed.Dimensions {
		t.Errorf("Dimensions() = %d", e.Dimensions())
	}
}
