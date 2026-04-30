// /api/orchestration/* mutation handlers (gm-s47n.16, spec §7).
//
// Each mutation shells to the bound adaptor's CLI (gt today; native
// returns KindUnsupported for every mutation). The response shape
// surfaces the executed command + stdout + stderr so the editor's
// RunCommandModal can echo what happened back to the operator.
//
// All routes nonce-gated upstream in router.go.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// gtRunner is the shape orchestration_mutations.go uses to execute the
// gt CLI. Tests inject a stub via SetGTRunner so they don't fork
// subprocesses; production wraps exec.CommandContext.
type gtRunner func(ctx context.Context, args ...string) (stdout, stderr []byte, err error)

// gtRunnerVal stores the active runner. atomic.Value lets tests swap
// it without touching unexported router fields.
var gtRunnerVal atomic.Value

func init() {
	gtRunnerVal.Store(gtRunner(defaultGTRunner))
}

// SetGTRunner overrides the gt-shellout runner used by the
// orchestration mutation handlers. Tests use this to inject a fake
// without spawning processes. Pass nil to restore the default.
func SetGTRunner(r gtRunner) {
	if r == nil {
		gtRunnerVal.Store(gtRunner(defaultGTRunner))
		return
	}
	gtRunnerVal.Store(r)
}

func currentGTRunner() gtRunner {
	if v := gtRunnerVal.Load(); v != nil {
		if r, ok := v.(gtRunner); ok && r != nil {
			return r
		}
	}
	return defaultGTRunner
}

func defaultGTRunner(ctx context.Context, args ...string) ([]byte, []byte, error) {
	path, err := exec.LookPath("gt")
	if err != nil {
		return nil, nil, errors.New("gt CLI not on PATH")
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, path, args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.Bytes(), errBuf.Bytes(), err
}

// runResult is the wire shape every mutation returns (success or
// failure — non-success errors still surface stdout/stderr so the
// modal can render diagnostic output).
type runResult struct {
	Command string `json:"command"`
	Stdout  string `json:"stdout"`
	Stderr  string `json:"stderr"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
}

func (r *Router) adaptorID() string {
	if r.host == nil || r.host.OrchestrationPlane() == nil {
		return ""
	}
	return r.host.OrchestrationPlane().Describe().AdaptorID
}

// requireGastown writes a 400 KindUnsupported response when the bound
// adaptor isn't gt and returns true. The handler returns immediately.
func (r *Router) requireGastown(w http.ResponseWriter) bool {
	if r.adaptorID() != "gastown" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "kind_unsupported",
			"message": "this mutation is gt-only; bound adaptor does not support it",
		})
		return false
	}
	return true
}

func (r *Router) shellGT(w http.ResponseWriter, req *http.Request, cmdline string, args []string) {
	runner := currentGTRunner()
	stdout, stderr, err := runner(req.Context(), args...)
	res := runResult{
		Command: cmdline,
		Stdout:  string(stdout),
		Stderr:  string(stderr),
		OK:      err == nil,
	}
	if err != nil {
		res.Error = err.Error()
		writeJSON(w, http.StatusBadGateway, res)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// ------------------------------------------------------------------
// POST /api/orchestration/scopes  → gt rig create <name>
// ------------------------------------------------------------------

type createScopeBody struct {
	Name string `json:"name"`
}

func (r *Router) createScope(w http.ResponseWriter, req *http.Request) {
	if !r.requireGastown(w) {
		return
	}
	var body createScopeBody
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "invalid_body",
			"message": "request body must be {name: string}",
		})
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "missing_name",
			"message": "name is required",
		})
		return
	}
	r.shellGT(w, req,
		"gt rig create "+body.Name,
		[]string{"rig", "create", body.Name})
}

// ------------------------------------------------------------------
// POST /api/orchestration/polecats  → gt polecat create <rig> <persona>
// ------------------------------------------------------------------

type createPolecatBody struct {
	Rig     string `json:"rig"`
	Persona string `json:"persona"`
}

func (r *Router) createPolecat(w http.ResponseWriter, req *http.Request) {
	if !r.requireGastown(w) {
		return
	}
	var body createPolecatBody
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "invalid_body",
			"message": "request body must be {rig, persona}",
		})
		return
	}
	body.Rig = strings.TrimSpace(body.Rig)
	body.Persona = strings.TrimSpace(body.Persona)
	if body.Rig == "" || body.Persona == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "missing_field",
			"message": "rig and persona are required",
		})
		return
	}
	r.shellGT(w, req,
		"gt polecat create "+body.Rig+" "+body.Persona,
		[]string{"polecat", "create", body.Rig, body.Persona})
}

// ------------------------------------------------------------------
// GET /api/orchestration/scheduler-config
//   → gt config get scheduler.max_polecats
// PUT /api/orchestration/scheduler-config
//   → gt config set scheduler.max_polecats <n>
// ------------------------------------------------------------------

type schedulerConfig struct {
	MaxPolecats int `json:"max_polecats"`
}

func (r *Router) getSchedulerConfig(w http.ResponseWriter, req *http.Request) {
	if r.adaptorID() != "gastown" {
		// Native's planner config lives elsewhere; return an empty
		// envelope so the editor renders a friendly "not configurable"
		// state instead of branching on a 503.
		writeJSON(w, http.StatusOK, schedulerConfig{})
		return
	}
	runner := currentGTRunner()
	stdout, _, err := runner(req.Context(), "config", "get", "scheduler.max_polecats")
	if err != nil {
		// Treat a missing config as zero rather than 5xx — the
		// editor renders an empty input.
		writeJSON(w, http.StatusOK, schedulerConfig{})
		return
	}
	val, _ := strconv.Atoi(strings.TrimSpace(string(stdout)))
	writeJSON(w, http.StatusOK, schedulerConfig{MaxPolecats: val})
}

func (r *Router) putSchedulerConfig(w http.ResponseWriter, req *http.Request) {
	if !r.requireGastown(w) {
		return
	}
	var body schedulerConfig
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "invalid_body",
			"message": "request body must be {max_polecats: int}",
		})
		return
	}
	if body.MaxPolecats < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "invalid_value",
			"message": "max_polecats must be >= 0",
		})
		return
	}
	val := strconv.Itoa(body.MaxPolecats)
	r.shellGT(w, req,
		"gt config set scheduler.max_polecats "+val,
		[]string{"config", "set", "scheduler.max_polecats", val})
}
