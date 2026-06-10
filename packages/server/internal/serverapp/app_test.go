package serverapp

import (
	"context"
	"errors"
	"testing"
)

// These tests exercise the App lifecycle helpers (addCleanup, addClose,
// Shutdown). The full Configure path requires live DB + env, covered
// elsewhere via integration tests.

func TestAppShutdownRunsCleanupsInReverseOrder(t *testing.T) {
	t.Parallel()
	app := &App{}
	var order []int
	app.addCleanup(func(context.Context) error {
		order = append(order, 1)
		return nil
	})
	app.addCleanup(func(context.Context) error {
		order = append(order, 2)
		return nil
	})
	app.addCleanup(func(context.Context) error {
		order = append(order, 3)
		return nil
	})

	app.Shutdown(context.Background())
	if len(order) != 3 {
		t.Fatalf("expected 3 cleanups ran, got %d", len(order))
	}
	if order[0] != 3 || order[1] != 2 || order[2] != 1 {
		t.Errorf("cleanup order wrong: %v, want reverse", order)
	}
}

func TestAppShutdownContinuesAfterErrors(t *testing.T) {
	t.Parallel()
	app := &App{}
	var ran []int
	app.addCleanup(func(context.Context) error {
		ran = append(ran, 1)
		return nil
	})
	app.addCleanup(func(context.Context) error {
		ran = append(ran, 2)
		return errors.New("boom")
	})
	app.addCleanup(func(context.Context) error {
		ran = append(ran, 3)
		return nil
	})
	app.Shutdown(context.Background())
	if len(ran) != 3 {
		t.Errorf("errors should not stop shutdown; ran = %v", ran)
	}
}

func TestAppAddCloseWrapsAsCleanup(t *testing.T) {
	t.Parallel()
	app := &App{}
	called := false
	app.addClose(func() { called = true })
	app.Shutdown(context.Background())
	if !called {
		t.Error("addClose callback not invoked during Shutdown")
	}
}

func TestAppShutdownWithNoCleanupsIsSafe(t *testing.T) {
	t.Parallel()
	app := &App{}
	app.Shutdown(context.Background())
	// No panic expected.
}
