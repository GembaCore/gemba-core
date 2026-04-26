package enrichment

import (
	"context"
	"errors"
)

// Extractor produces a starter [Enrichment] from a bead's textual
// content (title + body + an optional linked spec). The interface
// stays small so backends can swap freely:
//
//   - [NoopExtractor] returns the empty enrichment — safe default
//     when no provider is wired.
//   - [HeuristicExtractor] mines the text for path-shaped tokens
//     and vocabulary tags. Network-free; ships in the binary.
//   - A future LLM-backed implementation (gm-s47n follow-up) wraps
//     the Anthropic / Bedrock / local-model SDK behind the same
//     interface. It does not need to land for the bead-creation
//     pipeline to start producing useful enrichment.
//
// Extractors MUST be pure with respect to their inputs (same
// BeadInput → same Enrichment) so the bead-creation pipeline can
// rerun extraction safely. The Heuristic implementation is
// trivially pure; LLM-backed implementations should at minimum
// pin temperature=0 and document their non-determinism.
//
// Callers route the result through [enrichment.Store.Save]; the
// Extractor itself never writes.
type Extractor interface {
	Extract(ctx context.Context, in BeadInput) (Enrichment, error)
}

// BeadInput is the textual surface an extractor sees. Keep it
// stringly-typed so callers from the CLI, the bead-create hook,
// and the backfill loop can all build it without dragging in a
// WorkItem dependency. BeadID is required so the returned
// [Enrichment] can be saved without a second lookup; everything
// else is optional.
type BeadInput struct {
	BeadID string
	Title  string
	Body   string
	// Spec is the linked design / spec doc body, if any. Treated
	// as additional searchable text — extractors don't need to
	// distinguish it from Body for the heuristic path. LLM-backed
	// extractors may weight it differently.
	Spec string
	// Vocabulary is the active controlled vocabulary (canonical
	// term names plus aliases). Optional; an empty vocabulary
	// disables the concept-extraction half of the pipeline. The
	// CLI loads it via internal/concepts.LoadVocabulary.
	Vocabulary []string
}

// ErrExtractorUnavailable is the typed sentinel an LLM-backed
// extractor returns when its underlying client is configured but
// cannot answer right now (network down, API key missing, rate
// limited). The CLI / bead-create hook treats it as a soft skip
// (no enrichment recorded; operator can rerun later).
var ErrExtractorUnavailable = errors.New("enrichment: extractor unavailable")

// NoopExtractor returns an empty [Enrichment] — useful as the
// default when no extractor is wired so callers don't have to nil-
// check. Stamps BeadID + Source so the result is still a valid
// Save target.
type NoopExtractor struct{}

// Extract implements [Extractor].
func (NoopExtractor) Extract(_ context.Context, in BeadInput) (Enrichment, error) {
	return Enrichment{BeadID: in.BeadID, Source: SourceLLM}, nil
}
