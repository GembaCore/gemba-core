package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

type beadsHistoryEvent struct {
	EventID    string             `json:"event_id"`
	OccurredAt time.Time          `json:"occurred_at"`
	Actor      string             `json:"actor"`
	Mode       string             `json:"mode"`
	Source     any                `json:"source,omitempty"`
	Action     string             `json:"action"`
	Entity     beadsHistoryEntity `json:"entity"`
	Before     map[string]any     `json:"before,omitempty"`
	After      map[string]any     `json:"after,omitempty"`
	Summary    string             `json:"summary"`
}

type beadsHistoryEntity struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
}

func (r *Router) beadsHistory(w http.ResponseWriter, _ *http.Request) {
	if !r.cfg.BeadsOnly {
		writeJSON(w, http.StatusOK, map[string]any{
			"mode":    "full",
			"entries": []beadsHistoryEvent{},
		})
		return
	}
	entries, malformed, err := readBeadsHistory(r.cfg.BeadsOnlyManifest())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"mode":      "beads_only",
			"path":      r.cfg.BeadsOnlyManifest(),
			"entries":   []beadsHistoryEvent{},
			"malformed": malformed,
			"error":     err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"mode":      "beads_only",
		"path":      r.cfg.BeadsOnlyManifest(),
		"entries":   entries,
		"malformed": malformed,
	})
}

func readBeadsHistory(path string) ([]beadsHistoryEvent, int, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []beadsHistoryEvent{}, 0, nil
		}
		return nil, 0, err
	}
	defer f.Close()

	var out []beadsHistoryEvent
	malformed := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev beadsHistoryEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			malformed++
			continue
		}
		out = append(out, ev)
	}
	if err := sc.Err(); err != nil {
		return out, malformed, err
	}
	return out, malformed, nil
}

func (r *Router) appendBeadsHistory(ev beadsHistoryEvent) error {
	if !r.cfg.BeadsOnly {
		return nil
	}
	if ev.EventID == "" {
		ev.EventID = "evt_" + newInstanceID()
	}
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now().UTC()
	}
	if ev.Actor == "" {
		ev.Actor = actorName()
	}
	ev.Mode = "beads_only"
	ev.Source = r.cfg.BeadsSource()

	path := r.cfg.BeadsOnlyManifest()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	line, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return nil
}

func actorName() string {
	for _, k := range []string{"GEMBA_ACTOR", "BEADS_ACTOR", "USER"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return "operator"
}

func workItemEntity(it core.WorkItem) beadsHistoryEntity {
	return beadsHistoryEntity{
		Type:  historyEntityType(it.Kind),
		ID:    string(it.ID),
		Title: it.Title,
	}
}

func historyEntityType(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "milestone":
		return "milestone"
	case "epic":
		return "epic"
	case "decision":
		return "decision"
	case "task", "feature", "bug", "chore":
		return "bead"
	default:
		if kind == "" {
			return "bead"
		}
		return kind
	}
}

func createAction(kind string) string {
	switch historyEntityType(kind) {
	case "milestone":
		return "milestone.created"
	case "epic":
		return "epic.created"
	case "decision":
		return "decision.created"
	default:
		return "work_item.created"
	}
}

func createSummary(it core.WorkItem) string {
	return fmt.Sprintf("Created %s %q.", historyEntityType(it.Kind), it.Title)
}

func patchSummary(before, after core.WorkItem, patch core.WorkItemPatch) string {
	name := after.Title
	if name == "" {
		name = string(after.ID)
	}
	if patch.StateCategory != nil && before.StateCategory != after.StateCategory {
		return fmt.Sprintf("Moved %q from %s to %s.", name, before.StateCategory, after.StateCategory)
	}
	return fmt.Sprintf("Edited %q.", name)
}

func patchBeforeAfter(before, after core.WorkItem, patch core.WorkItemPatch) (map[string]any, map[string]any) {
	b := map[string]any{}
	a := map[string]any{}
	if patch.Title != nil {
		b["title"] = before.Title
		a["title"] = after.Title
	}
	if patch.Description != nil {
		b["description"] = before.Description
		a["description"] = after.Description
	}
	if patch.Status != nil {
		b["status"] = before.Status
		a["status"] = after.Status
	}
	if patch.StateCategory != nil {
		b["state_category"] = before.StateCategory
		a["state_category"] = after.StateCategory
	}
	if patch.Priority != nil {
		b["priority"] = before.Priority
		a["priority"] = after.Priority
	}
	if patch.Labels != nil {
		b["labels"] = before.Labels
		a["labels"] = after.Labels
	}
	if patch.Parent != nil {
		b["parent_id"] = parentID(before)
		a["parent_id"] = parentID(after)
	}
	if len(b) == 0 {
		return nil, nil
	}
	return b, a
}

func parentID(it core.WorkItem) string {
	for _, rel := range it.Relationships {
		if rel.Kind == core.RelParentChild && rel.To == it.ID {
			return string(rel.From)
		}
	}
	return ""
}
