package persona

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// containsString — local helper used by the seed-file test.
func containsString(haystack []string, needle string) bool {
	return slices.Contains(haystack, needle)
}

const minimalTOML = `
id = "tester"
name = "Test"
role = "Tester"
description = "fixture"
system_prompt = "You are a test."

[scope]
kind = "project"

[model]
vendor = "anthropic"
model = "claude-haiku-4-5"

[budget_policy]
counts_against_sprint = false
`

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestLoadFile_MinimalDefaultsToCoach(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "tester.toml", minimalTOML)

	p, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if p.ID != "tester" {
		t.Errorf("id = %q, want tester", p.ID)
	}
	if p.Variety != VarietyCoach {
		t.Errorf("variety = %q, want %q (default)", p.Variety, VarietyCoach)
	}
	if p.Model.Vendor != "anthropic" || p.Model.Model != "claude-haiku-4-5" {
		t.Errorf("model = %+v, want anthropic/claude-haiku-4-5", p.Model)
	}
}

func TestLoadFile_RejectsFilenameMismatch(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "wrong-name.toml", minimalTOML)

	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected error for filename/id mismatch")
	}
	if !strings.Contains(err.Error(), "does not match filename stem") {
		t.Errorf("error = %v, want filename mismatch", err)
	}
}

func TestLoadFile_RejectsMissingRequired(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(s string) string
		wantSub string
	}{
		{
			name:    "missing model.vendor",
			mutate:  func(s string) string { return strings.ReplaceAll(s, `vendor = "anthropic"`, `vendor = ""`) },
			wantSub: "model.vendor",
		},
		{
			name: "missing system_prompt",
			mutate: func(s string) string {
				return strings.ReplaceAll(s, `system_prompt = "You are a test."`, `system_prompt = ""`)
			},
			wantSub: "system_prompt",
		},
		{
			name:    "missing role",
			mutate:  func(s string) string { return strings.ReplaceAll(s, `role = "Tester"`, `role = ""`) },
			wantSub: "role",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeFile(t, dir, "tester.toml", tc.mutate(minimalTOML))
			_, err := LoadFile(path)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error = %v, want substring %q", err, tc.wantSub)
			}
		})
	}
}

func TestVariety_ManagerRequiresMutationAuthority(t *testing.T) {
	dir := t.TempDir()
	body := strings.Replace(minimalTOML, `description = "fixture"`,
		`description = "fixture"
variety = "manager"`, 1)
	path := writeFile(t, dir, "tester.toml", body)

	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected error: manager without mutation_authority")
	}
	if !strings.Contains(err.Error(), "mutation_authority") {
		t.Errorf("error = %v, want mention of mutation_authority", err)
	}
}

// gm-k2jn: scope is required on every persona file. A file that
// omits [scope] fails to load.
func TestScope_RequiredOnLoad(t *testing.T) {
	dir := t.TempDir()
	body := strings.ReplaceAll(minimalTOML, `[scope]
kind = "project"`, "")
	path := writeFile(t, dir, "tester.toml", body)
	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected error: missing scope")
	}
	if !strings.Contains(err.Error(), "scope.kind is required") {
		t.Errorf("error = %v, want scope.kind required", err)
	}
}

// scope.repository must be present when scope.kind=repository.
func TestScope_RepositoryKindRequiresRepoID(t *testing.T) {
	dir := t.TempDir()
	body := strings.ReplaceAll(minimalTOML,
		`[scope]
kind = "project"`,
		`[scope]
kind = "repository"`)
	path := writeFile(t, dir, "tester.toml", body)
	_, err := LoadFile(path)
	if err == nil || !strings.Contains(err.Error(), "scope.repository required") {
		t.Errorf("err = %v, want scope.repository required", err)
	}
}

// scope.repository must be EMPTY when scope.kind != repository.
func TestScope_NonRepositoryKindForbidsRepoID(t *testing.T) {
	dir := t.TempDir()
	body := strings.ReplaceAll(minimalTOML,
		`[scope]
kind = "project"`,
		`[scope]
kind = "project"
repository = "should-not-be-here"`)
	path := writeFile(t, dir, "tester.toml", body)
	_, err := LoadFile(path)
	if err == nil || !strings.Contains(err.Error(), "scope.repository must be empty") {
		t.Errorf("err = %v, want scope.repository must be empty", err)
	}
}

// scope.kind must be one of the three known values.
func TestScope_UnknownKindRejected(t *testing.T) {
	dir := t.TempDir()
	body := strings.ReplaceAll(minimalTOML,
		`[scope]
kind = "project"`,
		`[scope]
kind = "everywhere"`)
	path := writeFile(t, dir, "tester.toml", body)
	_, err := LoadFile(path)
	if err == nil || !strings.Contains(err.Error(), "unknown scope.kind") {
		t.Errorf("err = %v, want unknown-kind error", err)
	}
}

// Manager + scope=any is rejected — mutation must bind to project or
// a named repository, otherwise mutation_scope.paths can't bind to a
// real filesystem location.
func TestScope_ManagerForbidsScopeAny(t *testing.T) {
	dir := t.TempDir()
	body := strings.ReplaceAll(minimalTOML,
		`[scope]
kind = "project"`,
		`variety = "manager"
mutation_authority = ["docs_edit"]
[scope]
kind = "any"`)
	path := writeFile(t, dir, "tester.toml", body)
	_, err := LoadFile(path)
	if err == nil || !strings.Contains(err.Error(), "must not declare scope.kind=any") {
		t.Errorf("err = %v, want manager+any rejection", err)
	}
}

// scope=repository accepted with a non-empty repository id.
func TestScope_RepositoryKindAccepted(t *testing.T) {
	dir := t.TempDir()
	body := strings.ReplaceAll(minimalTOML,
		`[scope]
kind = "project"`,
		`[scope]
kind = "repository"
repository = "frontend"`)
	path := writeFile(t, dir, "tester.toml", body)
	p, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if p.Scope.Kind != ScopeRepository {
		t.Errorf("Scope.Kind = %q, want repository", p.Scope.Kind)
	}
	if p.Scope.RepositoryID != "frontend" {
		t.Errorf("Scope.RepositoryID = %q, want frontend", p.Scope.RepositoryID)
	}
}

func TestVariety_CoachRejectsMutationAuthority(t *testing.T) {
	dir := t.TempDir()
	body := strings.Replace(minimalTOML, `description = "fixture"`,
		`description = "fixture"
mutation_authority = ["docs_edit"]`, 1)
	path := writeFile(t, dir, "tester.toml", body)

	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected error: coach with mutation_authority")
	}
}

func TestLoadDir_SkipsNonTOMLAndReturnsEmptyOnMissing(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "tester.toml", minimalTOML)
	writeFile(t, dir, "README.md", "ignore me")

	got, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(got) != 1 || got["tester"] == nil {
		t.Fatalf("got %d personas; want 1 (tester)", len(got))
	}

	missing, err := LoadDir(filepath.Join(dir, "nope"))
	if err != nil {
		t.Errorf("missing dir should not error: %v", err)
	}
	if len(missing) != 0 {
		t.Errorf("missing dir should yield 0 personas, got %d", len(missing))
	}
}

func TestLoadDir_RejectsDuplicateID(t *testing.T) {
	dir := t.TempDir()
	dup := strings.ReplaceAll(minimalTOML, `id = "tester"`, `id = "tester"`)
	writeFile(t, dir, "tester.toml", minimalTOML)
	// Force a second file whose stem matches its declared id but
	// collides on the id field of the first file.
	collide := strings.ReplaceAll(dup, `id = "tester"`, `id = "duped"`)
	collide = strings.ReplaceAll(collide, `id = "duped"`, `id = "tester"`)
	writeFile(t, dir, "duped.toml", collide)

	_, err := LoadDir(dir)
	if err == nil {
		t.Fatal("expected duplicate-id error")
	}
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
	r := NewRegistry()
	p := &Persona{
		ID:           "tester",
		Name:         "Test",
		Role:         "Tester",
		Variety:      VarietyCoach,
		Model:        ModelConfig{Vendor: "anthropic", Model: "claude-haiku-4-5"},
		SystemPrompt: "You are a test.",
	}
	if err := r.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := r.Register(p); err != nil {
		t.Errorf("idempotent re-register of same pointer: %v", err)
	}

	other := &Persona{ID: "tester"}
	if err := r.Register(other); err == nil {
		t.Error("expected duplicate-id error on different pointer")
	}

	got, ok := r.Get("tester")
	if !ok || got != p {
		t.Errorf("Get returned (%v, %v); want (%p, true)", got, ok, p)
	}

	if list := r.List(); len(list) != 1 || list[0] != "tester" {
		t.Errorf("List = %v, want [tester]", list)
	}
}

func TestLoadRegistry_SeedFiles(t *testing.T) {
	// The repo ships seed personas under .gemba/personas/. They MUST
	// parse successfully — a malformed seed file would break server
	// startup. Tests run from the package directory, so walk back up.
	r, err := LoadRegistry("../../../.gemba/personas")
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	want := []string{"deployment-engineer", "documentarian", "project-manager"}
	for _, id := range want {
		p, ok := r.Get(id)
		if !ok {
			t.Errorf("seed persona %q missing", id)
			continue
		}
		if p.ID != id {
			t.Errorf("seed %q has id %q", id, p.ID)
		}
		if len(p.Skills) == 0 {
			t.Errorf("seed %q declares no skills", id)
		}
	}

	pm, _ := r.Get("project-manager")
	if pm.Variety != VarietyCoach {
		t.Errorf("project-manager variety = %q, want coach", pm.Variety)
	}
	if got := pm.Model.Vendor; got != "anthropic" {
		t.Errorf("project-manager model.vendor = %q, want anthropic", got)
	}
	// epic_order is the v1 skill. gm-e11.8.7 added escalation_handoff
	// so the persona is selectable from the EscalationsPage Hand-off
	// modal. The test only enforces that both skills are present;
	// later beads can add more without rewriting this assertion.
	if !containsString(pm.Skills, "epic_order") {
		t.Errorf("project-manager skills = %v, want includes epic_order", pm.Skills)
	}
	if !containsString(pm.Skills, "escalation_handoff") {
		t.Errorf("project-manager skills = %v, want includes escalation_handoff", pm.Skills)
	}
	// gm-yjst: PM must know it can propose milestone creation through a
	// suggested_action. The guidance lives in system_prompt; a missing
	// reference means the dispatched persona will not know the verb/path.
	for _, want := range []string{"milestone", "/api/work-items"} {
		if !strings.Contains(pm.SystemPrompt, want) {
			t.Errorf("project-manager system_prompt missing %q (gm-yjst)", want)
		}
	}
}

// gm-8qr: Personality + Perspective + Purview round-trip from TOML.
// A fully-populated PPPP fixture loads successfully and every field
// survives the decode → normalize → Validate path.
func TestLoadFile_PPPPRoundTrips(t *testing.T) {
	const body = `
id = "tester"
name = "Test"
role = "Tester"
description = "fixture"
system_prompt = "You are a test."

[scope]
kind = "project"

[model]
vendor = "anthropic"
model = "claude-haiku-4-5"

[budget_policy]
counts_against_sprint = false

[personality]
id = "laconic"
description = "Precise and economical."
examples = ["short", "blunt"]

[perspective]
statement = "design integrity"
triggers = ["area:core", "edges internal/core/*"]
volunteer_mode = "on_trigger"
cost_tier = "haiku"

[purview]
domain = "design"
active_phases = ["ideation", "design", "building"]
blocking_authority = "strong"
`
	dir := t.TempDir()
	path := writeFile(t, dir, "tester.toml", body)
	p, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	if p.Personality.ID != "laconic" {
		t.Errorf("Personality.ID = %q, want laconic", p.Personality.ID)
	}
	if p.Personality.Description != "Precise and economical." {
		t.Errorf("Personality.Description = %q", p.Personality.Description)
	}
	if got := p.Personality.Examples; len(got) != 2 || got[0] != "short" || got[1] != "blunt" {
		t.Errorf("Personality.Examples = %v, want [short blunt]", got)
	}

	if p.Perspective.Statement != "design integrity" {
		t.Errorf("Perspective.Statement = %q", p.Perspective.Statement)
	}
	if got := p.Perspective.Triggers; len(got) != 2 || got[0] != "area:core" || got[1] != "edges internal/core/*" {
		t.Errorf("Perspective.Triggers = %v", got)
	}
	if p.Perspective.VolunteerMode != VolunteerOnTrigger {
		t.Errorf("Perspective.VolunteerMode = %q, want on_trigger", p.Perspective.VolunteerMode)
	}
	if p.Perspective.CostTier != "haiku" {
		t.Errorf("Perspective.CostTier = %q", p.Perspective.CostTier)
	}

	if p.Purview.Domain != "design" {
		t.Errorf("Purview.Domain = %q", p.Purview.Domain)
	}
	wantPhases := []Phase{"ideation", "design", "building"}
	if got := p.Purview.ActivePhases; len(got) != len(wantPhases) {
		t.Errorf("Purview.ActivePhases = %v, want %v", got, wantPhases)
	} else {
		for i, w := range wantPhases {
			if got[i] != w {
				t.Errorf("Purview.ActivePhases[%d] = %q, want %q", i, got[i], w)
			}
		}
	}
	if p.Purview.BlockingAuthority != PurviewStrong {
		t.Errorf("Purview.BlockingAuthority = %q, want strong", p.Purview.BlockingAuthority)
	}
}

// gm-1w7: the seed personas now ship with concrete Personality +
// Perspective blocks per the gm-9rv design. Documentarian also
// declares a [purview] block (domain="docs", advisory). The PM
// intentionally OMITS [purview] per gm-9rv invariant #31 — PM is
// orchestrator, not domain-owner — so its Purview must remain the
// zero value.
func TestLoadRegistry_SeedFiles_PPPPPopulated(t *testing.T) {
	r, err := LoadRegistry("../../../.gemba/personas")
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}

	pm, ok := r.Get("project-manager")
	if !ok {
		t.Fatal("project-manager seed missing")
	}
	if pm.Personality.IsZero() {
		t.Error("project-manager: Personality must be populated (gm-1w7)")
	}
	if pm.Personality.ID == "" || pm.Personality.Description == "" {
		t.Errorf("project-manager: Personality.ID/Description must be set, got %+v", pm.Personality)
	}
	if pm.Perspective.IsZero() {
		t.Error("project-manager: Perspective must be populated (gm-1w7)")
	}
	if pm.Perspective.VolunteerMode != VolunteerOnDemand {
		t.Errorf("project-manager: Perspective.VolunteerMode = %q, want on_demand", pm.Perspective.VolunteerMode)
	}
	// gm-9rv invariant #31: PM has no Purview. The orchestrator does
	// not own a gateable domain.
	if !pm.Purview.IsZero() {
		t.Errorf("project-manager: Purview must remain zero-value per gm-9rv invariant #31, got %+v", pm.Purview)
	}

	docs, ok := r.Get("documentarian")
	if !ok {
		t.Fatal("documentarian seed missing")
	}
	if docs.Personality.IsZero() {
		t.Error("documentarian: Personality must be populated (gm-1w7)")
	}
	if docs.Perspective.IsZero() {
		t.Error("documentarian: Perspective must be populated (gm-1w7)")
	}
	// Per gm-9rv design, Documentarian volunteers always — the design's
	// canonical example of an `always`-mode persona.
	if docs.Perspective.VolunteerMode != VolunteerAlways {
		t.Errorf("documentarian: Perspective.VolunteerMode = %q, want always", docs.Perspective.VolunteerMode)
	}
	if docs.Purview.IsZero() {
		t.Error("documentarian: Purview must be populated (gm-1w7)")
	}
	if docs.Purview.Domain == "" {
		t.Errorf("documentarian: Purview.Domain must be set, got %+v", docs.Purview)
	}
	// Documentarian is `advisory` — per the design's "Example roster"
	// row "(any phase; advisory)".
	if docs.Purview.BlockingAuthority != PurviewAdvisory {
		t.Errorf("documentarian: Purview.BlockingAuthority = %q, want advisory", docs.Purview.BlockingAuthority)
	}
}

// gm-8qr: a declared [perspective] block without volunteer_mode
// normalizes to "never". The empty block stays untouched (zero
// value).
func TestLoadFile_PerspectiveDefaultsVolunteerModeNever(t *testing.T) {
	const fragment = `
[perspective]
statement = "design integrity"
`
	dir := t.TempDir()
	path := writeFile(t, dir, "tester.toml", minimalTOML+fragment)
	p, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if p.Perspective.VolunteerMode != VolunteerNever {
		t.Errorf("Perspective.VolunteerMode = %q, want never (default)", p.Perspective.VolunteerMode)
	}
}

// gm-8qr: a declared [purview] block without blocking_authority
// normalizes to "advisory".
func TestLoadFile_PurviewDefaultsAuthorityAdvisory(t *testing.T) {
	const fragment = `
[purview]
domain = "docs"
`
	dir := t.TempDir()
	path := writeFile(t, dir, "tester.toml", minimalTOML+fragment)
	p, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if p.Purview.BlockingAuthority != PurviewAdvisory {
		t.Errorf("Purview.BlockingAuthority = %q, want advisory (default)", p.Purview.BlockingAuthority)
	}
}

// gm-8qr: malformed enum values are rejected with a caller-readable
// error mentioning the offending field.
func TestLoadFile_RejectsMalformedPPPPEnums(t *testing.T) {
	cases := []struct {
		name     string
		fragment string
		wantSub  string
	}{
		{
			name: "unknown volunteer_mode",
			fragment: `
[perspective]
statement = "x"
volunteer_mode = "sometimes"
`,
			wantSub: "volunteer_mode",
		},
		{
			name: "unknown blocking_authority",
			fragment: `
[purview]
domain = "x"
blocking_authority = "veto"
`,
			wantSub: "blocking_authority",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeFile(t, dir, "tester.toml", minimalTOML+tc.fragment)
			_, err := LoadFile(path)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error = %v, want substring %q", err, tc.wantSub)
			}
		})
	}
}

// gm-lq1: a persona TOML with a full [output] table parses correctly
// and round-trips every sub-field.
func TestOutput_FullTableParses(t *testing.T) {
	dir := t.TempDir()
	body := minimalTOML + `
[output]
validation = "jsonl_schema"
schema_ref = "/api/v1/skills/epic_order/output_schema.json"
sharing = "audit_only"
retention_days = 90
redact_before_sharing = ["api_keys", "internal_urls"]
`
	path := writeFile(t, dir, "tester.toml", body)
	p, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if p.Output.Validation != OutputValidationJSONLSchema {
		t.Errorf("Output.Validation = %q, want jsonl_schema", p.Output.Validation)
	}
	if p.Output.SchemaRef != "/api/v1/skills/epic_order/output_schema.json" {
		t.Errorf("Output.SchemaRef = %q, want schema path", p.Output.SchemaRef)
	}
	if p.Output.Sharing != OutputSharingAuditOnly {
		t.Errorf("Output.Sharing = %q, want audit_only", p.Output.Sharing)
	}
	if p.Output.RetentionDays != 90 {
		t.Errorf("Output.RetentionDays = %d, want 90", p.Output.RetentionDays)
	}
	if got := p.Output.RedactBeforeSharing; len(got) != 2 || got[0] != "api_keys" || got[1] != "internal_urls" {
		t.Errorf("Output.RedactBeforeSharing = %v, want [api_keys internal_urls]", got)
	}
}

// gm-lq1: a persona TOML with no [output] table loads with the zero
// value. Existing personas (project-manager, documentarian) MUST keep
// loading without modification.
func TestOutput_OmittedTableYieldsZero(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "tester.toml", minimalTOML)
	p, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if !p.Output.IsZero() {
		t.Errorf("Output = %+v, want zero", p.Output)
	}
}

// gm-lq1: unknown output.validation enum value is rejected at load.
func TestOutput_UnknownValidationRejected(t *testing.T) {
	dir := t.TempDir()
	body := minimalTOML + `
[output]
validation = "bogus_shape"
`
	path := writeFile(t, dir, "tester.toml", body)
	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected error: unknown output.validation")
	}
	if !strings.Contains(err.Error(), "output.validation") {
		t.Errorf("error = %v, want mention of output.validation", err)
	}
}

// gm-lq1: unknown output.sharing enum value is rejected at load.
func TestOutput_UnknownSharingRejected(t *testing.T) {
	dir := t.TempDir()
	body := minimalTOML + `
[output]
sharing = "everyone"
`
	path := writeFile(t, dir, "tester.toml", body)
	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected error: unknown output.sharing")
	}
	if !strings.Contains(err.Error(), "output.sharing") {
		t.Errorf("error = %v, want mention of output.sharing", err)
	}
}

// gm-lq1: negative output.retention_days is rejected at load.
func TestOutput_NegativeRetentionDaysRejected(t *testing.T) {
	dir := t.TempDir()
	body := minimalTOML + `
[output]
retention_days = -7
`
	path := writeFile(t, dir, "tester.toml", body)
	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("expected error: negative retention_days")
	}
	if !strings.Contains(err.Error(), "retention_days") {
		t.Errorf("error = %v, want mention of retention_days", err)
	}
}

func TestLoadRegistry_SourcePath(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "tester.toml", minimalTOML)

	r, err := LoadRegistry(dir)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if got := r.SourcePath("tester"); got != path {
		t.Errorf("SourcePath = %q, want %q", got, path)
	}
	if got := r.SourcePath("missing"); got != "" {
		t.Errorf("missing SourcePath = %q, want \"\"", got)
	}
}
