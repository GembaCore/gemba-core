package epic_order

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/core/persona"
)

// minimalInput returns a JSON body that ValidateInput accepts. Tests
// mutate this in place to exercise rejection paths.
func minimalInput() map[string]any {
	return map[string]any{
		"workspace":      "gemba",
		"workspace_name": "Gemba",
		"as_of":          "2026-04-25T00:00:00Z",
		"candidate_epics": []map[string]any{
			{
				"epic_id":  "gm-e3",
				"title":    "Plan view",
				"ui_state": "in_scope",
			},
		},
		"constraints": map[string]any{},
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return b
}

func TestSkill_StaticMetadata(t *testing.T) {
	s := New()
	if s.ID() != ID {
		t.Errorf("ID = %q, want %q", s.ID(), ID)
	}
	if s.Name() == "" {
		t.Error("Name must not be empty")
	}
	if s.Description() == "" {
		t.Error("Description must not be empty")
	}
	if s.SkillPrompt() == "" {
		t.Error("SkillPrompt must not be empty")
	}
	if got := s.OutputTool(); got.Name != "emit_ordering" {
		t.Errorf("OutputTool.Name = %q, want emit_ordering", got.Name)
	}
	if (persona.ToolSpec{}).IsZero() != true {
		t.Error("zero ToolSpec should report IsZero (interface guard)")
	}
}

func TestSkill_RegisterRoundtrip(t *testing.T) {
	r := persona.NewSkillRegistry()
	if err := Register(r); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, ok := r.Get(ID)
	if !ok {
		t.Fatalf("registry missing %q after Register", ID)
	}
	if got.ID() != ID {
		t.Errorf("registered skill ID = %q, want %q", got.ID(), ID)
	}
}

func TestValidateInput_Accepts(t *testing.T) {
	s := New()
	out, err := s.ValidateInput(mustJSON(t, minimalInput()))
	if err != nil {
		t.Fatalf("ValidateInput: %v", err)
	}
	in, ok := out.(*EpicOrderInput)
	if !ok {
		t.Fatalf("ValidateInput returned %T, want *EpicOrderInput", out)
	}
	if in.Workspace != "gemba" || in.WorkspaceName != "Gemba" {
		t.Errorf("workspace fields = %+v", in)
	}
	if in.AsOf.IsZero() {
		t.Error("as_of did not parse")
	}
	if len(in.CandidateEpics) != 1 || in.CandidateEpics[0].EpicID != "gm-e3" {
		t.Errorf("candidate_epics = %+v", in.CandidateEpics)
	}
}

func TestValidateInput_Rejects(t *testing.T) {
	type tc struct {
		name    string
		mutate  func(m map[string]any)
		wantSub string
	}
	cases := []tc{
		{
			name:    "empty workspace",
			mutate:  func(m map[string]any) { m["workspace"] = "" },
			wantSub: "workspace must not be empty",
		},
		{
			name:    "empty workspace_name",
			mutate:  func(m map[string]any) { m["workspace_name"] = "" },
			wantSub: "workspace_name",
		},
		{
			name:    "missing as_of",
			mutate:  func(m map[string]any) { delete(m, "as_of") },
			wantSub: "as_of",
		},
		{
			name:    "no candidates",
			mutate:  func(m map[string]any) { m["candidate_epics"] = []any{} },
			wantSub: "candidate_epics must not be empty",
		},
		{
			name: "candidate missing epic_id",
			mutate: func(m map[string]any) {
				m["candidate_epics"] = []map[string]any{
					{"epic_id": "", "title": "x", "ui_state": "in_scope"},
				}
			},
			wantSub: "epic_id must not be empty",
		},
		{
			name: "candidate missing title",
			mutate: func(m map[string]any) {
				m["candidate_epics"] = []map[string]any{
					{"epic_id": "gm-e1", "title": "", "ui_state": "in_scope"},
				}
			},
			wantSub: "title must not be empty",
		},
		{
			name: "candidate missing ui_state",
			mutate: func(m map[string]any) {
				m["candidate_epics"] = []map[string]any{
					{"epic_id": "gm-e1", "title": "x", "ui_state": ""},
				}
			},
			wantSub: "ui_state must not be empty",
		},
		{
			name: "negative max_recommendations",
			mutate: func(m map[string]any) {
				m["max_recommendations"] = -1
			},
			wantSub: "max_recommendations",
		},
		{
			name: "min_confidence > 1",
			mutate: func(m map[string]any) {
				m["constraints"] = map[string]any{"min_confidence": 1.5}
			},
			wantSub: "min_confidence",
		},
		{
			name: "burn_thresholds out of order",
			mutate: func(m map[string]any) {
				m["sprint_context"] = map[string]any{
					"started_at":   "2026-04-25T00:00:00Z",
					"token_budget": 100,
					"tokens_used":  50,
					"burn_thresholds": map[string]any{
						"inform": 0.9, "warn": 0.5, "stop": 0.99,
					},
				}
			},
			wantSub: "burn_thresholds",
		},
		{
			name: "unknown top-level field",
			mutate: func(m map[string]any) {
				m["typo_field"] = "anything"
			},
			wantSub: "decode input",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := New()
			body := minimalInput()
			c.mutate(body)
			_, err := s.ValidateInput(mustJSON(t, body))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("error = %v, want substring %q", err, c.wantSub)
			}
		})
	}
}

func TestValidateInput_EmptyBody(t *testing.T) {
	s := New()
	if _, err := s.ValidateInput(nil); err == nil {
		t.Error("expected error on nil input")
	}
}

func TestValidateOutputLine_Strategy(t *testing.T) {
	s := New()
	body := mustJSON(t, map[string]any{
		"type":             "strategy",
		"workspace":        "gemba",
		"as_of":            time.Now().UTC().Format(time.RFC3339),
		"model":            "claude-opus-4-7",
		"reasoning":        "Sprint at 60% — trim.",
		"total_considered": 8,
		"total_ranked":     5,
	})
	got, err := s.ValidateOutputLine(body)
	if err != nil {
		t.Fatalf("ValidateOutputLine: %v", err)
	}
	if _, ok := got.(*StrategyLine); !ok {
		t.Fatalf("got %T, want *StrategyLine", got)
	}
}

func TestValidateOutputLine_Recommendation(t *testing.T) {
	s := New()
	body := mustJSON(t, map[string]any{
		"type":       "recommendation",
		"rank":       0,
		"epic_id":    "gm-e3",
		"confidence": 0.9,
		"rationale":  "Unblocks four downstream Epics.",
	})
	got, err := s.ValidateOutputLine(body)
	if err != nil {
		t.Fatalf("ValidateOutputLine: %v", err)
	}
	rec, ok := got.(*RecommendationLine)
	if !ok {
		t.Fatalf("got %T, want *RecommendationLine", got)
	}
	if rec.Rank != 0 || rec.EpicID != "gm-e3" {
		t.Errorf("rec = %+v", rec)
	}
}

func TestValidateOutputLine_Rejects(t *testing.T) {
	cases := []struct {
		name    string
		body    map[string]any
		wantSub string
	}{
		{
			name:    "unknown type",
			body:    map[string]any{"type": "bogus"},
			wantSub: "unknown line type",
		},
		{
			name: "strategy missing reasoning",
			body: map[string]any{
				"type":             "strategy",
				"workspace":        "gemba",
				"as_of":            "2026-04-25T00:00:00Z",
				"model":            "claude-opus-4-7",
				"reasoning":        "",
				"total_considered": 1,
				"total_ranked":     1,
			},
			wantSub: "reasoning",
		},
		{
			name: "recommendation confidence > 1",
			body: map[string]any{
				"type":       "recommendation",
				"rank":       0,
				"epic_id":    "gm-e3",
				"confidence": 1.5,
				"rationale":  "x",
			},
			wantSub: "confidence",
		},
		{
			name: "recommendation negative rank",
			body: map[string]any{
				"type":       "recommendation",
				"rank":       -1,
				"epic_id":    "gm-e3",
				"confidence": 0.5,
				"rationale":  "x",
			},
			wantSub: "rank",
		},
		{
			name: "warning missing detail",
			body: map[string]any{
				"type":   "warning",
				"kind":   "parallel_conflict",
				"detail": "",
			},
			wantSub: "detail",
		},
		{
			name: "deferred missing reason",
			body: map[string]any{
				"type":    "deferred",
				"epic_id": "gm-e9",
				"reason":  "",
			},
			wantSub: "reason",
		},
		{
			name: "summary unknown extra field",
			body: map[string]any{
				"type":                 "summary",
				"ranked_count":         1,
				"advisor_cost_dollars": 0.01,
				"tokens_in":            10,
				"tokens_out":           5,
				"latency_ms":           100,
				"model":                "claude-opus-4-7",
				"sneaky_field":         "value",
			},
			wantSub: "decode line",
		},
	}
	s := New()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body, err := json.Marshal(c.body)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			_, err = s.ValidateOutputLine(body)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("error = %v, want substring %q", err, c.wantSub)
			}
		})
	}
}

func TestValidateOutputLine_EmptyAndNoType(t *testing.T) {
	s := New()
	if _, err := s.ValidateOutputLine(nil); err == nil {
		t.Error("expected error on nil line")
	}
	if _, err := s.ValidateOutputLine(json.RawMessage(`{}`)); err == nil {
		t.Error("expected error on empty object (no type)")
	}
}

func TestSkill_SatisfiesPersonaSkillInterface(t *testing.T) {
	// Compile-time check: the package's *Skill must satisfy
	// persona.Skill. Without this, a refactor of either side could
	// silently drift.
	var _ persona.Skill = (*Skill)(nil)
}
