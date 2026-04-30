// start.go (gm-root.28.2) — session lifecycle for the mock plane.
//
// The autodispatch daemon (gm-s47n.12) calls StartSession with an
// assignment id and a SessionPrompt whose Extension carries
// gemba:bead_id (set by the daemon's dispatch logic). The mock
// plane:
//
//   1. Mints a Session{ID, AssignmentID, Status: SessionInitializing}.
//   2. Stores it in the in-memory map; ListSessions returns it
//      immediately so the daemon's view stays consistent.
//   3. Transitions to SessionWorking and spawns RunBead asynchronously.
//   4. On success → SessionReady (warm pool eligibility, gm-s47n.11).
//   5. On failure → SessionFailed.
//
// EndSession removes the session from the map; RecycleSession resets
// it to SessionReady (no-op for mock — there's no worktree to clean).

package mock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

const (
	extKeyBeadID      = "gemba:bead_id"
	extKeyPersonaID   = "gemba:persona_id"
	extKeyReusePaneID = "gemba:reuse_pane_id"
)

// StartSession implements core.OrchestrationPlaneAdaptor.StartSession.
// Returns immediately with the session in SessionWorking; the per-
// bead work runs in a background goroutine that updates status when
// done.
func (o *OrchestrationPlane) StartSession(ctx context.Context, assignmentID string, prompt core.SessionPrompt) (core.Session, error) {
	beadID := stringExtension(prompt.Extension, extKeyBeadID)
	if beadID == "" {
		return core.Session{}, core.NewAdaptorError(core.KindValidation,
			"mock: SessionPrompt.Extension must include %q", extKeyBeadID)
	}
	personaID := stringExtension(prompt.Extension, extKeyPersonaID)
	reusePaneID := stringExtension(prompt.Extension, extKeyReusePaneID)

	o.mu.Lock()
	now := time.Now().UTC()

	// Reuse path: if reuse_pane_id is set and we have a session
	// with that pane_id, transition it to Working with a new bead
	// instead of minting a fresh session. Mirrors the native
	// adaptor's warm-pool reuse semantics (gm-s47n.11).
	var sessionID string
	if reusePaneID != "" {
		for id, existing := range o.sessions {
			if pid, _ := existing.ProviderMetadata["pane_id"].(string); pid == reusePaneID {
				existing.Status = core.SessionWorking
				existing.AssignmentID = assignmentID
				existing.EndedAt = nil
				existing.ProviderMetadata["bead_id"] = beadID
				if personaID != "" {
					existing.Persona = personaID
				}
				o.sessions[id] = existing
				sessionID = id
				break
			}
		}
	}

	if sessionID == "" {
		// Mint a fresh session.
		o.nextSessionN++
		suffix := randomSuffix()
		sessionID = fmt.Sprintf("mock-sess-%d-%s", o.nextSessionN, suffix)
		paneID := fmt.Sprintf("mock-pane-%d-%s", o.nextSessionN, suffix)
		sess := core.Session{
			ID:           sessionID,
			AssignmentID: assignmentID,
			Status:       core.SessionWorking,
			StartedAt:    now,
			Persona:      personaID,
			ProviderMetadata: map[string]any{
				"adaptor": "mock",
				"pane_id": paneID,
				"bead_id": beadID,
				"started": now.Format(time.RFC3339),
			},
		}
		o.sessions[sessionID] = sess
	}
	sess := o.sessions[sessionID]
	projectDir := o.projectDir
	o.mu.Unlock()

	// Spawn the per-bead work asynchronously so StartSession returns
	// immediately. The daemon polls ListSessions to observe state
	// transitions.
	go o.runSession(ctx, sessionID, projectDir, beadID)

	return sess, nil
}

func (o *OrchestrationPlane) runSession(_ context.Context, sessionID, projectDir, beadID string) {
	log := func(msg string) {
		// Currently silent — daemon observes via session-status
		// polling. Hook into a real logger when needed.
		_ = msg
	}
	err := RunBead(context.Background(), projectDir, beadID, log)
	o.mu.Lock()
	defer o.mu.Unlock()
	sess, ok := o.sessions[sessionID]
	if !ok {
		return
	}
	if err != nil {
		sess.Status = core.SessionFailed
		reason := core.CloseProviderExit
		sess.CloseReason = &reason
		// Stash the error message in metadata so observers can see it.
		if sess.ProviderMetadata == nil {
			sess.ProviderMetadata = map[string]any{}
		}
		sess.ProviderMetadata["mock_error"] = err.Error()
	} else {
		sess.Status = core.SessionReady
	}
	endedAt := time.Now().UTC()
	sess.EndedAt = nil // SessionReady is not terminal; kept warm for recycle
	if sess.Status == core.SessionFailed {
		sess.EndedAt = &endedAt
	}
	o.sessions[sessionID] = sess
}

// EndSession removes the session from the map. Idempotent.
func (o *OrchestrationPlane) EndSession(_ context.Context, sessionID string, _ core.SessionEndMode, _ core.ConfirmNonce) (core.Session, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	sess, ok := o.sessions[sessionID]
	if !ok {
		return core.Session{}, core.NewAdaptorError(core.KindSessionNotFound,
			"mock: session %q unknown", sessionID)
	}
	now := time.Now().UTC()
	sess.Status = core.SessionCompleted
	sess.EndedAt = &now
	o.sessions[sessionID] = sess
	return sess, nil
}

// RecycleSession returns the session to SessionReady so the daemon's
// pool sees it as warm-and-eligible for the next bead. Mock has no
// worktree to clean; recycle is a state reset.
func (o *OrchestrationPlane) RecycleSession(_ context.Context, sessionID string) (core.Session, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	sess, ok := o.sessions[sessionID]
	if !ok {
		return core.Session{}, core.NewAdaptorError(core.KindSessionNotFound,
			"mock: session %q unknown", sessionID)
	}
	if sess.Status == core.SessionFailed || sess.Status == core.SessionCompleted {
		return core.Session{}, core.NewAdaptorError(core.KindValidation,
			"mock: session %q is in terminal state %s; cannot recycle",
			sessionID, sess.Status)
	}
	sess.Status = core.SessionReady
	sess.EndedAt = nil
	o.sessions[sessionID] = sess
	return sess, nil
}

func stringExtension(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func randomSuffix() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "0000"
	}
	return hex.EncodeToString(b)
}
