package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GembaCore/gemba-core/internal/config"
	"github.com/GembaCore/gemba-core/internal/persona"
	"github.com/GembaCore/gemba-core/internal/skills/epic_order"
)

// applyTestRouter spins a Router with one running consult and two
// validated lines so apply tests have something to record against.
func applyTestRouter(t *testing.T) (*Router, string) {
	t.Helper()
	r, p := newConsultsPostRouter(t)
	skill, _ := r.skillRegistry.Get(epic_order.ID)
	c, err := r.personaDispatcher.Begin(persona.BeginRequest{
		Persona:   p,
		Skill:     skill,
		Workspace: "gemba",
		RawInput:  epicOrderRawInput(t),
	})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := r.personaDispatcher.Receive(c.ID, []json.RawMessage{
		json.RawMessage(`{"type":"strategy","workspace":"gemba","as_of":"2026-04-26T00:00:00Z","model":"test","reasoning":"r","total_considered":1,"total_ranked":1}`),
		json.RawMessage(`{"type":"recommendation","rank":1,"epic_id":"gm-1","confidence":0.9,"rationale":"top"}`),
	}); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	return r, c.ID
}

func postApply(t *testing.T, r *Router, consultID string, idx int, nonce string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost,
		"/api/consults/"+consultID+"/apply/"+itoa(idx), nil)
	if nonce != "" {
		req.Header.Set(ConfirmHeader, nonce)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func TestApplyConsult_HappyPathReturns200WithLine(t *testing.T) {
	r, id := applyTestRouter(t)
	rec := postApply(t, r, id, 1, "nonce-apply-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body applyResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.ConsultID != id {
		t.Errorf("consult_id = %q, want %q", body.ConsultID, id)
	}
	if body.Idx != 1 {
		t.Errorf("idx = %d, want 1", body.Idx)
	}
	if body.Line == nil {
		t.Error("line nil; want the validated entry at idx 1")
	}
	if len(body.AppliedIdx) != 1 || body.AppliedIdx[0] != 1 {
		t.Errorf("applied_idx = %v, want [1]", body.AppliedIdx)
	}
}

func TestApplyConsult_DuplicateIdxReturns409(t *testing.T) {
	r, id := applyTestRouter(t)
	if rec := postApply(t, r, id, 0, "nonce-apply-dup-1"); rec.Code != http.StatusOK {
		t.Fatalf("first apply: %d", rec.Code)
	}
	rec := postApply(t, r, id, 0, "nonce-apply-dup-2")
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 (already recorded)", rec.Code)
	}
}

func TestApplyConsult_OutOfRangeReturns400(t *testing.T) {
	r, id := applyTestRouter(t)
	rec := postApply(t, r, id, 99, "nonce-apply-oor")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestApplyConsult_UnknownConsultReturns404(t *testing.T) {
	r, _ := applyTestRouter(t)
	rec := postApply(t, r, "no-such-consult", 0, "nonce-apply-unknown")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestApplyConsult_BeforeAttachReturns503(t *testing.T) {
	r := NewRouter(config.ServeConfig{}, fakeSPA(), nil)
	rec := postApply(t, r, "any-id", 0, "nonce-pre-attach")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestApplyConsult_NonceReplayReturnsCachedResponse(t *testing.T) {
	r, id := applyTestRouter(t)
	first := postApply(t, r, id, 0, "nonce-apply-replay")
	if first.Code != http.StatusOK {
		t.Fatalf("first: %d", first.Code)
	}
	second := postApply(t, r, id, 0, "nonce-apply-replay")
	if second.Code != http.StatusOK {
		t.Errorf("replay: status = %d, want 200 from cache", second.Code)
	}
	// AppliedIdx should still be just [0] — the replay must NOT
	// have re-recorded a duplicate apply.
	c, _ := r.personaDispatcher.Get(id)
	if len(c.AppliedIdx) != 1 {
		t.Errorf("AppliedIdx = %v after replay, want [0]", c.AppliedIdx)
	}
}

func TestApplyConsult_NonIntegerIdxReturns400(t *testing.T) {
	r, id := applyTestRouter(t)
	req := httptest.NewRequest(http.MethodPost,
		"/api/consults/"+id+"/apply/notanumber", nil)
	req.Header.Set(ConfirmHeader, "nonce-apply-bad-idx")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
