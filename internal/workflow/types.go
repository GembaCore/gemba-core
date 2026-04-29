// Package workflow wraps the bd CLI's formula + mol surfaces behind a
// thin Go client (gm-e12.22.2). It is the proxy layer the
// /v1/workflows/{formulas,runs} HTTP handlers shell out to. The
// package owns no business logic — every method is a single bd
// invocation, decoded into a typed struct so handlers stay handler-
// shaped.
package workflow

// Formula is the wire shape the SPA's Workflow Library renders. It is
// a faithful projection of `bd formula list/show --json` enriched
// with the resolved DAG when available (Show calls bd cook). Fields
// not emitted by every bd version stay omitempty so the response is
// stable across versions.
type Formula struct {
	Name        string     `json:"name"`
	Type        string     `json:"type,omitempty"`
	Version     int        `json:"version,omitempty"`
	Description string     `json:"description,omitempty"`
	SourcePath  string     `json:"source_path,omitempty"`
	Variables   []Variable `json:"variables"`
	Steps       []Step     `json:"steps,omitempty"`

	Composition *Composition `json:"composition,omitempty"`
}

// Variable is one entry from a formula's vars table. Default + Required
// are bd-side hints the Authoring tab uses to render an input form.
type Variable struct {
	Name        string `json:"name"`
	Default     string `json:"default,omitempty"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// Step is one node in the cooked DAG. Needs lists step ids that gate
// this step. The SPA renders Needs as edges in the three-column graph.
type Step struct {
	ID          string   `json:"id"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Needs       []string `json:"needs,omitempty"`
}

// Composition surfaces the formula's inheritance + extension graph.
// Empty slices are valid (formula doesn't extend anything); a nil
// Composition pointer means the bd version we're shelling to didn't
// emit composition info.
type Composition struct {
	Extends    []string `json:"extends,omitempty"`
	Aspects    []string `json:"aspects,omitempty"`
	Expansions []string `json:"expansions,omitempty"`
}

// Run is the wire shape for a poured/wisped molecule. The id, formula,
// and progress counters drive the Active runs tab. AttachedToBead
// surfaces the parent bead the run was poured against (when the
// formula spec carries `attach_to`).
type Run struct {
	ID             string    `json:"id"`
	Formula        string    `json:"formula,omitempty"`
	Status         string    `json:"status"`
	Ephemeral      bool      `json:"ephemeral"`
	Progress       Progress  `json:"progress"`
	AttachedToBead string    `json:"attached_to_bead,omitempty"`
	StartedAt      string    `json:"started_at,omitempty"`
	EndedAt        string    `json:"ended_at,omitempty"`
	Steps          []RunStep `json:"steps,omitempty"`
}

// Progress is a {done,total} pair derived from the run's child issues.
type Progress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// RunStep is the per-step detail surfaced by GET /v1/workflows/runs/:id.
// State is one of {ready, in-progress, done, blocked, skipped}; the
// SPA paints chips off this token.
type RunStep struct {
	ID            string   `json:"id"`
	Title         string   `json:"title,omitempty"`
	State         string   `json:"state"`
	AssignedAgent string   `json:"assigned_agent,omitempty"`
	Evidence      []string `json:"evidence,omitempty"`
}

// PourRequest is the body of POST /v1/workflows/runs. Ephemeral=true
// dispatches to bd mol wisp; false to bd mol pour. The SPA sends the
// same shape for both.
type PourRequest struct {
	Formula       string            `json:"formula"`
	Vars          map[string]string `json:"vars,omitempty"`
	AttachToBead  string            `json:"attach_to_bead,omitempty"`
	Ephemeral     bool              `json:"ephemeral,omitempty"`
}
