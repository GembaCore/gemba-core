// /api/bootstrap/* handlers — the project-bootstrap wizard's backend
// (gm-ad1u; SPA contract gm-uipx.7).
//
// The /bootstrap SPA wizard composes a project config across four
// steps: Source → Analysis → Plan review → Ratify. This file ships
// the matching backend so the wizard works against a real server,
// not just the route-fake fixture.
//
// Wire contract: web/src/api/bootstrap.ts is the canonical shape.
// Field names + URL params (analysis_id, since, frames, findings,
// project_path, board_url) are mirrored verbatim — the SPA does
// not change. The bead description for gm-ad1u proposed `run_id`
// + `lines` + `consistency.summary`; that contradicts the typed
// client and is ignored. The typed client wins.
//
// Stub-mode analysis. v1 ships templated/stubbed analysis output
// keyed off `source`. The SPA's UX is what's being shipped, not
// real Jira/Beads/source-code introspection. Each kind returns a
// plausible 3-frame progress sequence + a sample plan + a synthesised
// consistency report. Real source readers are a follow-up bead.
//
// Storage: in-memory BootstrapStore. SQL persistence is deferred
// until a multi-instance deployment exists. Mirrors the walk.Store
// pattern (Start / Get / AddProgressFrame / Commit) so a future
// SQL implementation drops in behind the same interface.
//
// Auth + nonce: routes sit inside the same /api auth block as
// everything else. POST /commit is gated by requireConfirmNonce —
// the operation writes project state and a SPA double-click MUST
// not double-write. Read endpoints (start/progress/plan) are not
// nonce-gated; start is technically a mutation but it only allocates
// an analysis id against an in-memory map and the SPA doesn't reuse
// the id across mounts, so the cost of a duplicate alloc on retry
// is one stale entry that ages out of the store on the next sweep.

package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MikeBengtson/gemba/internal/server/httperr"
)

// BootstrapSource enumerates the four source kinds the wizard
// supports per ui-spec §5.15. Mirrors the typed client's union
// exactly so the JSON token round-trips without translation.
type BootstrapSource string

const (
	BootstrapSourceJira       BootstrapSource = "jira"
	BootstrapSourceBeads      BootstrapSource = "beads"
	BootstrapSourceSourceCode BootstrapSource = "source-code"
	BootstrapSourceFresh      BootstrapSource = "fresh"
)

// validBootstrapSource returns true if s is one of the supported
// source kinds. Any other value is rejected with a 400 by /start
// so a typo in the SPA surfaces as a clear error rather than a
// silently-empty analysis.
func validBootstrapSource(s BootstrapSource) bool {
	switch s {
	case BootstrapSourceJira, BootstrapSourceBeads,
		BootstrapSourceSourceCode, BootstrapSourceFresh:
		return true
	}
	return false
}

// BootstrapProgressFrame is one streamed line of analysis progress.
// JSON shape mirrors web/src/api/bootstrap.ts BootstrapProgress.
type BootstrapProgressFrame struct {
	Seq  int       `json:"seq"`
	Line string    `json:"line"`
	At   time.Time `json:"at"`
	Done bool      `json:"done,omitempty"`
}

// BootstrapPlanItem is one outline row in the generated plan
// (epic / milestone / sprint / story). Children render the
// two-level outline the SPA expects.
type BootstrapPlanItem struct {
	ID       string              `json:"id"`
	Kind     string              `json:"kind"` // epic | milestone | sprint | story
	Title    string              `json:"title"`
	Detail   string              `json:"detail,omitempty"`
	Children []BootstrapPlanItem `json:"children,omitempty"`
}

// BootstrapFinding is one row in the consistency report. level is
// pass | warn | fail; the SPA totals the levels for the top-line
// PASS=12 / WARN=2 / FAIL=0 summary.
type BootstrapFinding struct {
	ID     string `json:"id"`
	Level  string `json:"level"` // pass | warn | fail
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
}

// BootstrapAnalysis is the persisted record for one in-flight
// analysis. The store mints id; AppendProgress drives the long-poll
// stream; Plan + Findings are populated on completion; Committed
// flips on a successful POST /commit (idempotent guard against a
// second commit).
type BootstrapAnalysis struct {
	ID          string                   `json:"analysis_id"`
	Source      BootstrapSource          `json:"source"`
	Config      map[string]any           `json:"config,omitempty"`
	Frames      []BootstrapProgressFrame `json:"-"`
	Plan        []BootstrapPlanItem      `json:"-"`
	Findings    []BootstrapFinding       `json:"-"`
	StartedAt   time.Time                `json:"started_at"`
	CompletedAt time.Time                `json:"completed_at,omitempty"`
	Committed   bool                     `json:"committed"`
}

// BootstrapStore is the persistence interface. All methods are
// context-aware so a future SQL implementation honours deadlines.
// Mirrors walk.Store's narrow shape: callers hand pre-built records
// to mutation methods, the store concerns itself only with id +
// lifecycle.
type BootstrapStore interface {
	// Start allocates a new analysis with frames seeded from
	// `seed` (the synthesised progress sequence keyed off source).
	// Returns the persisted record so the handler can read the
	// minted id back without a second Get.
	Start(ctx context.Context, source BootstrapSource, config map[string]any, seed BootstrapAnalysisSeed) (BootstrapAnalysis, error)

	// Get returns the analysis by id. ErrBootstrapNotFound when
	// unknown.
	Get(ctx context.Context, id string) (BootstrapAnalysis, error)

	// FramesSince returns frames with seq > since, in seq order.
	// Used by GET /progress for the long-poll loop.
	FramesSince(ctx context.Context, id string, since int) ([]BootstrapProgressFrame, error)

	// Commit marks the analysis committed and returns the resulting
	// (project_path, board_url) pair. Idempotent: a second commit
	// returns the same response; ErrBootstrapAlreadyCommitted is
	// surfaced when the caller wants to know they raced (the HTTP
	// handler treats that as success).
	Commit(ctx context.Context, id string) (BootstrapCommitResult, error)
}

// BootstrapAnalysisSeed bundles the canned analysis output the
// handler synthesises off the source. The store doesn't care what
// the values mean — it just persists them so subsequent Get/Frames/
// Plan calls can read them back.
type BootstrapAnalysisSeed struct {
	Frames   []BootstrapProgressFrame
	Plan     []BootstrapPlanItem
	Findings []BootstrapFinding
}

// BootstrapCommitResult is the response shape /commit hands back.
// project_path lands in the success toast; board_url is the SPA's
// post-ratify destination.
type BootstrapCommitResult struct {
	ProjectPath string `json:"project_path"`
	BoardURL    string `json:"board_url"`
}

// ErrBootstrapNotFound is returned by BootstrapStore.Get / Commit
// when the analysis id is unknown.
var ErrBootstrapNotFound = errors.New("bootstrap: analysis not found")

// memoryBootstrapStore is the in-memory implementation. Thread-safe
// (RWMutex) so concurrent SPA polls + a single commit don't race.
// Capacity is unbounded today — bootstrap analyses are operator-
// driven (one or two per workspace, ever) so an LRU isn't worth the
// complexity in v1.
type memoryBootstrapStore struct {
	mu  sync.RWMutex
	now func() time.Time
	by  map[string]*BootstrapAnalysis
}

// NewMemoryBootstrapStore returns a fresh in-memory store. cmd/gemba
// serve calls this once at boot and hands the result to AttachBootstrap.
func NewMemoryBootstrapStore() BootstrapStore {
	return &memoryBootstrapStore{
		now: time.Now,
		by:  map[string]*BootstrapAnalysis{},
	}
}

func (s *memoryBootstrapStore) Start(_ context.Context, source BootstrapSource, config map[string]any, seed BootstrapAnalysisSeed) (BootstrapAnalysis, error) {
	id := newBootstrapID()
	now := s.now().UTC()
	rec := &BootstrapAnalysis{
		ID:        id,
		Source:    source,
		Config:    config,
		Frames:    append([]BootstrapProgressFrame(nil), seed.Frames...),
		Plan:      append([]BootstrapPlanItem(nil), seed.Plan...),
		Findings:  append([]BootstrapFinding(nil), seed.Findings...),
		StartedAt: now,
	}
	// Stamp completed_at when the seed already includes a terminal
	// frame (done=true). v1 stub-mode emits all frames synchronously,
	// so completion is immediate; a future async analyser would
	// AppendProgress over time and only stamp completed_at on the
	// final frame.
	for _, f := range rec.Frames {
		if f.Done {
			rec.CompletedAt = now
			break
		}
	}
	s.mu.Lock()
	s.by[id] = rec
	s.mu.Unlock()
	return *rec, nil
}

func (s *memoryBootstrapStore) Get(_ context.Context, id string) (BootstrapAnalysis, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.by[id]
	if !ok {
		return BootstrapAnalysis{}, ErrBootstrapNotFound
	}
	return *rec, nil
}

func (s *memoryBootstrapStore) FramesSince(_ context.Context, id string, since int) ([]BootstrapProgressFrame, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.by[id]
	if !ok {
		return nil, ErrBootstrapNotFound
	}
	out := make([]BootstrapProgressFrame, 0, len(rec.Frames))
	for _, f := range rec.Frames {
		if f.Seq > since {
			out = append(out, f)
		}
	}
	return out, nil
}

func (s *memoryBootstrapStore) Commit(_ context.Context, id string) (BootstrapCommitResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.by[id]
	if !ok {
		return BootstrapCommitResult{}, ErrBootstrapNotFound
	}
	rec.Committed = true
	// project_path + board_url are static for v1 — the workspace
	// only has one project config destination and the post-ratify
	// route is fixed. Per-workspace overrides land when the SQL
	// store does.
	return BootstrapCommitResult{
		ProjectPath: ".gemba/project.toml",
		BoardURL:    "/board",
	}, nil
}

// newBootstrapID mints a 16-byte hex analysis id. Same generator as
// newInstanceID; the prefix makes log-grep cheap.
func newBootstrapID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is functionally never; fall back to a
		// timestamp-derived sentinel so Start never panics.
		return "bootstrap-" + time.Now().UTC().Format("20060102T150405.000000000")
	}
	return "bootstrap-" + hex.EncodeToString(b)
}

// AttachBootstrap binds the BootstrapStore used by the
// /api/bootstrap/* handlers. Calling with nil removes the binding
// (handlers return 503). cmd/gemba serve calls this once at boot;
// tests inject memory stores per case.
func (r *Router) AttachBootstrap(store BootstrapStore) {
	r.bootstrapStore = store
}

// bootstrapAdaptorUnavailable writes the 503 envelope handlers
// reach for when AttachBootstrap hasn't been called. Mirrors the
// shared "adaptor not configured" shape the SPA's degraded-state
// banner already understands.
func bootstrapAdaptorUnavailable(w http.ResponseWriter) {
	httperr.Write(w, http.StatusServiceUnavailable,
		"adaptor_not_configured", "bootstrap store not registered")
}

// bootstrapStartRequest is the wire body of POST /api/bootstrap/start.
type bootstrapStartRequest struct {
	Source BootstrapSource `json:"source"`
	Config map[string]any  `json:"config,omitempty"`
}

// bootstrapStartResponse mirrors web/src/api/bootstrap.ts
// BootstrapStartResponse — opaque analysis_id and nothing else.
type bootstrapStartResponse struct {
	AnalysisID string `json:"analysis_id"`
}

func (r *Router) bootstrapStart(w http.ResponseWriter, req *http.Request) {
	if r.bootstrapStore == nil {
		bootstrapAdaptorUnavailable(w)
		return
	}
	var body bootstrapStartRequest
	if err := decodeJSONBody(req, &body); err != nil {
		httperr.Write(w, http.StatusBadRequest, "bad_request",
			"invalid JSON body: "+err.Error())
		return
	}
	if !validBootstrapSource(body.Source) {
		httperr.Write(w, http.StatusBadRequest, "bad_request",
			"source must be one of jira | beads | source-code | fresh")
		return
	}
	seed := buildBootstrapSeed(body.Source, time.Now().UTC())
	rec, err := r.bootstrapStore.Start(req.Context(), body.Source, body.Config, seed)
	if err != nil {
		httperr.WriteError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, bootstrapStartResponse{AnalysisID: rec.ID})
}

// bootstrapProgressResponse mirrors web/src/api/bootstrap.ts
// BootstrapProgressResponse — frames keyed by name to match the
// typed client.
type bootstrapProgressResponse struct {
	Frames []BootstrapProgressFrame `json:"frames"`
}

func (r *Router) bootstrapProgress(w http.ResponseWriter, req *http.Request) {
	if r.bootstrapStore == nil {
		bootstrapAdaptorUnavailable(w)
		return
	}
	id := strings.TrimSpace(req.URL.Query().Get("analysis_id"))
	if id == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request",
			"analysis_id query parameter is required")
		return
	}
	since := 0
	if s := req.URL.Query().Get("since"); s != "" {
		// Tolerate a non-integer since — a stale browser tab passing
		// junk shouldn't 500. Treat junk as zero (replay all frames),
		// the SPA reconciles by tracking the highest seq it sees.
		if n, err := strconv.Atoi(s); err == nil {
			since = n
		}
	}
	frames, err := r.bootstrapStore.FramesSince(req.Context(), id, since)
	if err != nil {
		writeBootstrapError(w, err)
		return
	}
	if frames == nil {
		frames = []BootstrapProgressFrame{}
	}
	writeJSON(w, http.StatusOK, bootstrapProgressResponse{Frames: frames})
}

// bootstrapPlanResponse mirrors web/src/api/bootstrap.ts BootstrapPlan —
// analysis_id + source + plan + findings, returned together so the SPA's
// PlanReviewStep renders both panes from one fetch. The bead description
// proposed a `consistency: { summary, details }` envelope; that
// contradicts the typed client (which expects flat findings + a
// SPA-side count) so we mirror the typed client.
type bootstrapPlanResponse struct {
	AnalysisID string              `json:"analysis_id"`
	Source     BootstrapSource     `json:"source"`
	Plan       []BootstrapPlanItem `json:"plan"`
	Findings   []BootstrapFinding  `json:"findings"`
}

func (r *Router) bootstrapPlan(w http.ResponseWriter, req *http.Request) {
	if r.bootstrapStore == nil {
		bootstrapAdaptorUnavailable(w)
		return
	}
	id := strings.TrimSpace(req.URL.Query().Get("analysis_id"))
	if id == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request",
			"analysis_id query parameter is required")
		return
	}
	rec, err := r.bootstrapStore.Get(req.Context(), id)
	if err != nil {
		writeBootstrapError(w, err)
		return
	}
	// Typed client expects 404 + code "bootstrap_pending" before the
	// analysis completes. Stub-mode synthesises every frame
	// synchronously so the analysis is always complete by the time
	// /plan lands, but we keep the gate so a future async analyser
	// drops in without a wire change.
	if rec.CompletedAt.IsZero() {
		httperr.Write(w, http.StatusNotFound, "bootstrap_pending",
			"analysis is still running; poll /progress until done=true")
		return
	}
	plan := rec.Plan
	if plan == nil {
		plan = []BootstrapPlanItem{}
	}
	findings := rec.Findings
	if findings == nil {
		findings = []BootstrapFinding{}
	}
	writeJSON(w, http.StatusOK, bootstrapPlanResponse{
		AnalysisID: rec.ID,
		Source:     rec.Source,
		Plan:       plan,
		Findings:   findings,
	})
}

// bootstrapCommitRequest is the wire body of POST /api/bootstrap/commit.
type bootstrapCommitRequest struct {
	AnalysisID string `json:"analysis_id"`
}

func (r *Router) bootstrapCommit(w http.ResponseWriter, req *http.Request) {
	if r.bootstrapStore == nil {
		bootstrapAdaptorUnavailable(w)
		return
	}
	var body bootstrapCommitRequest
	if err := decodeJSONBody(req, &body); err != nil {
		httperr.Write(w, http.StatusBadRequest, "bad_request",
			"invalid JSON body: "+err.Error())
		return
	}
	id := strings.TrimSpace(body.AnalysisID)
	if id == "" {
		httperr.Write(w, http.StatusBadRequest, "bad_request",
			"analysis_id is required")
		return
	}
	res, err := r.bootstrapStore.Commit(req.Context(), id)
	if err != nil {
		writeBootstrapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// writeBootstrapError maps store errors onto the shared envelope.
// ErrBootstrapNotFound → 404 "bootstrap_not_found"; everything else
// flows through the generic mapper.
func writeBootstrapError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrBootstrapNotFound):
		httperr.Write(w, http.StatusNotFound, "bootstrap_not_found", err.Error())
	default:
		httperr.WriteError(w, err)
	}
}

// buildBootstrapSeed returns the canned analysis output for `source`.
// All four kinds emit a 3-frame progress sequence + a 2-epic plan +
// a mixed PASS/WARN findings list so the SPA exercises every branch of
// its Step-2 / Step-3 UI.
//
// Stub-mode: real source readers (Jira REST API, beads database
// introspection, source-code AST scan, fresh-project skeleton
// generator) are deferred — the wizard's UX is what's being shipped.
// The seed shape is the same across kinds; only the strings vary so
// the operator sees a plausible analysis without a backend integration.
// Follow-up: file beads to replace each source's seed with real
// introspection.
func buildBootstrapSeed(source BootstrapSource, at time.Time) BootstrapAnalysisSeed {
	frames := []BootstrapProgressFrame{
		{Seq: 1, Line: bootstrapFrame1(source), At: at},
		{Seq: 2, Line: bootstrapFrame2(source), At: at},
		{Seq: 3, Line: bootstrapFrame3(source), At: at, Done: true},
	}
	plan := []BootstrapPlanItem{
		{
			ID:     "epic-onboard",
			Kind:   "epic",
			Title:  "Onboard the team",
			Detail: "Initial configuration milestone — define values, wire adaptors, ratify board.",
			Children: []BootstrapPlanItem{
				{ID: "milestone-values", Kind: "milestone", Title: "M1 — define values"},
				{ID: "milestone-adaptors", Kind: "milestone", Title: "M2 — wire adaptors"},
			},
		},
		{ID: "sprint-1", Kind: "sprint", Title: "Sprint 1"},
	}
	findings := []BootstrapFinding{
		{ID: "f1", Level: "pass", Title: "Project skeleton is well-formed"},
		{
			ID:     "f2",
			Level:  "warn",
			Title:  "No owner set for Sprint 1",
			Detail: "Assign an owner via the Personas section after ratify.",
		},
		{ID: "f3", Level: "pass", Title: "Source kind " + string(source) + " recognised"},
	}
	return BootstrapAnalysisSeed{Frames: frames, Plan: plan, Findings: findings}
}

func bootstrapFrame1(s BootstrapSource) string {
	switch s {
	case BootstrapSourceJira:
		return "Scanning Jira board for active epics…"
	case BootstrapSourceBeads:
		return "Scanning beads database for open issues…"
	case BootstrapSourceSourceCode:
		return "Scanning source tree for module structure…"
	default:
		return "Scanning fresh-project skeleton…"
	}
}

func bootstrapFrame2(s BootstrapSource) string {
	switch s {
	case BootstrapSourceJira:
		return "Analyzing Jira hierarchy…"
	case BootstrapSourceBeads:
		return "Analyzing beads dependency graph…"
	case BootstrapSourceSourceCode:
		return "Analyzing module ownership…"
	default:
		return "Analyzing project template…"
	}
}

func bootstrapFrame3(_ BootstrapSource) string {
	return "Generating epic decomposition…"
}
