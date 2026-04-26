package core

import "testing"

func TestDispatchStatus_IsValid(t *testing.T) {
	cases := map[DispatchStatus]bool{
		"":                         true, // empty = unset, normalised to ready
		DispatchReady:              true,
		DispatchAwaitingDesign:     true,
		DispatchAwaitingVendor:     true,
		DispatchAwaitingReview:     true,
		DispatchNotNow:             true,
		"bogus":                    false,
		"awaiting-something-else":  false,
		"READY":                    false, // case-sensitive on purpose
	}
	for in, want := range cases {
		if got := in.IsValid(); got != want {
			t.Errorf("DispatchStatus(%q).IsValid() = %v, want %v", in, got, want)
		}
	}
}

func TestDispatchStatus_EffectiveDefaultsToReady(t *testing.T) {
	if got := DispatchStatus("").Effective(); got != DispatchReady {
		t.Errorf("empty.Effective() = %q, want %q", got, DispatchReady)
	}
	if got := DispatchAwaitingVendor.Effective(); got != DispatchAwaitingVendor {
		t.Errorf("vendor.Effective() = %q, want unchanged", got)
	}
}

func TestEstimatedSize_IsValid(t *testing.T) {
	cases := map[EstimatedSize]bool{
		"":           true,
		SizeSmall:    true,
		SizeMedium:   true,
		SizeLarge:    true,
		"enormous":   false,
		"SMALL":      false,
		"medium-big": false,
	}
	for in, want := range cases {
		if got := in.IsValid(); got != want {
			t.Errorf("EstimatedSize(%q).IsValid() = %v, want %v", in, got, want)
		}
	}
}

func TestEstimatedSize_EffectiveDefaultsToMedium(t *testing.T) {
	if got := EstimatedSize("").Effective(); got != SizeMedium {
		t.Errorf("empty.Effective() = %q, want %q", got, SizeMedium)
	}
	if got := SizeSmall.Effective(); got != SizeSmall {
		t.Errorf("small.Effective() = %q, want unchanged", got)
	}
}
