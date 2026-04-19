package registry

import "testing"

func TestRegisterAndListSorted(t *testing.T) {
	Reset()
	t.Cleanup(Reset)

	Register(Adaptor{Name: "zeta", Plane: WorkPlane})
	Register(Adaptor{Name: "alpha", Plane: OrchestrationPlane})
	Register(Adaptor{Name: "beads", Plane: WorkPlane})

	got := List()
	if len(got) != 3 {
		t.Fatalf("want 3 adaptors, got %d", len(got))
	}

	// orchestration sorts before work ("orchestration" < "work"), then by name.
	want := []string{"alpha", "beads", "zeta"}
	for i, a := range got {
		if a.Name != want[i] {
			t.Errorf("index %d: got %q, want %q", i, a.Name, want[i])
		}
	}
}

func TestListReturnsCopy(t *testing.T) {
	Reset()
	t.Cleanup(Reset)

	Register(Adaptor{Name: "x", Plane: WorkPlane})
	got := List()
	got[0].Name = "mutated"

	again := List()
	if again[0].Name != "x" {
		t.Fatalf("List() must not share backing storage: got %q", again[0].Name)
	}
}
