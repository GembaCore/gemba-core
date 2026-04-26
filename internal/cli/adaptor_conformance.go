package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync/atomic"

	"github.com/spf13/cobra"

	"github.com/MikeBengtson/gemba/internal/adapter/noop"
	"github.com/MikeBengtson/gemba/internal/core"
	gembatesting "github.com/MikeBengtson/gemba/testing"
)

// testFlags captures the `gemba adaptor test` flag surface. See the
// Long help below for semantics. The --target builtin:noop-work and
// builtin:noop-orch values drive the in-process noop adaptor; other
// targets require a transport wire client which lands in later epics.
type testFlags struct {
	transport string
	target    string
	plane     string
	junit     string
	jsonOut   bool
}

// newAdaptorTestCmd builds the `gemba adaptor test` subcommand (gm-e3.5).
func newAdaptorTestCmd() *cobra.Command {
	var f testFlags

	cmd := &cobra.Command{
		Use:   "test",
		Short: "Run the conformance harness against an adaptor",
		Long: `Run the Gemba conformance harness (gm-e3.5) against an adaptor and
report per-group pass/fail.

The harness executes probe groups A–F:

  Group A: describe + capability shape
  Group B: WorkItem CRUD round-trip (work plane) / session lifecycle (orchestration)
  Group C: edge / relationship round-trip (work plane; interface pending)
  Group D: subscribe / event delivery (orchestration plane)
  Group E: capability-declared optional features (sprint, budget, evidence)
  Group F: error semantics (idempotency, tagged errors, retry)

The exit code is 0 when every non-skipped probe passes and 1 when any
probe fails.

--target values:

  builtin:noop-work    in-process noop WorkPlane reference adaptor
  builtin:noop-orch    in-process noop OrchestrationPlane reference adaptor

  <URL|socket|cmd>     dial a remote adaptor over --transport. Transport
                       wire clients land incrementally in gm-e3.5 / gm-e4.x;
                       until they are wired this exits non-zero with a
                       structured not-yet-implemented message so CI gates
                       detect a bad target.

--transport values:

  api | jsonl | mcp    matched against the adaptor's declared transport;
                       mismatches fail fast (gm-root DD-12).

--junit <path>         emit a JUnit XML report alongside the text output
                       (CI integration is optional; the text summary is
                       the canonical format).

--json                 emit the machine-readable Report as JSON on stdout
                       instead of the text summary.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdaptorTest(cmd, f)
		},
	}

	cmd.Flags().StringVar(&f.transport, "transport", "",
		"transport kind: api | jsonl | mcp (required)")
	cmd.Flags().StringVar(&f.target, "target", "",
		"adaptor target: builtin:noop-work | builtin:noop-orch | <url|socket|cmd>")
	cmd.Flags().StringVar(&f.plane, "plane", "",
		"plane to test: work | orchestration (defaults to the plane implied by --target for builtins)")
	cmd.Flags().StringVar(&f.junit, "junit", "",
		"write a JUnit XML report to this path (optional)")
	cmd.Flags().BoolVar(&f.jsonOut, "json", false,
		"emit the report as JSON on stdout (default: human text)")

	_ = cmd.MarkFlagRequired("transport")
	_ = cmd.MarkFlagRequired("target")

	return cmd
}

// ErrConformanceFailed signals one or more probes failed. Execute()
// returns a non-zero exit code on this sentinel without re-printing
// cobra's usage block — the text summary already tells the operator
// exactly what went wrong.
var ErrConformanceFailed = fmt.Errorf("gemba adaptor test: one or more conformance probes failed")

func runAdaptorTest(cmd *cobra.Command, f testFlags) error {
	out := cmd.OutOrStdout()

	transport := core.Transport(f.transport)
	if !transport.Valid() {
		return fmt.Errorf(
			"unknown --transport %q: must be one of api, jsonl, mcp (gm-root DD-12)",
			f.transport)
	}

	report, err := runBuiltinOrRemote(cmd.Context(), f, transport)
	if err != nil {
		return err
	}

	if err := emitReport(out, report, f); err != nil {
		return err
	}

	if !report.Passed() {
		cmd.SilenceErrors = true
		return ErrConformanceFailed
	}
	return nil
}

func runBuiltinOrRemote(_ context.Context, f testFlags, transport core.Transport) (*gembatesting.Report, error) {
	switch f.target {
	case "builtin:noop-work":
		return runBuiltinNoopWork(transport, f)
	case "builtin:noop-orch":
		return runBuiltinNoopOrchestration(transport, f)
	default:
		return nil, fmt.Errorf(
			"remote --target %q over --transport %s: not-yet-implemented "+
				"(transport wire clients land incrementally; use builtin:noop-work "+
				"or builtin:noop-orch today)",
			f.target, transport)
	}
}

func runBuiltinNoopWork(transport core.Transport, f testFlags) (*gembatesting.Report, error) {
	if f.plane != "" && f.plane != "work" {
		return nil, fmt.Errorf(
			"--plane=%q is incompatible with --target=builtin:noop-work (implicit plane is work)",
			f.plane)
	}
	impl := noop.NewWorkPlane()
	m, err := impl.Describe(context.Background())
	if err != nil {
		return nil, fmt.Errorf("Describe: %w", err)
	}
	if m.Transport != transport {
		return nil, fmt.Errorf(
			"transport mismatch: --transport=%s but adaptor declares %s (gm-root DD-12)",
			transport, m.Transport)
	}
	fixture := &gembatesting.WorkPlaneFixture{
		KnownMissingID: core.WorkItemID("gemba/gemba/gm-does-not-exist"),
	}
	return gembatesting.RunWorkPlaneProbes(impl, fixture), nil
}

func runBuiltinNoopOrchestration(transport core.Transport, f testFlags) (*gembatesting.Report, error) {
	if f.plane != "" && f.plane != "orchestration" {
		return nil, fmt.Errorf(
			"--plane=%q is incompatible with --target=builtin:noop-orch (implicit plane is orchestration)",
			f.plane)
	}
	impl := noop.NewOrchestrationPlane()
	if got := impl.Describe().Transport; got != transport {
		return nil, fmt.Errorf(
			"transport mismatch: --transport=%s but adaptor declares %s (gm-root DD-12)",
			transport, got)
	}
	fixture := &gembatesting.OrchestrationFixture{
		KnownMissingSessionID: "sess-does-not-exist",
		ProgrammaticSessionStarter: func(a core.OrchestrationPlaneAdaptor) (string, func(), error) {
			op, ok := a.(*noop.OrchestrationPlane)
			if !ok {
				return "", nil, fmt.Errorf("SessionStarter: expected *noop.OrchestrationPlane, got %T", a)
			}
			// Unique assignment id per probe so the second Group B probe
			// (which closes its session) does not collide with the first.
			id := fmt.Sprintf("assign-conformance-%d", atomic.AddUint64(&conformanceAssignSeq, 1))
			op.CreateAssignment(core.Assignment{
				ID:         id,
				WorkItemID: core.WorkItemID("gemba/gemba/gm-conformance"),
				AgentID:    core.AgentID("gemba/polecats/conformance"),
				Status:     core.AssignmentActive,
			})
			s, err := op.StartSession(context.Background(), id, core.SessionPrompt{})
			if err != nil {
				return "", nil, fmt.Errorf("StartSession: %w", err)
			}
			return s.ID, nil, nil
		},
	}
	return gembatesting.RunOrchestrationProbes(impl, fixture), nil
}

// conformanceAssignSeq mints unique assignment ids for the noop
// orchestration fixture across Group B probes.
var conformanceAssignSeq uint64

func emitReport(out io.Writer, r *gembatesting.Report, f testFlags) error {
	if f.jsonOut {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(r); err != nil {
			return err
		}
	} else {
		gembatesting.WriteTextReport(out, r)
	}
	if f.junit != "" {
		fh, err := os.Create(f.junit)
		if err != nil {
			return fmt.Errorf("create --junit %q: %w", f.junit, err)
		}
		defer fh.Close()
		if err := gembatesting.WriteJUnit(fh, r); err != nil {
			return fmt.Errorf("write --junit: %w", err)
		}
	}
	return nil
}
