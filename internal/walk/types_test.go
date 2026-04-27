package walk

import "testing"

func TestStatus_IsTerminal(t *testing.T) {
	cases := map[Status]bool{
		StatusActive:    false,
		StatusPaused:    false,
		StatusCompleted: true,
		StatusAbandoned: true,
	}
	for s, want := range cases {
		if got := s.IsTerminal(); got != want {
			t.Errorf("%s.IsTerminal() = %v, want %v", s, got, want)
		}
	}
}

func TestAgendaItem_IsQueued(t *testing.T) {
	cases := map[AgendaItemStatus]bool{
		AgendaQueued:    true,
		AgendaActive:    false,
		AgendaDecided:   false,
		AgendaDeferred:  false,
		AgendaDismissed: false,
	}
	for s, want := range cases {
		a := AgendaItem{Status: s}
		if got := a.IsQueued(); got != want {
			t.Errorf("status=%s: IsQueued() = %v, want %v", s, got, want)
		}
	}
}

func TestCost_Add(t *testing.T) {
	a := Cost{TokensIn: 10, TokensOut: 5, Dollars: 0.04}
	b := Cost{TokensIn: 3, TokensOut: 7, Dollars: 0.06}
	got := a.Add(b)
	want := Cost{TokensIn: 13, TokensOut: 12, Dollars: 0.10}
	if got != want {
		t.Errorf("Cost.Add: got %+v, want %+v", got, want)
	}
}

func TestWalk_QueuedAgenda(t *testing.T) {
	w := Walk{Agenda: []AgendaItem{
		{ID: "a", Status: AgendaQueued},
		{ID: "b", Status: AgendaDecided},
		{ID: "c", Status: AgendaQueued},
		{ID: "d", Status: AgendaDeferred},
	}}
	got := w.QueuedAgenda()
	if len(got) != 2 {
		t.Fatalf("QueuedAgenda len = %d, want 2", len(got))
	}
	if got[0].ID != "a" || got[1].ID != "c" {
		t.Errorf("QueuedAgenda preserved order: %+v", got)
	}
}

func TestWalk_IsActive(t *testing.T) {
	cases := map[Status]bool{
		StatusActive:    true,
		StatusPaused:    false,
		StatusCompleted: false,
		StatusAbandoned: false,
	}
	for s, want := range cases {
		w := Walk{Status: s}
		if got := w.IsActive(); got != want {
			t.Errorf("status=%s: IsActive=%v, want %v", s, got, want)
		}
	}
}
