// Gas Town cost meter synthesis (gm-e7.4).
//
// CostMeter synthesis from Gas Town session telemetry + Claude
// Code transcripts. Three axes:
//
//   - wallclock_seconds — EndedAt - StartedAt (clamped to 0).
//   - tokens_total      — sum of input + cache_creation +
//                         cache_read + output tokens across every
//                         assistant turn in the transcript.
//   - dollars_est       — derived when pricing config is present.
//
// The transcript is optional: when missing or unreadable, the
// wallclock axis still ships and the rollup degrades gracefully
// (TokensTotal=0, DollarsEst=0). The bead's DoD ("falls back
// gracefully when transcripts are missing") names this directly.

package gt

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/MikeBengtson/gemba/core"
)

// SessionTelemetry is the input bundle CostMeter synthesis
// consumes. StartedAt is required; the rest is optional.
type SessionTelemetry struct {
	// StartedAt is when the session opened. Required (zero
	// returns wallclock=0 + an empty CostMeter; the synthesiser
	// still doesn't error so a partial rollup beats no rollup).
	StartedAt time.Time
	// EndedAt is when the session closed. Zero ⇒ use the
	// caller-provided clock (or time.Now when nil).
	EndedAt time.Time
	// TranscriptPath is the absolute path to a Claude Code
	// JSONL transcript. Empty / unreadable ⇒ TokensTotal=0.
	TranscriptPath string
	// Pricing, when non-nil, enables DollarsEst. When nil the
	// synthesiser leaves DollarsEst=0 (the manifest still
	// declares cost_axes:[wallclock, tokens] either way; dollars
	// is opt-in via pricing config).
	Pricing *Pricing
}

// Pricing is per-million-token rates. Zero values disable that
// axis (e.g. unset CacheRead pricing ⇒ cache reads contribute
// nothing to DollarsEst).
type Pricing struct {
	InputDollarsPerMillion         float64
	OutputDollarsPerMillion        float64
	CacheCreationDollarsPerMillion float64
	CacheReadDollarsPerMillion     float64
}

// TokenBreakdown is the per-axis sum the parser returns. Total()
// is the value that lands on CostMeter.TokensTotal.
type TokenBreakdown struct {
	Input         int64
	Output        int64
	CacheCreation int64
	CacheRead     int64
}

// Total is the canonical "all tokens consumed" sum the manifest's
// cost_axes:[tokens] axis aggregates.
func (b TokenBreakdown) Total() int64 {
	return b.Input + b.Output + b.CacheCreation + b.CacheRead
}

// SynthesizeCostMeter computes a core.CostMeter from the
// telemetry bundle. now is injected for deterministic tests; nil
// resolves to time.Now. Never returns an error — a missing
// transcript / parse failure degrades to TokensTotal=0 so the
// rollup ships partial-but-useful data; the bead's "falls back
// gracefully" DoD is captured in this signature.
func SynthesizeCostMeter(t SessionTelemetry, now func() time.Time) core.CostMeter {
	if now == nil {
		now = time.Now
	}
	out := core.CostMeter{}

	// Wallclock — clamp to 0 when StartedAt is zero or EndedAt
	// is before StartedAt (stale telemetry).
	if !t.StartedAt.IsZero() {
		end := t.EndedAt
		if end.IsZero() {
			end = now()
		}
		secs := end.Sub(t.StartedAt).Seconds()
		if secs > 0 {
			out.WallclockSeconds = secs
		}
	}

	// Tokens — read the transcript when present. Errors degrade
	// silently (the rollup still has wallclock).
	if t.TranscriptPath != "" {
		if breakdown, err := readTranscriptTokensFromPath(t.TranscriptPath); err == nil {
			out.TokensTotal = breakdown.Total()
			if t.Pricing != nil {
				out.DollarsEst = estimateDollars(breakdown, *t.Pricing)
			}
		}
	}
	return out
}

// SynthesizeCostSamples returns CostSamples instead of a single
// CostMeter — useful when a caller wants to push axis-tagged
// samples into Session.CostSamples for downstream aggregation.
// One sample per non-zero axis; tokens collapse the breakdown
// into a single sample (the per-axis breakdown is preserved by
// the manifest's cost_axes declaration).
func SynthesizeCostSamples(t SessionTelemetry, now func() time.Time) []core.CostSample {
	if now == nil {
		now = time.Now
	}
	at := t.EndedAt
	if at.IsZero() {
		at = now()
	}
	meter := SynthesizeCostMeter(t, now)
	samples := make([]core.CostSample, 0, 3)
	if meter.WallclockSeconds > 0 {
		v := meter.WallclockSeconds
		samples = append(samples, core.CostSample{At: at, WallclockSeconds: &v})
	}
	if meter.TokensTotal > 0 {
		v := meter.TokensTotal
		samples = append(samples, core.CostSample{At: at, Tokens: &v})
	}
	if meter.DollarsEst > 0 {
		v := meter.DollarsEst
		samples = append(samples, core.CostSample{At: at, DollarsEst: &v})
	}
	return samples
}

// readTranscriptTokensFromPath reads the JSONL file and sums
// usage across every assistant message. Errors propagate so the
// caller can decide whether to degrade silently.
func readTranscriptTokensFromPath(path string) (TokenBreakdown, error) {
	f, err := os.Open(path)
	if err != nil {
		return TokenBreakdown{}, err
	}
	defer f.Close()
	return ParseTranscriptTokens(f)
}

// ParseTranscriptTokens parses a Claude Code transcript JSONL
// stream and returns the per-axis token breakdown. Lines that
// don't contain a usage block are ignored; malformed JSON lines
// are skipped (a single bad line shouldn't tank a session's
// rollup). Surface buffer is sized for transcripts with very
// long messages — production transcripts have multi-MB lines
// when long tool outputs land verbatim.
func ParseTranscriptTokens(r io.Reader) (TokenBreakdown, error) {
	const maxLineSize = 64 * 1024 * 1024 // 64 MiB safety ceiling
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1<<20), maxLineSize)

	var out TokenBreakdown
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var row transcriptRow
		if err := json.Unmarshal(line, &row); err != nil {
			continue // skip malformed lines silently
		}
		if row.Message == nil {
			continue
		}
		u := row.Message.Usage
		if u == nil {
			continue
		}
		out.Input += int64(u.InputTokens)
		out.Output += int64(u.OutputTokens)
		out.CacheCreation += int64(u.CacheCreationInputTokens)
		out.CacheRead += int64(u.CacheReadInputTokens)
	}
	if err := scanner.Err(); err != nil {
		return out, err
	}
	return out, nil
}

// transcriptRow is the subset of the Claude Code JSONL row this
// parser cares about. The transcript carries many other fields
// (parentUuid, timestamp, gitBranch, etc.) — we don't unmarshal
// them to keep the parse cheap.
type transcriptRow struct {
	Message *transcriptMessage `json:"message,omitempty"`
}

type transcriptMessage struct {
	Usage *transcriptUsage `json:"usage,omitempty"`
}

type transcriptUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// estimateDollars projects a TokenBreakdown onto a Pricing config.
// Per-million arithmetic; returns 0 when every rate is zero.
func estimateDollars(b TokenBreakdown, p Pricing) float64 {
	const million = 1_000_000.0
	dollars := 0.0
	dollars += float64(b.Input) / million * p.InputDollarsPerMillion
	dollars += float64(b.Output) / million * p.OutputDollarsPerMillion
	dollars += float64(b.CacheCreation) / million * p.CacheCreationDollarsPerMillion
	dollars += float64(b.CacheRead) / million * p.CacheReadDollarsPerMillion
	if dollars < 0 {
		return 0
	}
	return dollars
}

// LoadDefaultPricing reads pricing from environment variables.
// Returns nil when no rate is configured — the rollup then leaves
// DollarsEst=0 and the manifest's cost_axes still declares
// [wallclock, tokens] only.
//
//	GEMBA_GASTOWN_PRICE_INPUT_PER_MILLION
//	GEMBA_GASTOWN_PRICE_OUTPUT_PER_MILLION
//	GEMBA_GASTOWN_PRICE_CACHE_CREATION_PER_MILLION
//	GEMBA_GASTOWN_PRICE_CACHE_READ_PER_MILLION
//
// Per-million dollar amounts as floats. Missing env vars default
// to zero (that axis disabled); a single set var enables the
// returned Pricing. Designed so an operator can opt in for one
// model (just input + output) without configuring all four.
func LoadDefaultPricing() *Pricing {
	p := Pricing{
		InputDollarsPerMillion:         envFloat("GEMBA_GASTOWN_PRICE_INPUT_PER_MILLION"),
		OutputDollarsPerMillion:        envFloat("GEMBA_GASTOWN_PRICE_OUTPUT_PER_MILLION"),
		CacheCreationDollarsPerMillion: envFloat("GEMBA_GASTOWN_PRICE_CACHE_CREATION_PER_MILLION"),
		CacheReadDollarsPerMillion:     envFloat("GEMBA_GASTOWN_PRICE_CACHE_READ_PER_MILLION"),
	}
	if p.InputDollarsPerMillion == 0 && p.OutputDollarsPerMillion == 0 &&
		p.CacheCreationDollarsPerMillion == 0 && p.CacheReadDollarsPerMillion == 0 {
		return nil
	}
	return &p
}

func envFloat(key string) float64 {
	v := os.Getenv(key)
	if v == "" {
		return 0
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0
	}
	if f < 0 {
		return 0
	}
	return f
}

// ErrNoTranscript is returned by helpers that require a
// transcript but receive none. Surface only when the caller
// explicitly demands transcript-backed numbers (the default
// SynthesizeCostMeter degrades silently and never returns this).
var ErrNoTranscript = errors.New("gastown: no transcript path configured")
