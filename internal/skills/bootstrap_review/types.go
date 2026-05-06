package bootstrap_review

type LineType string

const (
	LineQuestion LineType = "question"
	LinePatch    LineType = "patch"
	LineSummary  LineType = "summary"
)

type ReviewInput struct {
	WorkspaceName string       `json:"workspace_name"`
	PackID        string       `json:"pack_id"`
	FeatureID     string       `json:"feature_id"`
	FeatureTitle  string       `json:"feature_title"`
	Guidance      string       `json:"guidance,omitempty"`
	PlanHash      string       `json:"plan_hash"`
	Items         []DraftItem  `json:"items"`
	Plan          PlanSnapshot `json:"plan"`
}

type PlanSnapshot struct {
	Create int `json:"create"`
	Update int `json:"update"`
	Delete int `json:"delete"`
}

type DraftItem struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Labels      []string `json:"labels,omitempty"`
}

type QuestionLine struct {
	Type     LineType `json:"type"`
	Question string   `json:"question"`
	Reason   string   `json:"reason,omitempty"`
}

type PatchLine struct {
	Type        LineType `json:"type"`
	ItemID      string   `json:"item_id"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Rationale   string   `json:"rationale"`
}

type SummaryLine struct {
	Type      LineType `json:"type"`
	FeatureID string   `json:"feature_id"`
	Ready     bool     `json:"ready"`
	Note      string   `json:"note"`
}
