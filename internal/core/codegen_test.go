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
		string(RelBlocks), string(RelDependsOn), string(RelParentOf),
		string(RelChildOf), string(RelRelated),
		// EvidenceKind
		string(EvidenceCommit), string(EvidenceLog), string(EvidenceTestResult),
		string(EvidenceURL), string(EvidenceFile), string(EvidenceCustom),
		// BudgetTier
		string(BudgetInform), string(BudgetWarn), string(BudgetStop),
	}
	for _, v := range enums {
		if !strings.Contains(CoreTypesTS, `"`+v+`"`) {
			t.Errorf("TS codegen missing enum literal %q", v)
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
