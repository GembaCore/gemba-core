package phase

import (
	"fmt"
	"strings"
	"time"

	"github.com/MikeBengtson/gemba/internal/core"
)

// Advance applies a single Phase transition to state and returns the
// updated state. Pure: state is taken by value, mutations land on the
// returned copy, and Advance never touches a Store. Caller is
// responsible for persistence.
//
// Errors:
//
//   - ErrInvalidPhase   — to is not a recognised Phase.
//   - ErrSamePhase      — to equals the current Phase (no-op).
//   - ErrInvalidTransition — (state.Phase, to) is not in the
//     transition graph. The HTTP layer surfaces this as 400; an
//     operator-initiated override path lives at the API layer
//     (force=true with nonce + justification — gm-jt9 keeps the
//     primitive strict; the override is the API layer's job to allow
//     when added).
//
// On success the returned state has Phase=to, Since=now, and a new
// Transition appended to History. The input state is not mutated.
func Advance(state WorkspaceState, to Phase, by core.AgentID, reason string, now time.Time) (WorkspaceState, error) {
	if err := to.Validate(); err != nil {
		return state, err
	}
	if state.Phase == to {
		return state, fmt.Errorf("%w: %s", ErrSamePhase, to)
	}
	if !CanTransition(state.Phase, to) {
		return state, fmt.Errorf("%w: %s → %s", ErrInvalidTransition, state.Phase, to)
	}

	t := Transition{
		From:   state.Phase,
		To:     to,
		At:     now.UTC(),
		By:     by,
		Reason: strings.TrimSpace(reason),
	}

	out := state.Clone()
	out.Phase = to
	out.Since = t.At
	out.History = append(out.History, t)
	return out, nil
}
