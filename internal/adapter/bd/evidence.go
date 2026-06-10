package bd

import (
	"context"
	"log/slog"
	"strings"

	"github.com/GembaCore/gemba-core/core"
	"github.com/GembaCore/gemba-core/internal/evidence"
)

// SetSynthesizer wires a custom evidence Synthesizer onto the
// WorkPlane. Used by tests to inject deterministic collectors;
// production callers receive a default synthesizer via NewWorkPlane.
func (w *WorkPlane) SetSynthesizer(s *evidence.Synthesizer, repoPath, githubRepo string) {
	w.synth = s
	w.repoPath = repoPath
	w.githubRepo = githubRepo
}

// synthesizeEvidence runs the configured Synthesizer against wi and
// merges the result into wi.Evidence. Best-effort: any failure is
// logged and the WorkItem is returned with whatever native Evidence
// it already had. gm-e6.5.
func (w *WorkPlane) synthesizeEvidence(ctx context.Context, wi core.WorkItem) core.WorkItem {
	if w.synth == nil {
		return wi
	}
	target := evidence.Target{
		ID:         wi.ID,
		NativeID:   nativeID(w.prefix, wi.ID),
		RepoPath:   w.repoPath,
		GitHubRepo: w.githubRepo,
	}
	out, err := w.synth.Collect(ctx, target)
	if err != nil {
		// Synthesizer's own errors are partial-failure aware; only a
		// total loss surfaces here. Log + continue per DoD.
		slog.Warn("beads: evidence synthesis failed",
			"id", string(wi.ID), "err", err)
		return wi
	}
	if len(out) == 0 {
		return wi
	}
	merged := append([]core.Evidence(nil), wi.Evidence...)
	merged = append(merged, out...)
	wi.Evidence = merged
	return wi
}

// evidenceFromLabels projects native Beads `evidence:*` labels onto
// core.Evidence entries WITHOUT the synthesized payload key. Format:
// `evidence:<kind>:<ref>` (e.g. evidence:url:https://example.com/foo).
// Unknown kinds fall through to EvidenceCustom so labels survive even
// if the convention drifts. gm-e6.5 / DD-13.
func evidenceFromLabels(labels []string) []core.Evidence {
	var out []core.Evidence
	for _, l := range labels {
		if !strings.HasPrefix(l, "evidence:") {
			continue
		}
		body := strings.TrimPrefix(l, "evidence:")
		kindStr, ref, ok := strings.Cut(body, ":")
		if !ok || kindStr == "" || ref == "" {
			continue
		}
		out = append(out, core.Evidence{
			ID:     "label:" + body,
			Kind:   evidenceKindFromLabel(kindStr),
			Source: "beads:label",
			Ref:    ref,
		})
	}
	return out
}

// evidenceKindFromLabel maps the label kind token to a core.EvidenceKind.
// Unknown tokens become EvidenceCustom so the operator's intent is
// preserved without forcing the adaptor to be the validator.
func evidenceKindFromLabel(s string) core.EvidenceKind {
	switch s {
	case "commit":
		return core.EvidenceCommit
	case "log":
		return core.EvidenceLog
	case "test_result", "test":
		return core.EvidenceTestResult
	case "url":
		return core.EvidenceURL
	case "file":
		return core.EvidenceFile
	}
	return core.EvidenceCustom
}
