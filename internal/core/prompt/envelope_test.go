package prompt

import (
	"strings"
	"testing"
)

// fixedCounter returns a hard-coded token count per call, in sequence.
// Tests that need to force truncation wire this in so budget decisions
// don't depend on the Heuristic's string length math.
type fixedCounter struct {
	costs map[string]int
}

func (f fixedCounter) Count(s string) int {
	if c, ok := f.costs[s]; ok {
		return c
	}
	// Fall back to 1 per rune so unknown strings (wrappers) still round-trip.
	n := 0
	for range s {
		n++
	}
	return n
}

func TestEmptyEnvelopeProducesEmptyOutput(t *testing.T) {
	c := Envelope{}.Compose(ComposeOptions{})
	if c.Text != "" {
		t.Errorf("empty envelope text = %q, want empty", c.Text)
	}
	if len(c.Layers) != 0 {
		t.Errorf("empty envelope layers = %d, want 0", len(c.Layers))
	}
	if c.TotalTokens != 0 {
		t.Errorf("empty envelope tokens = %d, want 0", c.TotalTokens)
	}
}

func TestLayersRenderInOuterToInnerOrder(t *testing.T) {
	e := Envelope{
		ProjectValues:     []string{"innovation"},
		ProjectGuardrails: []string{"never ship without tests"},
		ProjectGoals:      []string{"ship PromptEnvelope"},
		WorkspaceValues:   []string{"terse output"},
		PersonaSystem:     "You are the PM.",
		Skill:             "Order these epics.",
		ContextChunks:     []string{"Epic A: foo", "Epic B: bar"},
		UserInput:         "What's next?",
	}
	c := e.Compose(ComposeOptions{})

	want := []Layer{
		LayerProjectValues,
		LayerProjectGuardrails,
		LayerProjectGoals,
		LayerWorkspaceValues,
		LayerPersonaSystem,
		LayerSkill,
		LayerContext,
		LayerUserInput,
	}
	if len(c.Layers) != len(want) {
		t.Fatalf("got %d layers, want %d", len(c.Layers), len(want))
	}
	for i, w := range want {
		if c.Layers[i].Layer != w {
			t.Errorf("layer[%d] = %q, want %q", i, c.Layers[i].Layer, w)
		}
	}

	// Text must contain tags in order.
	prev := -1
	for _, w := range want {
		idx := strings.Index(c.Text, "<"+layerTag(w)+">")
		if idx < 0 {
			t.Errorf("missing tag for %q in composed text", w)
			continue
		}
		if idx <= prev {
			t.Errorf("tag for %q at %d is not after prev %d", w, idx, prev)
		}
		prev = idx
	}
}

func TestEachLayerIndependentlyNullable(t *testing.T) {
	// Only persona set — other layers should be absent from output.
	e := Envelope{PersonaSystem: "You are the PM."}
	c := e.Compose(ComposeOptions{})
	if got := len(c.Layers); got != 1 {
		t.Fatalf("layers=%d want 1", got)
	}
	if c.Layers[0].Layer != LayerPersonaSystem {
		t.Errorf("layer=%q want %q", c.Layers[0].Layer, LayerPersonaSystem)
	}
	if !strings.Contains(c.Text, "<persona-system>") {
		t.Errorf("text missing persona-system tag: %q", c.Text)
	}
	if strings.Contains(c.Text, "<project-values>") {
		t.Errorf("text should not contain project-values tag: %q", c.Text)
	}
}

func TestBudgetTruncationDropsOldestFirst(t *testing.T) {
	// Each item costs 10 tokens; wrapper overhead is 8; budget is 30.
	// That fits wrapper(8) + two items (20) = 28. Oldest of three is dropped.
	counter := fixedCounter{costs: map[string]int{
		"old":    10,
		"middle": 10,
		"new":    10,
	}}
	e := Envelope{ContextChunks: []string{"old", "middle", "new"}}
	budget := Budget{LayerContext: 30}
	c := e.Compose(ComposeOptions{Budget: budget, Counter: counter})

	if len(c.Layers) != 1 {
		t.Fatalf("layers=%d want 1", len(c.Layers))
	}
	lo := c.Layers[0]
	if lo.DroppedItems != 1 {
		t.Errorf("DroppedItems=%d want 1", lo.DroppedItems)
	}
	if !lo.Truncated {
		t.Error("Truncated=false, want true")
	}
	if strings.Contains(c.Text, "old") {
		t.Error("oldest item 'old' should have been dropped")
	}
	if !strings.Contains(c.Text, "middle") || !strings.Contains(c.Text, "new") {
		t.Errorf("expected middle+new in text, got %q", c.Text)
	}
}

func TestBudgetZeroMeansUnbounded(t *testing.T) {
	big := strings.Repeat("x", 10_000)
	e := Envelope{UserInput: big}
	// Default budget for user input is 0 = unbounded.
	c := e.Compose(ComposeOptions{})
	if !strings.Contains(c.Text, big) {
		t.Error("user input should be preserved in full when budget is 0")
	}
	for _, lo := range c.Layers {
		if lo.Truncated {
			t.Errorf("layer %q truncated unexpectedly", lo.Layer)
		}
	}
}

func TestSingleItemOverBudgetStillEmits(t *testing.T) {
	counter := fixedCounter{costs: map[string]int{"huge": 1000}}
	e := Envelope{ContextChunks: []string{"huge"}}
	budget := Budget{LayerContext: 10}
	c := e.Compose(ComposeOptions{Budget: budget, Counter: counter})

	if len(c.Layers) != 1 {
		t.Fatalf("layers=%d want 1", len(c.Layers))
	}
	if !strings.Contains(c.Text, "huge") {
		t.Error("single over-budget item should still be emitted")
	}
	if c.Layers[0].DroppedItems != 0 {
		t.Errorf("DroppedItems=%d want 0 (only one item)", c.Layers[0].DroppedItems)
	}
}

func TestComposeDeterministic(t *testing.T) {
	e := Envelope{
		ProjectValues: []string{"innovation", "transparency"},
		PersonaSystem: "Persona text.",
		UserInput:     "Do the thing.",
	}
	a := e.Compose(ComposeOptions{})
	b := e.Compose(ComposeOptions{})
	if a.Text != b.Text {
		t.Error("Compose is not deterministic: different Text across identical calls")
	}
	if a.TotalTokens != b.TotalTokens {
		t.Errorf("Compose is not deterministic: tokens %d vs %d", a.TotalTokens, b.TotalTokens)
	}
}

func TestComposeOptionsOrderOverride(t *testing.T) {
	e := Envelope{
		ProjectValues: []string{"innovation"},
		UserInput:     "Do the thing.",
	}
	// Reverse: user input first, then values.
	c := e.Compose(ComposeOptions{Order: []Layer{LayerUserInput, LayerProjectValues}})
	if len(c.Layers) != 2 {
		t.Fatalf("layers=%d want 2", len(c.Layers))
	}
	if c.Layers[0].Layer != LayerUserInput {
		t.Errorf("first layer=%q want user_input", c.Layers[0].Layer)
	}
	if c.Layers[1].Layer != LayerProjectValues {
		t.Errorf("second layer=%q want project_values", c.Layers[1].Layer)
	}
	ui := strings.Index(c.Text, "<user-input>")
	pv := strings.Index(c.Text, "<project-values>")
	if ui < 0 || pv < 0 || ui > pv {
		t.Errorf("order override not reflected in text: ui=%d pv=%d", ui, pv)
	}
}

func TestBudgetOverrideMergesWithDefault(t *testing.T) {
	// Override project_values only; other layers keep defaults.
	b := Budget{LayerProjectValues: 42}
	merged := mergedBudget(b)
	if merged[LayerProjectValues] != 42 {
		t.Errorf("override not applied: got %d", merged[LayerProjectValues])
	}
	if merged[LayerContext] != DefaultBudget()[LayerContext] {
		t.Errorf("default not preserved for context: got %d want %d",
			merged[LayerContext], DefaultBudget()[LayerContext])
	}
	// mergedBudget must not mutate input.
	if len(b) != 1 {
		t.Errorf("input budget was mutated: len=%d", len(b))
	}
}

func TestWhitespaceItemsSkipped(t *testing.T) {
	e := Envelope{ProjectValues: []string{"", "   ", "\t\n"}}
	c := e.Compose(ComposeOptions{})
	if len(c.Layers) != 0 {
		t.Errorf("whitespace-only layer should be skipped, got %d layers", len(c.Layers))
	}
}

func TestLayerOutputRecordsTokensAndBudget(t *testing.T) {
	e := Envelope{UserInput: "hello"}
	c := e.Compose(ComposeOptions{})
	if len(c.Layers) != 1 {
		t.Fatalf("layers=%d", len(c.Layers))
	}
	lo := c.Layers[0]
	if lo.Tokens <= 0 {
		t.Errorf("Tokens=%d want > 0", lo.Tokens)
	}
	// Default budget for user_input is 0 (unbounded).
	if lo.Budget != 0 {
		t.Errorf("Budget=%d want 0", lo.Budget)
	}
	if lo.Truncated {
		t.Error("Truncated=true want false")
	}
}

func TestHeuristicCount(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"a", 1},     // ceil(1/4)
		{"abcd", 1},  // ceil(4/4)
		{"abcde", 2}, // ceil(5/4)
		{"1234567890abcdef", 4},
	}
	for _, c := range cases {
		if got := (Heuristic{}).Count(c.in); got != c.want {
			t.Errorf("Count(%q)=%d want %d", c.in, got, c.want)
		}
	}
}

func TestTotalTokensSumsLayers(t *testing.T) {
	e := Envelope{
		ProjectValues: []string{"v1"},
		UserInput:     "u1",
	}
	c := e.Compose(ComposeOptions{})
	var sum int
	for _, lo := range c.Layers {
		sum += lo.Tokens
	}
	if c.TotalTokens != sum {
		t.Errorf("TotalTokens=%d want sum(layers)=%d", c.TotalTokens, sum)
	}
}

func TestUnknownLayerSkipped(t *testing.T) {
	e := Envelope{UserInput: "hi"}
	c := e.Compose(ComposeOptions{Order: []Layer{"invented_layer", LayerUserInput}})
	if len(c.Layers) != 1 {
		t.Fatalf("layers=%d want 1 (unknown layer skipped)", len(c.Layers))
	}
	if c.Layers[0].Layer != LayerUserInput {
		t.Errorf("layer=%q want user_input", c.Layers[0].Layer)
	}
}

func TestSortedLayers(t *testing.T) {
	b := Budget{LayerUserInput: 1, LayerProjectValues: 2, LayerSkill: 3}
	got := SortedLayers(b)
	// Alphabetical order of the string forms: project_values, skill, user_input.
	want := []Layer{LayerProjectValues, LayerSkill, LayerUserInput}
	if len(got) != len(want) {
		t.Fatalf("len=%d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d]=%q want %q", i, got[i], want[i])
		}
	}
}

func TestDefaultBudgetMatchesDesign(t *testing.T) {
	d := DefaultBudget()
	cases := map[Layer]int{
		LayerProjectValues:     500,
		LayerProjectGuardrails: 500,
		LayerProjectGoals:      1000,
		LayerWorkspaceValues:   500,
		LayerPersonaSystem:     2000,
		LayerSkill:             2000,
		LayerContext:           4000,
		LayerUserInput:         0,
	}
	for l, want := range cases {
		if got := d[l]; got != want {
			t.Errorf("DefaultBudget[%q]=%d want %d", l, got, want)
		}
	}
}
