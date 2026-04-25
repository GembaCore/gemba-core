package persona

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
	corepersona "github.com/MikeBengtson/gemba/internal/core/persona"
)

// validatingFakeSkill extends fakeSkill with controllable validators
// so dispatcher tests can drive both happy and error paths without
// importing internal/skills/epic_order.
type validatingFakeSkill struct {
	fakeSkill
	inputErr   error
	outputErr  error // returned for any line whose payload contains "bad":true
}

func (s validatingFakeSkill) ValidateInput(raw json.RawMessage) (any, error) {
	if s.inputErr != nil {
		return nil, s.inputErr
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s validatingFakeSkill) ValidateOutputLine(raw json.RawMessage) (any, error) {
	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, err
	}
	if bad, ok := probe["bad"].(bool); ok && bad {
		return nil, errors.New("schema mismatch: bad line")
	}
	if s.outputErr != nil {
		return nil, s.outputErr
	}
	return probe, nil
}

func dispatcherPersona() *corepersona.Persona {
	return &corepersona.Persona{
		ID:           "project-manager",
		Name:         "PM",
		Role:         "Project Manager",
		Variety:      corepersona.VarietyCoach,
		Scope:        corepersona.PersonaScope{Kind: corepersona.ScopeProject},
		Skills:       []string{"epic_order"},
		SystemPrompt: "You are {{role}} for {{workspace_name}}.",
	}
}

func dispatcherSkill() validatingFakeSkill {
	return validatingFakeSkill{
		fakeSkill: fakeSkill{id: "epic_order", prompt: "rank epics"},
	}
}

func newTestDispatcher(t *testing.T, opts ...DispatcherOption) *Dispatcher {
	t.Helper()
	log := NewAuditLog(t.TempDir())
	repos := core.NewRepositoryRegistry()
	_ = repos.Register(&core.Repository{
		ID:            "gemba",
		Path:          "/repos/gemba",
		DefaultBranch: "main",
		BeadPrefix:    "gm",
	})
	// Deterministic time + ids so tests can assert exact audit-log
	// partitioning and don't race on rand.
	defaultOpts := []DispatcherOption{
		WithClock(func() time.Time {
			return time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
		}),
		WithIDFunc(func() string { return "consult-test-1" }),
		WithWorkspaceDir("/work/gemba"),
		WithRepositoryRegistry(repos),
	}
	d := NewDispatcher(log, append(defaultOpts, opts...)...)
	return d
}

func validBeginRequest() BeginRequest {
	return BeginRequest{
		Persona:   dispatcherPersona(),
		Skill:     dispatcherSkill(),
		Workspace: "gemba",
		RawInput:  json.RawMessage(`{"workspace":"gemba"}`),
		Template:  TemplateValues{WorkspaceName: "Gemba"},
	}
}

func TestDispatcher_BeginRegistersSession(t *testing.T) {
	d := newTestDispatcher(t)
	c, err := d.Begin(validBeginRequest())
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if c.ID != "consult-test-1" {
		t.Errorf("ID = %q, want consult-test-1", c.ID)
	}
	// gm-k2jn: project-scope persona resolves to workspaceDir.
	if c.WorkingDir != "/work/gemba" {
		t.Errorf("WorkingDir = %q, want /work/gemba", c.WorkingDir)
	}
	if c.RepositoryID != "" {
		t.Errorf("RepositoryID = %q, want empty for project-scope", c.RepositoryID)
	}
	if c.PersonaID != "project-manager" || c.SkillID != "epic_order" {
		t.Errorf("ids: %+v", c)
	}
	if c.Status != StatusRunning {
		t.Errorf("status = %q, want running", c.Status)
	}
	if !strings.Contains(c.Composed.System, "Project Manager") {
		t.Errorf("composed system missing role substitution:\n%s", c.Composed.System)
	}
	if !strings.Contains(c.Composed.System, "Gemba") {
		t.Errorf("composed system missing workspace substitution:\n%s", c.Composed.System)
	}

	got, ok := d.Get(c.ID)
	if !ok || got != c {
		t.Errorf("Get returned (%v, %v); want same pointer back", got, ok)
	}
}

func TestDispatcher_BeginValidates(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*BeginRequest)
		wantSub string
	}{
		{
			name:    "nil persona",
			mutate:  func(r *BeginRequest) { r.Persona = nil },
			wantSub: "persona must not be nil",
		},
		{
			name:    "nil skill",
			mutate:  func(r *BeginRequest) { r.Skill = nil },
			wantSub: "skill must not be nil",
		},
		{
			name:    "empty workspace",
			mutate:  func(r *BeginRequest) { r.Workspace = "" },
			wantSub: "workspace must not be empty",
		},
		{
			name:    "empty raw input",
			mutate:  func(r *BeginRequest) { r.RawInput = nil },
			wantSub: "raw_input must not be empty",
		},
		{
			name: "persona not authorized for skill",
			mutate: func(r *BeginRequest) {
				p := dispatcherPersona()
				p.Skills = []string{"different_skill"}
				r.Persona = p
			},
			wantSub: "not authorized to invoke skill",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := newTestDispatcher(t)
			req := validBeginRequest()
			c.mutate(&req)
			_, err := d.Begin(req)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("err = %v, want substring %q", err, c.wantSub)
			}
		})
	}
}

func TestDispatcher_BeginRejectsSkillValidationFailure(t *testing.T) {
	d := newTestDispatcher(t)
	skill := validatingFakeSkill{
		fakeSkill: fakeSkill{id: "epic_order"},
		inputErr:  errors.New("missing candidate_epics"),
	}
	req := validBeginRequest()
	req.Skill = skill
	_, err := d.Begin(req)
	if err == nil {
		t.Fatal("expected validate-input failure")
	}
	if !strings.Contains(err.Error(), "validate input") {
		t.Errorf("err = %v, want validate-input wrap", err)
	}
}

func TestDispatcher_BeginFillsRoleFromPersona(t *testing.T) {
	d := newTestDispatcher(t)
	req := validBeginRequest()
	// Caller leaves Template.Role empty — dispatcher should
	// substitute persona.Role.
	req.Template = TemplateValues{WorkspaceName: "Gemba"}
	c, err := d.Begin(req)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if !strings.Contains(c.Composed.System, "Project Manager") {
		t.Errorf("role substitution missing:\n%s", c.Composed.System)
	}
}

func TestDispatcher_ReceiveValidLines(t *testing.T) {
	d := newTestDispatcher(t)
	c, _ := d.Begin(validBeginRequest())

	lines := []json.RawMessage{
		json.RawMessage(`{"type":"strategy","reasoning":"x"}`),
		json.RawMessage(`{"type":"recommendation","rank":0,"epic_id":"gm-e3"}`),
	}
	if err := d.Receive(c.ID, lines); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	got, _ := d.Get(c.ID)
	if len(got.ValidatedLines) != 2 {
		t.Errorf("ValidatedLines = %d, want 2", len(got.ValidatedLines))
	}
	if len(got.LineErrors) != 0 {
		t.Errorf("LineErrors = %d, want 0", len(got.LineErrors))
	}
}

func TestDispatcher_ReceiveBadLineRecordedAsLineError(t *testing.T) {
	d := newTestDispatcher(t)
	c, _ := d.Begin(validBeginRequest())

	// Mix one good + one bad line. The bad line is recorded as a
	// LineError; the good line still lands in ValidatedLines.
	lines := []json.RawMessage{
		json.RawMessage(`{"type":"strategy","reasoning":"x"}`),
		json.RawMessage(`{"type":"recommendation","bad":true}`),
	}
	if err := d.Receive(c.ID, lines); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	got, _ := d.Get(c.ID)
	if len(got.ValidatedLines) != 1 {
		t.Errorf("ValidatedLines = %d, want 1", len(got.ValidatedLines))
	}
	if len(got.LineErrors) != 1 {
		t.Fatalf("LineErrors = %d, want 1", len(got.LineErrors))
	}
	if !strings.Contains(got.LineErrors[0].Reason, "bad line") {
		t.Errorf("LineError reason = %q", got.LineErrors[0].Reason)
	}
}

func TestDispatcher_ReceiveOnUnknownConsult(t *testing.T) {
	d := newTestDispatcher(t)
	err := d.Receive("nope", []json.RawMessage{json.RawMessage(`{}`)})
	if err == nil || !strings.Contains(err.Error(), "unknown consult_id") {
		t.Errorf("err = %v, want unknown consult_id", err)
	}
}

func TestDispatcher_ReceiveAfterFinishFails(t *testing.T) {
	d := newTestDispatcher(t)
	c, _ := d.Begin(validBeginRequest())
	if _, err := d.Finish(c.ID, FinishInfo{}); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	err := d.Receive(c.ID, []json.RawMessage{json.RawMessage(`{}`)})
	if err == nil || !strings.Contains(err.Error(), "unknown consult_id") {
		t.Errorf("err = %v, want unknown consult_id (consult removed after Finish)", err)
	}
}

func TestDispatcher_ReceiveValidates(t *testing.T) {
	d := newTestDispatcher(t)
	c, _ := d.Begin(validBeginRequest())

	if err := d.Receive("", []json.RawMessage{json.RawMessage(`{}`)}); err == nil {
		t.Error("empty consult_id should error")
	}
	if err := d.Receive(c.ID, nil); err == nil {
		t.Error("empty lines should error")
	}
}

func TestDispatcher_FinishWritesAuditLogAndRemovesSession(t *testing.T) {
	d := newTestDispatcher(t)
	c, _ := d.Begin(validBeginRequest())
	_ = d.Receive(c.ID, []json.RawMessage{
		json.RawMessage(`{"type":"strategy","reasoning":"x"}`),
	})

	rec, err := d.Finish(c.ID, FinishInfo{
		Tokens:    corepersona.TokenUsage{In: 100, Out: 50},
		Dollars:   0.0123,
		LatencyMs: 4210,
		Model:     "claude-opus-4-7",
	})
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if rec.ID != c.ID {
		t.Errorf("rec.ID = %q, want %q", rec.ID, c.ID)
	}
	if rec.Tokens.In != 100 || rec.Tokens.Out != 50 {
		t.Errorf("tokens not propagated: %+v", rec.Tokens)
	}
	if rec.Dollars != 0.0123 {
		t.Errorf("dollars not propagated: %v", rec.Dollars)
	}

	// Session removed from the in-memory registry.
	if _, ok := d.Get(c.ID); ok {
		t.Error("session should be removed after Finish")
	}

	// Audit log persisted the row.
	got, err := d.auditLog.Get(c.ID)
	if err != nil {
		t.Fatalf("audit Get: %v", err)
	}
	if got.PersonaID != "project-manager" {
		t.Errorf("audit row persona = %q", got.PersonaID)
	}
	// Response field round-trips: lines + diagnostics.
	var resp struct {
		Lines       []map[string]any `json:"lines"`
		LineErrors  []LineError      `json:"line_errors,omitempty"`
		Diagnostics any              `json:"diagnostics,omitempty"`
	}
	if err := json.Unmarshal(got.Response, &resp); err != nil {
		t.Fatalf("decode audit response: %v", err)
	}
	if len(resp.Lines) != 1 {
		t.Errorf("audit response lines = %d, want 1", len(resp.Lines))
	}
}

func TestDispatcher_FinishWithErrorMarksFailed(t *testing.T) {
	d := newTestDispatcher(t)
	c, _ := d.Begin(validBeginRequest())
	rec, err := d.Finish(c.ID, FinishInfo{Error: "spawn died"})
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if rec.Error != "spawn died" {
		t.Errorf("audit row error = %q", rec.Error)
	}
	// The Consult.Status was set before delete; we can't read it
	// from the registry anymore. Read it from the audit row's Error.
	if rec.Error == "" {
		t.Error("error field should round-trip to audit row")
	}
}

func TestDispatcher_FinishUnknownConsult(t *testing.T) {
	d := newTestDispatcher(t)
	_, err := d.Finish("nope", FinishInfo{})
	if err == nil || !strings.Contains(err.Error(), "unknown consult_id") {
		t.Errorf("err = %v", err)
	}
}

func TestDispatcher_FinishTwiceFails(t *testing.T) {
	d := newTestDispatcher(t)
	c, _ := d.Begin(validBeginRequest())
	if _, err := d.Finish(c.ID, FinishInfo{}); err != nil {
		t.Fatalf("first Finish: %v", err)
	}
	if _, err := d.Finish(c.ID, FinishInfo{}); err == nil {
		t.Error("second Finish should fail (session removed)")
	}
}

func TestDispatcher_FinishUsesNowWhenEndedAtZero(t *testing.T) {
	d := newTestDispatcher(t)
	c, _ := d.Begin(validBeginRequest())
	rec, err := d.Finish(c.ID, FinishInfo{})
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if rec.EndedAt.IsZero() {
		t.Error("EndedAt should default to dispatcher.now()")
	}
	if !rec.EndedAt.Equal(rec.StartedAt) {
		// The fake clock returns a fixed time, so started == ended.
		t.Errorf("EndedAt %v, StartedAt %v", rec.EndedAt, rec.StartedAt)
	}
}

func TestDispatcher_FinishPropagatesEndedAt(t *testing.T) {
	d := newTestDispatcher(t)
	c, _ := d.Begin(validBeginRequest())
	override := time.Date(2026, 4, 26, 9, 0, 0, 0, time.UTC)
	rec, err := d.Finish(c.ID, FinishInfo{EndedAt: override})
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if !rec.EndedAt.Equal(override) {
		t.Errorf("EndedAt = %v, want %v", rec.EndedAt, override)
	}
}

func TestDispatcher_NoAuditLogStillReturnsRecord(t *testing.T) {
	// In-test consults that don't want audit-log persistence pass
	// nil; Finish still produces a record so callers can echo it.
	d := NewDispatcher(nil,
		WithClock(func() time.Time { return time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC) }),
		WithIDFunc(func() string { return "c-x" }),
		WithWorkspaceDir("/work/gemba"),
	)
	c, err := d.Begin(validBeginRequest())
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	rec, err := d.Finish(c.ID, FinishInfo{})
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if rec == nil || rec.ID != c.ID {
		t.Errorf("rec = %+v", rec)
	}
}

func TestDispatcher_ListNewestFirst(t *testing.T) {
	times := []time.Time{
		time.Date(2026, 4, 25, 9, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 25, 11, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 25, 10, 0, 0, 0, time.UTC),
	}
	idx := 0
	idCounter := 0
	d := NewDispatcher(nil,
		WithClock(func() time.Time {
			t := times[idx]
			idx++
			return t
		}),
		WithIDFunc(func() string {
			idCounter++
			return "c-" + string(rune('a'+idCounter-1))
		}),
		WithWorkspaceDir("/work/gemba"),
	)
	for range times {
		if _, err := d.Begin(validBeginRequest()); err != nil {
			t.Fatalf("Begin: %v", err)
		}
	}
	got := d.List()
	if len(got) != 3 {
		t.Fatalf("List = %d, want 3", len(got))
	}
	// Sorted newest-first: 11:00 (c-b), 10:00 (c-c), 09:00 (c-a)
	wantOrder := []string{"c-b", "c-c", "c-a"}
	for i, c := range got {
		if c.ID != wantOrder[i] {
			t.Errorf("[%d] = %q, want %q", i, c.ID, wantOrder[i])
		}
	}
}

// gm-k2jn: scope=repository persona binds to the named repo's Path.
func TestDispatcher_BeginResolvesRepositoryScope(t *testing.T) {
	d := newTestDispatcher(t)
	req := validBeginRequest()
	p := dispatcherPersona()
	p.Scope = corepersona.PersonaScope{
		Kind:         corepersona.ScopeRepository,
		RepositoryID: "gemba",
	}
	req.Persona = p
	c, err := d.Begin(req)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if c.WorkingDir != "/repos/gemba" {
		t.Errorf("WorkingDir = %q, want /repos/gemba", c.WorkingDir)
	}
	if c.RepositoryID != "gemba" {
		t.Errorf("RepositoryID = %q, want gemba", c.RepositoryID)
	}
}

// scope=repository against an unregistered repo fails Begin.
func TestDispatcher_BeginRejectsUnregisteredRepo(t *testing.T) {
	d := newTestDispatcher(t)
	req := validBeginRequest()
	p := dispatcherPersona()
	p.Scope = corepersona.PersonaScope{
		Kind:         corepersona.ScopeRepository,
		RepositoryID: "ghost",
	}
	req.Persona = p
	_, err := d.Begin(req)
	if err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Errorf("err = %v, want unregistered-repo error", err)
	}
}

// scope=any consult uses RepositoryOverride and surfaces the chosen
// repo on the Consult.
func TestDispatcher_BeginUsesRepositoryOverrideForScopeAny(t *testing.T) {
	d := newTestDispatcher(t)
	req := validBeginRequest()
	p := dispatcherPersona()
	p.Scope = corepersona.PersonaScope{Kind: corepersona.ScopeAny}
	req.Persona = p
	req.RepositoryOverride = "gemba"
	c, err := d.Begin(req)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if c.WorkingDir != "/repos/gemba" {
		t.Errorf("WorkingDir = %q, want /repos/gemba", c.WorkingDir)
	}
	if c.RepositoryID != "gemba" {
		t.Errorf("RepositoryID = %q, want gemba", c.RepositoryID)
	}
}

// scope=any without an override fails Begin.
func TestDispatcher_BeginRejectsScopeAnyWithoutOverride(t *testing.T) {
	d := newTestDispatcher(t)
	req := validBeginRequest()
	p := dispatcherPersona()
	p.Scope = corepersona.PersonaScope{Kind: corepersona.ScopeAny}
	req.Persona = p
	_, err := d.Begin(req)
	if err == nil || !strings.Contains(err.Error(), "requires a repository override") {
		t.Errorf("err = %v, want override-required error", err)
	}
}

func TestDispatcher_DefaultIDFormat(t *testing.T) {
	// Sanity: defaultConsultID returns a stable shape so the
	// AuditLog's path-separator validation (which rejects '/' in
	// ids) never trips on a real dispatcher-generated id.
	id := defaultConsultID()
	if !strings.HasPrefix(id, "consult-") {
		t.Errorf("id %q missing consult- prefix", id)
	}
	if strings.ContainsAny(id, `/\`) {
		t.Errorf("id %q contains path separator", id)
	}
}
