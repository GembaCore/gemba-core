package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/GembaCore/gemba-core/internal/adapter/registry"
	"github.com/GembaCore/gemba-core/internal/config"
	"github.com/GembaCore/gemba-core/internal/server/httperr"
)

type beadsHealthAction struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Destructive bool   `json:"destructive,omitempty"`
}

type beadsHealthResult struct {
	Source            config.BeadsSource       `json:"source"`
	CurrentDB         string                   `json:"current_db"`
	RemoteConfigured  bool                     `json:"remote_configured"`
	RemoteKind        string                   `json:"remote_kind"`
	RemoteStatusLabel string                   `json:"remote_status_label"`
	Adaptor           *registry.AdaptorStatus  `json:"adaptor,omitempty"`
	Actions           []beadsHealthAction      `json:"actions"`
	LastAction        *beadsHealthActionResult `json:"last_action,omitempty"`
}

type beadsHealthActionResult struct {
	Action   string `json:"action"`
	Ok       bool   `json:"ok"`
	Message  string `json:"message"`
	Output   string `json:"output,omitempty"`
	ExitCode int    `json:"exit_code,omitempty"`
}

func (r *Router) beadsHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, r.beadsHealthSnapshot(nil))
}

func (r *Router) beadsHealthAction(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		httperr.Write(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	action := strings.TrimSpace(body.Action)
	result, err := r.runBeadsHealthAction(req.Context(), action)
	if err != nil {
		httperr.Write(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, r.beadsHealthSnapshot(result))
}

func (r *Router) beadsHealthSnapshot(last *beadsHealthActionResult) beadsHealthResult {
	source := r.cfg.BeadsSource()
	adaptor := workAdaptorStatus(r.snapshotAdaptorStatuses())
	return beadsHealthResult{
		Source:            source,
		CurrentDB:         currentBeadsDB(source),
		RemoteConfigured:  remoteConfigured(source),
		RemoteKind:        remoteKind(source),
		RemoteStatusLabel: remoteStatusLabel(source, adaptor),
		Adaptor:           adaptor,
		Actions:           r.beadsHealthActions(source),
		LastAction:        last,
	}
}

func (r *Router) beadsHealthActions(source config.BeadsSource) []beadsHealthAction {
	actions := []beadsHealthAction{{
		ID:          "refresh",
		Label:       "Refresh health",
		Description: "Re-run the bound Beads adaptor health probe.",
	}}
	switch source.Kind {
	case "beads-dir":
		actions = append(actions,
			beadsHealthAction{ID: "dolt-start", Label: "Start Dolt server", Description: "Run bd dolt start in the Beads worktree."},
			beadsHealthAction{ID: "dolt-test", Label: "Test Dolt connection", Description: "Run bd dolt test in the Beads worktree."},
			beadsHealthAction{ID: "remote-list", Label: "List remotes", Description: "Run bd dolt remote list to confirm remote configuration."},
			beadsHealthAction{ID: "doctor-server", Label: "Run server doctor", Description: "Run bd doctor --server --json for structured diagnostics."},
		)
	case "dolt-url":
		actions = append(actions, beadsHealthAction{
			ID:          "remote-test",
			Label:       "Test remote connection",
			Description: "Ping the configured Dolt/Dolthub URL through the bound adaptor.",
		})
	}
	return actions
}

func (r *Router) runBeadsHealthAction(ctx context.Context, action string) (*beadsHealthActionResult, error) {
	switch action {
	case "refresh":
		if r.healthBus != nil {
			r.healthBus.Poll()
		}
		return &beadsHealthActionResult{Action: action, Ok: true, Message: "Health probe refreshed."}, nil
	case "remote-test":
		statuses := r.snapshotAdaptorStatuses()
		if r.healthBus != nil {
			statuses = r.healthBus.Poll()
		}
		adaptor := workAdaptorStatus(statuses)
		if adaptor == nil {
			return &beadsHealthActionResult{Action: action, Ok: false, Message: "No Beads adaptor is bound."}, nil
		}
		if !adaptor.Healthy {
			msg := adaptor.Reason
			if msg == "" {
				msg = "Remote connection is not healthy."
			}
			return &beadsHealthActionResult{Action: action, Ok: false, Message: msg}, nil
		}
		return &beadsHealthActionResult{Action: action, Ok: true, Message: "Remote connection is healthy."}, nil
	case "dolt-start":
		return r.runBdHealthCommand(ctx, action, "bd dolt start", "bd", "dolt", "start")
	case "dolt-test":
		return r.runBdHealthCommand(ctx, action, "bd dolt test", "bd", "dolt", "test")
	case "remote-list":
		return r.runBdHealthCommand(ctx, action, "bd dolt remote list", "bd", "dolt", "remote", "list")
	case "doctor-server":
		return r.runBdHealthCommand(ctx, action, "bd doctor --server --json", "bd", "doctor", "--server", "--json")
	default:
		return nil, &badRequestError{msg: "unknown Beads health action: " + action}
	}
}

func (r *Router) runBdHealthCommand(ctx context.Context, action, label, name string, args ...string) (*beadsHealthActionResult, error) {
	source := r.cfg.BeadsSource()
	if source.Kind != "beads-dir" {
		return nil, &badRequestError{msg: "action requires a local Beads worktree"}
	}
	cwd := r.cfg.BeadsDir
	if cwd == "" {
		if wd, err := os.Getwd(); err == nil {
			cwd = wd
		}
	}
	if cwd != "" {
		if abs, err := filepath.Abs(cwd); err == nil {
			cwd = abs
		}
	}
	cmdCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, name, args...) //nolint:gosec // fixed command + fixed argv from server allowlist.
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	result := &beadsHealthActionResult{
		Action:  action,
		Ok:      err == nil,
		Message: label,
		Output:  truncateHealthOutput(output),
	}
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exit.ExitCode()
		}
		if cmdCtx.Err() == context.DeadlineExceeded {
			result.Message = label + " timed out."
		} else if output != "" {
			result.Message = firstLine(output)
		} else {
			result.Message = err.Error()
		}
		return result, nil
	}
	if output != "" {
		result.Message = firstLine(output)
	}
	if r.healthBus != nil {
		r.healthBus.Poll()
	}
	return result, nil
}

func workAdaptorStatus(statuses []registry.AdaptorStatus) *registry.AdaptorStatus {
	for _, status := range statuses {
		if status.Plane != registry.WorkPlane {
			continue
		}
		s := status
		return &s
	}
	return nil
}

func currentBeadsDB(source config.BeadsSource) string {
	if source.Label != "" {
		return source.Label
	}
	if source.Detail != "" {
		return source.Detail
	}
	return "not configured"
}

func remoteConfigured(source config.BeadsSource) bool {
	return source.Kind == "dolt-url"
}

func remoteKind(source config.BeadsSource) string {
	switch source.Kind {
	case "dolt-url":
		if strings.Contains(strings.ToLower(source.Detail), "dolthub") {
			return "Dolthub"
		}
		return "Dolt URL"
	case "beads-dir":
		return "Local worktree"
	default:
		return "None"
	}
}

func remoteStatusLabel(source config.BeadsSource, adaptor *registry.AdaptorStatus) string {
	if source.Kind == "dolt-url" {
		if adaptor != nil && adaptor.Healthy {
			return "Remote configured"
		}
		return "Remote needs attention"
	}
	if source.Kind == "beads-dir" {
		return "Local DB"
	}
	return "Not configured"
}

func truncateHealthOutput(s string) string {
	const max = 2000
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n…"
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
