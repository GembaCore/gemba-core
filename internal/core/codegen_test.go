package core

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestCoreTypesTSContainsEveryGoJSONField asserts shape-level parity:
// every json tag declared on the core Go types has a matching property
// name somewhere in the generated TS. A missing tag means the next
// `make gen` would ship out-of-sync types; treat this as a hard failure
// so the codegen contract can't silently drift.
func TestCoreTypesTSContainsEveryGoJSONField(t *testing.T) {
	probes := []any{
		WorkItem{},
		AgentRef{},
		Relationship{},
		Evidence{},
		DefinitionOfDone{},
		TokenBudget{},
		Sprint{},
		Flags{},
		DerivedSignals{},
	}

	missing := []string{}
	for _, p := range probes {
		typ := reflect.TypeOf(p)
		for i := 0; i < typ.NumField(); i++ {
			tag := typ.Field(i).Tag.Get("json")
			if tag == "" || tag == "-" {
				continue
			}
			name := strings.Split(tag, ",")[0]
			// TS emits required fields as `name:` and optional as `name?:`;
			// accept either form.
			if !strings.Contains(CoreTypesTS, name+":") &&
				!strings.Contains(CoreTypesTS, name+"?:") {
				missing = append(missing, typ.Name()+"."+name)
			}
		}
	}

	if len(missing) > 0 {
		t.Fatalf("TS codegen missing fields present in Go types: %v", missing)
	}
}

// TestCoreTypesTSContainsEveryEnumString asserts every canonical enum
// value has a literal in the TS output.
func TestCoreTypesTSContainsEveryEnumString(t *testing.T) {
	enums := []string{
		// StateCategory
		string(StateBacklog), string(StateUnstarted), string(StateStarted),
		string(StateCompleted), string(StateCanceled),
		// RelationshipKind
		string(RelBlocks), string(RelParentChild), string(RelRelatesTo),
		// AgentKind
		string(AgentKindAgent), string(AgentKindHuman),
		// EvidenceKind
		string(EvidenceCommit), string(EvidenceLog), string(EvidenceTestResult),
		string(EvidenceURL), string(EvidenceFile), string(EvidenceCustom),
		// BudgetTier
		string(BudgetInform), string(BudgetWarn), string(BudgetStop),
		// AdaptorErrorKind (gm-faz) — every canonical kind must appear
		// as a TS literal so the SPA gets exhaustive-switch coverage.
		string(KindValidation), string(KindSessionNotFound), string(KindSessionClosed),
		string(KindRequestFailed), string(KindProcessFailed), string(KindRateLimited),
		string(KindUnsupported), string(KindCapabilityDenied), string(KindAdaptorDegraded),
	}
	for _, v := range enums {
		if !strings.Contains(CoreTypesTS, `"`+v+`"`) {
			t.Errorf("TS codegen missing enum literal %q", v)
		}
	}
}

// TestCoreTypesTSHasAdaptorErrorShape asserts the TS emission of
// AdaptorError carries the _kind discriminator and retryable field so
// the SPA can branch correctly. Shape-level — checks the key fragments
// show up somewhere in the generated output.
func TestCoreTypesTSHasAdaptorErrorShape(t *testing.T) {
	required := []string{
		"AdaptorErrorKind",
		"AdaptorError",
		"_kind:",
		"retryable:",
	}
	for _, frag := range required {
		if !strings.Contains(CoreTypesTS, frag) {
			t.Errorf("TS codegen missing AdaptorError fragment %q", frag)
		}
	}
}

// Sanity check the generated header is present so downstream greps for
// "DO NOT EDIT" don't silently fail.
func TestCoreTypesTSHasDoNotEditBanner(t *testing.T) {
	if !strings.Contains(CoreTypesTS, "DO NOT EDIT") {
		t.Error("codegen output must carry a DO NOT EDIT banner")
	}
}

// Guard against accidentally dropping the time.Time import / usage.
var _ = time.Time{}
