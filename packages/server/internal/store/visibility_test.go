package store

import (
	"testing"
)

func TestVisibilityScopeSQL_PersonalOnly(t *testing.T) {
	scope := VisibilityScope{OwnerID: "user-1"}
	pred, args := scope.SQLFilter("m", 1)

	if pred != "m.owner_id = $1::uuid" {
		t.Errorf("expected owner-only predicate, got: %s", pred)
	}
	if len(args) != 1 || args[0] != "user-1" {
		t.Errorf("expected [user-1], got: %v", args)
	}
	if scope.ParamCount() != 1 {
		t.Errorf("expected ParamCount=1, got: %d", scope.ParamCount())
	}
}

func TestVisibilityScopeSQL_WithHubs(t *testing.T) {
	hubs := []string{"hub-a", "hub-b"}
	scope := VisibilityScope{OwnerID: "user-1", HubIDs: hubs}
	pred, args := scope.SQLFilter("m", 1)

	expected := "(m.owner_id = $1::uuid OR m.hub_id = ANY($2::uuid[]))"
	if pred != expected {
		t.Errorf("expected %q, got: %q", expected, pred)
	}
	if len(args) != 2 {
		t.Fatalf("expected 2 args, got: %d", len(args))
	}
	if args[0] != "user-1" {
		t.Errorf("expected args[0]=user-1, got: %v", args[0])
	}
	hubArgs, ok := args[1].([]string)
	if !ok {
		t.Fatalf("expected args[1] to be []string, got: %T", args[1])
	}
	if len(hubArgs) != 2 || hubArgs[0] != "hub-a" || hubArgs[1] != "hub-b" {
		t.Errorf("expected [hub-a, hub-b], got: %v", hubArgs)
	}
	if scope.ParamCount() != 2 {
		t.Errorf("expected ParamCount=2, got: %d", scope.ParamCount())
	}
}

func TestVisibilityScopeSQL_DifferentAlias(t *testing.T) {
	scope := VisibilityScope{OwnerID: "u1", HubIDs: []string{"h1"}}
	pred, _ := scope.SQLFilter("t", 3)

	expected := "(t.owner_id = $3::uuid OR t.hub_id = ANY($4::uuid[]))"
	if pred != expected {
		t.Errorf("expected %q, got: %q", expected, pred)
	}
}

func TestVisibilityScopeSQL_EmptyHubs(t *testing.T) {
	// Empty slice should behave like no hubs (personal only)
	scope := VisibilityScope{OwnerID: "user-1", HubIDs: []string{}}
	pred, args := scope.SQLFilter("m", 1)

	if pred != "m.owner_id = $1::uuid" {
		t.Errorf("expected owner-only for empty hubs, got: %s", pred)
	}
	if len(args) != 1 {
		t.Errorf("expected 1 arg for empty hubs, got: %d", len(args))
	}
}

func TestVisibilityScopeSQL_ParamOffset(t *testing.T) {
	// When used after other params (e.g., topic_id = $1), startParam > 1
	scope := VisibilityScope{OwnerID: "u1", HubIDs: []string{"h1"}}
	pred, args := scope.SQLFilter("m", 5)

	expected := "(m.owner_id = $5::uuid OR m.hub_id = ANY($6::uuid[]))"
	if pred != expected {
		t.Errorf("expected %q, got: %q", expected, pred)
	}
	if len(args) != 2 {
		t.Errorf("expected 2 args, got: %d", len(args))
	}
}
