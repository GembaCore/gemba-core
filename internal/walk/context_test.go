package walk

import (
	"context"
	"testing"
)

func TestContextID_RoundTrip(t *testing.T) {
	ctx := context.Background()
	if id := IDFromContext(ctx); id != "" {
		t.Errorf("empty context should yield empty id; got %q", id)
	}
	if IsActive(ctx) {
		t.Error("empty context should not be active")
	}
	ctx = ContextWithID(ctx, "walk-1")
	if got := IDFromContext(ctx); got != "walk-1" {
		t.Errorf("expected walk-1; got %q", got)
	}
	if !IsActive(ctx) {
		t.Error("context with id should be active")
	}
}

func TestContextID_EmptyShadowsParent(t *testing.T) {
	parent := ContextWithID(context.Background(), "walk-1")
	child := ContextWithID(parent, "")
	if got := IDFromContext(child); got != "" {
		t.Errorf("child should shadow parent id; got %q", got)
	}
	if IsActive(child) {
		t.Error("child with empty id should not be active")
	}
	// parent unchanged.
	if IDFromContext(parent) != "walk-1" {
		t.Errorf("parent id corrupted: %q", IDFromContext(parent))
	}
}

func TestContextID_NilContextSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("IDFromContext(nil) panicked: %v", r)
		}
	}()
	if id := IDFromContext(nil); id != "" {
		t.Errorf("nil ctx id = %q, want empty", id)
	}
}
