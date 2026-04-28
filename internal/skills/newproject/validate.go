package newproject

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ValidationError is the structured error type ValidateState
// returns when the model's output violates the schema or
// invariants. The persona host (gm-root.17.10) treats these as
// retryable -- it asks the skill to re-emit -- distinct from
// transport / decode errors, which are caller bugs.
//
// Field is a slash-joined path into the offending location
// (e.g. "Milestones[1].Epics[0].Beads[2].Title"). Empty when the
// failure is structural (missing top-level key) rather than
// pinpointed.
type ValidationError struct {
	Field   string
	Message string
}

// Error implements error. Format is "<Field>: <Message>" when
// Field is non-empty, otherwise just "<Message>" -- matches the
// "user-facing string" convention the dispatcher already uses.
func (e *ValidationError) Error() string {
	if e.Field == "" {
		return "newproject: " + e.Message
	}
	return "newproject: " + e.Field + ": " + e.Message
}

// IsValidationError reports whether err (or anything it wraps) is
// a *ValidationError. The host uses this to decide retry vs. fail.
func IsValidationError(err error) bool {
	var v *ValidationError
	return errors.As(err, &v)
}

// ValidateState runs the full per-turn schema check. Returns the
// first error encountered, with a Field path that the host can
// surface back to the model on retry.
//
// The validator is deliberately strict on structural invariants
// (no orphan beads, no dangling refs) and lenient on free-form
// content (Description / Notes / DesignNotes can be empty
// strings -- empty is a valid empty state per the contract).
//
// Also enforces -- on every turn after the first -- that the
// state isn't pathologically empty (zero milestones with a
// non-zero Turn means the model lost the plan tree, which is the
// most common skill regression).
func ValidateState(s *NewProjectState, requireNonEmpty bool) error {
	if s == nil {
		return &ValidationError{Message: "state is nil"}
	}
	if s.Turn < 0 {
		return &ValidationError{Field: "Turn", Message: "must be >= 0"}
	}
	if s.TechStack == nil {
		// Open-string-array fields MUST be present (even if
		// empty) so JSON round-tripping doesn't silently drop
		// the key. The host should never see nil at this point;
		// if it does, the decoder broke.
		return &ValidationError{Field: "TechStack", Message: "must not be nil (use [])"}
	}
	if s.Milestones == nil {
		return &ValidationError{Field: "Milestones", Message: "must not be nil (use [])"}
	}
	if requireNonEmpty && len(s.Milestones) == 0 {
		return &ValidationError{
			Field:   "Milestones",
			Message: "must not be empty after the first turn -- the operator can't ratify an empty plan",
		}
	}
	if err := validateChangeRef(&s.LastChange); err != nil {
		return err
	}

	// Track every (milestone:M/epic:E/bead:B) path so dependency
	// refs can be cross-checked against the live tree. Build the
	// set in a single forward pass before checking refs in a
	// second pass -- a bead can legitimately depend on a
	// later-numbered sibling.
	paths := map[string]struct{}{}
	for mi, m := range s.Milestones {
		mPath := fmt.Sprintf("Milestones[%d]", mi)
		if err := validateMilestone(&m, mPath); err != nil {
			return err
		}
		paths[fmt.Sprintf("milestone:%d", mi)] = struct{}{}
		for ei, e := range m.Epics {
			ePath := fmt.Sprintf("%s.Epics[%d]", mPath, ei)
			if err := validateEpic(&e, ePath); err != nil {
				return err
			}
			paths[fmt.Sprintf("milestone:%d/epic:%d", mi, ei)] = struct{}{}
			for bi, b := range e.Beads {
				bPath := fmt.Sprintf("%s.Beads[%d]", ePath, bi)
				if err := validateBead(&b, bPath); err != nil {
					return err
				}
				paths[fmt.Sprintf("milestone:%d/epic:%d/bead:%d", mi, ei, bi)] = struct{}{}
			}
		}
	}

	// Second pass: every DependsOnRefs / BlocksRefs entry must
	// resolve into the path set we just built.
	for mi, m := range s.Milestones {
		for ei, e := range m.Epics {
			for bi, b := range e.Beads {
				bPath := fmt.Sprintf("Milestones[%d].Epics[%d].Beads[%d]", mi, ei, bi)
				selfRef := fmt.Sprintf("milestone:%d/epic:%d/bead:%d", mi, ei, bi)
				for ri, r := range b.DependsOnRefs {
					if err := validateRef(r, selfRef, paths, fmt.Sprintf("%s.DependsOnRefs[%d]", bPath, ri)); err != nil {
						return err
					}
				}
				for ri, r := range b.BlocksRefs {
					if err := validateRef(r, selfRef, paths, fmt.Sprintf("%s.BlocksRefs[%d]", bPath, ri)); err != nil {
						return err
					}
				}
			}
		}
	}

	return nil
}

func validateMilestone(m *DraftMilestone, path string) error {
	if strings.TrimSpace(m.Title) == "" {
		return &ValidationError{Field: path + ".Title", Message: "must not be empty"}
	}
	if m.Labels == nil {
		return &ValidationError{Field: path + ".Labels", Message: "must not be nil (use [])"}
	}
	if m.Skills == nil {
		return &ValidationError{Field: path + ".Skills", Message: "must not be nil (use [])"}
	}
	if m.Epics == nil {
		return &ValidationError{Field: path + ".Epics", Message: "must not be nil (use [])"}
	}
	if len(m.Epics) == 0 {
		return &ValidationError{
			Field:   path + ".Epics",
			Message: "milestone must have at least one epic -- every milestone rolls up child epics",
		}
	}
	if m.Priority < 0 || m.Priority > 4 {
		return &ValidationError{
			Field:   path + ".Priority",
			Message: "must be in 0..4 (0 = highest)",
		}
	}
	if m.Estimate < 0 {
		return &ValidationError{Field: path + ".Estimate", Message: "must be >= 0"}
	}
	for li, lbl := range m.Labels {
		if strings.TrimSpace(lbl) == "" {
			return &ValidationError{
				Field:   fmt.Sprintf("%s.Labels[%d]", path, li),
				Message: "label must not be empty",
			}
		}
		// type:milestone is added by ratification; the skill
		// MUST NOT emit it. Catching it here keeps the
		// canonical-encoding contract (docs/design/milestone-convention.md)
		// honest.
		if strings.EqualFold(lbl, "type:milestone") {
			return &ValidationError{
				Field:   fmt.Sprintf("%s.Labels[%d]", path, li),
				Message: "the type:milestone label is added by ratification -- do not emit it from the skill",
			}
		}
	}
	return nil
}

func validateEpic(e *DraftEpic, path string) error {
	if strings.TrimSpace(e.Title) == "" {
		return &ValidationError{Field: path + ".Title", Message: "must not be empty"}
	}
	if e.Labels == nil {
		return &ValidationError{Field: path + ".Labels", Message: "must not be nil (use [])"}
	}
	if e.Skills == nil {
		return &ValidationError{Field: path + ".Skills", Message: "must not be nil (use [])"}
	}
	if e.Beads == nil {
		return &ValidationError{Field: path + ".Beads", Message: "must not be nil (use [])"}
	}
	if len(e.Beads) == 0 {
		return &ValidationError{
			Field:   path + ".Beads",
			Message: "epic must have at least one bead -- every epic rolls up child beads",
		}
	}
	if e.Priority < 0 || e.Priority > 4 {
		return &ValidationError{Field: path + ".Priority", Message: "must be in 0..4"}
	}
	if e.Estimate < 0 {
		return &ValidationError{Field: path + ".Estimate", Message: "must be >= 0"}
	}
	for li, lbl := range e.Labels {
		if strings.TrimSpace(lbl) == "" {
			return &ValidationError{
				Field:   fmt.Sprintf("%s.Labels[%d]", path, li),
				Message: "label must not be empty",
			}
		}
	}
	return nil
}

func validateBead(b *DraftBead, path string) error {
	if strings.TrimSpace(b.Title) == "" {
		return &ValidationError{Field: path + ".Title", Message: "must not be empty"}
	}
	if b.Labels == nil {
		return &ValidationError{Field: path + ".Labels", Message: "must not be nil (use [])"}
	}
	if b.Skills == nil {
		return &ValidationError{Field: path + ".Skills", Message: "must not be nil (use [])"}
	}
	if b.DependsOnRefs == nil {
		return &ValidationError{Field: path + ".DependsOnRefs", Message: "must not be nil (use [])"}
	}
	if b.BlocksRefs == nil {
		return &ValidationError{Field: path + ".BlocksRefs", Message: "must not be nil (use [])"}
	}
	if _, ok := validBeadTypes[b.Type]; !ok {
		return &ValidationError{
			Field:   path + ".Type",
			Message: fmt.Sprintf("must be one of task|bug|feature|chore (got %q)", b.Type),
		}
	}
	if b.Priority < 0 || b.Priority > 4 {
		return &ValidationError{Field: path + ".Priority", Message: "must be in 0..4"}
	}
	if b.Estimate < 0 {
		return &ValidationError{Field: path + ".Estimate", Message: "must be >= 0"}
	}
	for li, lbl := range b.Labels {
		if strings.TrimSpace(lbl) == "" {
			return &ValidationError{
				Field:   fmt.Sprintf("%s.Labels[%d]", path, li),
				Message: "label must not be empty",
			}
		}
	}
	return nil
}

func validateChangeRef(c *ChangeRef) error {
	if _, ok := validChangeKinds[c.Kind]; !ok {
		return &ValidationError{
			Field:   "LastChange.kind",
			Message: fmt.Sprintf("must be one of added|removed|renamed|edited|\"\" (got %q)", c.Kind),
		}
	}
	if c.Path != "" {
		if !changeRefPathRE.MatchString(c.Path) {
			return &ValidationError{
				Field:   "LastChange.path",
				Message: fmt.Sprintf("must match milestone:N[/epic:N[/bead:N]] (got %q)", c.Path),
			}
		}
	}
	return nil
}

// changeRefPathRE matches the three legal LastChange.path shapes:
//   - "milestone:N"
//   - "milestone:N/epic:N"
//   - "milestone:N/epic:N/bead:N"
var changeRefPathRE = regexp.MustCompile(`^milestone:\d+(/epic:\d+(/bead:\d+)?)?$`)

// validateRef checks one DependsOnRefs / BlocksRefs entry: must
// resolve to a real path in the tree, must not equal the bead's
// own path (no self-loops), and must structurally point at a
// bead (refs to a milestone or epic are rejected -- bead
// dependencies live at bead granularity in v1).
func validateRef(ref string, selfRef string, paths map[string]struct{}, fieldPath string) error {
	if ref == "" {
		return &ValidationError{Field: fieldPath, Message: "ref must not be empty"}
	}
	if !beadRefRE.MatchString(ref) {
		return &ValidationError{
			Field:   fieldPath,
			Message: fmt.Sprintf("must match milestone:N/epic:N/bead:N (got %q)", ref),
		}
	}
	if ref == selfRef {
		return &ValidationError{
			Field:   fieldPath,
			Message: "self-reference -- a bead cannot depend on itself",
		}
	}
	if _, ok := paths[ref]; !ok {
		return &ValidationError{
			Field:   fieldPath,
			Message: fmt.Sprintf("dangling ref %q does not resolve to any bead in the tree", ref),
		}
	}
	return nil
}

// beadRefRE matches "milestone:N/epic:N/bead:N" exactly. Refs at
// coarser granularity (just milestone, just milestone+epic) are
// rejected -- bead dependencies are bead-to-bead in v1.
var beadRefRE = regexp.MustCompile(`^milestone:\d+/epic:\d+/bead:\d+$`)

// parseRef breaks "milestone:M/epic:E/bead:B" into integers.
// Exposed (lower-cased) so the diff-rederivation helper in
// state.go can walk refs without re-parsing strings. Returns
// negative values when ref is malformed; callers use
// beadRefRE.MatchString first.
func parseRef(ref string) (mi, ei, bi int) {
	mi, ei, bi = -1, -1, -1
	parts := strings.Split(ref, "/")
	for _, p := range parts {
		kv := strings.SplitN(p, ":", 2)
		if len(kv) != 2 {
			return -1, -1, -1
		}
		n, err := strconv.Atoi(kv[1])
		if err != nil {
			return -1, -1, -1
		}
		switch kv[0] {
		case "milestone":
			mi = n
		case "epic":
			ei = n
		case "bead":
			bi = n
		default:
			return -1, -1, -1
		}
	}
	return mi, ei, bi
}
