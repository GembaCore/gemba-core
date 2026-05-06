// Package speckit reads Spec Kit planning artifacts and projects them
// onto Gemba work items.
package speckit

import "github.com/GembaCore/gemba-core/core"

type Feature struct {
	ID                string      `json:"id"`
	Title             string      `json:"title"`
	Directory         string      `json:"directory"`
	SpecPath          string      `json:"spec_path,omitempty"`
	PlanPath          string      `json:"plan_path,omitempty"`
	TasksPath         string      `json:"tasks_path,omitempty"`
	HasSpec           bool        `json:"has_spec"`
	HasPlan           bool        `json:"has_plan"`
	HasTasks          bool        `json:"has_tasks"`
	Spec              SpecSummary `json:"spec"`
	Tasks             []Task      `json:"tasks"`
	TaskCount         int         `json:"task_count"`
	ParallelTaskCount int         `json:"parallel_task_count"`
}

type SpecSummary struct {
	Title                  string      `json:"title,omitempty"`
	UserStories            []UserStory `json:"user_stories,omitempty"`
	AcceptanceScenarios    []string    `json:"acceptance_scenarios,omitempty"`
	FunctionalRequirements []string    `json:"functional_requirements,omitempty"`
}

type UserStory struct {
	ID                  string   `json:"id"`
	Title               string   `json:"title"`
	Priority            string   `json:"priority,omitempty"`
	AcceptanceScenarios []string `json:"acceptance_scenarios,omitempty"`
}

type Task struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Phase       string `json:"phase,omitempty"`
	StoryID     string `json:"story_id,omitempty"`
	Parallel    bool   `json:"parallel"`
	Done        bool   `json:"done"`
	Line        int    `json:"line,omitempty"`
	Description string `json:"description,omitempty"`
}

type ListResult struct {
	Configured bool      `json:"configured"`
	Features   []Feature `json:"features"`
	Total      int       `json:"total"`
}

type ChangeKind string

const (
	ChangeCreate ChangeKind = "create"
	ChangeUpdate ChangeKind = "update"
	ChangeDelete ChangeKind = "delete"
)

type SyncPlan struct {
	FeatureID string       `json:"feature_id"`
	Changes   []SyncChange `json:"changes"`
	Counts    ChangeCounts `json:"counts"`
	Hash      string       `json:"hash"`
	JSONL     string       `json:"jsonl,omitempty"`
	Warnings  []string     `json:"warnings,omitempty"`
}

type SyncDraft struct {
	FeatureID string          `json:"feature_id"`
	Plan      SyncPlan        `json:"plan"`
	Items     []core.WorkItem `json:"items"`
	Warnings  []string        `json:"warnings,omitempty"`
}

type ChangeCounts struct {
	Create int `json:"create"`
	Update int `json:"update"`
	Delete int `json:"delete"`
}

type SyncChange struct {
	Action   ChangeKind `json:"action"`
	Key      string     `json:"key"`
	Kind     string     `json:"kind"`
	SourceID string     `json:"source_id,omitempty"`
	ID       string     `json:"id,omitempty"`
	Title    string     `json:"title"`
	Summary  string     `json:"summary"`
}

type SyncResult struct {
	FeatureID  string            `json:"feature_id"`
	Milestone  string            `json:"milestone_id,omitempty"`
	Epic       string            `json:"epic_id,omitempty"`
	Stories    map[string]string `json:"story_ids,omitempty"`
	Tasks      map[string]string `json:"task_ids,omitempty"`
	Plan       SyncPlan          `json:"plan"`
	Created    []string          `json:"created,omitempty"`
	Updated    []string          `json:"updated,omitempty"`
	Deleted    []string          `json:"deleted,omitempty"`
	TaskCount  int               `json:"task_count"`
	StoryCount int               `json:"story_count"`
}

type SyncOptions struct {
	ExpectedHash string
	AllowDeletes bool
	Items        []core.WorkItem
}
