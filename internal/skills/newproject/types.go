package newproject

// ID is the canonical skill identifier. The Onboarder persona
// (gm-root.17.10) references this string; the HTTP /newproject
// handlers also reference it for audit-log row tagging.
const ID = "newproject"

// DraftBead is the leaf of the plan tree. Field names are
// PascalCase to match the JSON wire shape declared on the SPA side
// in web/src/api/newproject.ts (which the SPA renders verbatim).
//
// All fields are populated. Empty strings, empty slices, and
// Estimate=0 are valid empty states; the contract is "no missing
// keys" so ratification is total. The skill MUST emit values for
// every field on every turn.
type DraftBead struct {
	Title       string `json:"Title"`
	Description string `json:"Description"`
	// Type is one of "task" | "bug" | "feature" | "chore" today.
	// Open string at the wire so a future bead kind doesn't break
	// the SPA. ValidateState restricts the closed set; downgrade
	// to a warning if the model emits an unknown kind.
	Type        string   `json:"Type"`
	Acceptance  string   `json:"Acceptance"`
	Labels      []string `json:"Labels"`
	Priority    int      `json:"Priority"`
	Estimate    int      `json:"Estimate"`
	Skills      []string `json:"Skills"`
	DesignNotes string   `json:"DesignNotes"`
	Notes       string   `json:"Notes"`
	// DependsOnRefs / BlocksRefs use intra-tree references like
	// "milestone:0/epic:1/bead:2". Ratification (gm-root.17.6)
	// resolves these to bd-... ids in step 8a. The skill MUST NOT
	// emit external (bd-...) ids -- the project doesn't exist yet.
	DependsOnRefs []string `json:"DependsOnRefs"`
	BlocksRefs    []string `json:"BlocksRefs"`
}

// DraftEpic groups beads under a milestone.
type DraftEpic struct {
	Title       string      `json:"Title"`
	Description string      `json:"Description"`
	Acceptance  string      `json:"Acceptance"`
	Labels      []string    `json:"Labels"`
	Priority    int         `json:"Priority"`
	Estimate    int         `json:"Estimate"`
	Skills      []string    `json:"Skills"`
	DesignNotes string      `json:"DesignNotes"`
	Notes       string      `json:"Notes"`
	Beads       []DraftBead `json:"Beads"`
}

// DraftMilestone is the top of the plan tree. Note that the
// type:milestone label is added by ratification, NOT by the skill
// (see docs/design/milestone-convention.md).
type DraftMilestone struct {
	Title       string      `json:"Title"`
	Description string      `json:"Description"`
	Acceptance  string      `json:"Acceptance"`
	Labels      []string    `json:"Labels"`
	Priority    int         `json:"Priority"`
	Estimate    int         `json:"Estimate"`
	Skills      []string    `json:"Skills"`
	DesignNotes string      `json:"DesignNotes"`
	Notes       string      `json:"Notes"`
	Epics       []DraftEpic `json:"Epics"`
}

// ChangeRef points into the plan tree for the most-recent skill
// edit. The SPA tone-codes the diff badge by Kind. Open shape --
// the skill emits whatever granularity it needs and the SPA
// renders Path verbatim.
//
// Path is slash-joined ("milestone:1/epic:0/bead:2"). Empty path
// means "the change spans the whole tree" (e.g. a fresh draft).
type ChangeRef struct {
	Path string `json:"path"`
	// Kind is one of: "added" | "removed" | "renamed" | "edited" | "".
	// Empty means the skill didn't categorise the change.
	Kind    string `json:"kind"`
	Summary string `json:"summary"`
}

// Valid ChangeRef.Kind values. Exposed so validators and tests
// don't have to string-match.
const (
	ChangeKindAdded   = "added"
	ChangeKindRemoved = "removed"
	ChangeKindRenamed = "renamed"
	ChangeKindEdited  = "edited"
)

// NewProjectState is the authoritative server-side state for the
// session. Each turn returns a fresh value -- the host replaces
// its in-memory copy wholesale. Ratify (gm-root.17.6) consumes
// this verbatim.
type NewProjectState struct {
	ProjectName  string           `json:"ProjectName"`
	Description  string           `json:"Description"`
	TechStack    []string         `json:"TechStack"`
	Architecture string           `json:"Architecture"`
	Milestones   []DraftMilestone `json:"Milestones"`
	// DraftProjectMD is the running narrative the skill keeps
	// current each turn. Ratification persists this verbatim as
	// docs/project.md.
	DraftProjectMD string `json:"DraftProjectMD"`
	// Turn is monotonic across the conversation. Starts at 0 in
	// EmptyState; the skill increments on every successful turn.
	Turn       int       `json:"Turn"`
	LastChange ChangeRef `json:"LastChange"`
}

// EmptyState is the initial value the host starts a conversation
// with. Mirrors web/src/api/newproject.ts EMPTY_STATE -- every
// field is present, just empty. The "no missing keys" contract
// applies to the empty state too.
func EmptyState() NewProjectState {
	return NewProjectState{
		ProjectName:    "",
		Description:    "",
		TechStack:      []string{},
		Architecture:   "",
		Milestones:     []DraftMilestone{},
		DraftProjectMD: "",
		Turn:           0,
		LastChange:     ChangeRef{Path: "", Kind: "", Summary: ""},
	}
}

// validBeadTypes is the closed set the skill is allowed to emit
// today. Open string on the wire (so a future kind doesn't break
// the SPA) but enforced at validation time so a model that
// hallucinates "story" or "subtask" gets caught.
var validBeadTypes = map[string]struct{}{
	"task":    {},
	"bug":     {},
	"feature": {},
	"chore":   {},
}

// validChangeKinds mirrors the SPA-side ChangeRef.kind union. The
// empty string is also valid -- "the skill didn't categorise this
// change" -- so callers can degrade gracefully when the model
// omits the kind.
var validChangeKinds = map[string]struct{}{
	"":                {},
	ChangeKindAdded:   {},
	ChangeKindRemoved: {},
	ChangeKindRenamed: {},
	ChangeKindEdited:  {},
}
