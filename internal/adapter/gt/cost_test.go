package gt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GembaCore/gemba-core/core"
)

const sampleTranscript = `{"type":"permission-mode","permissionMode":"bypassPermissions","sessionId":"s1"}
{"type":"file-history-snapshot"}
{"parentUuid":"a","message":{"model":"claude-opus-4-7","role":"assistant","usage":{"input_tokens":10,"cache_creation_input_tokens":1000,"cache_read_input_tokens":2000,"output_tokens":50}},"timestamp":"2026-04-23T17:54:46.072Z"}
{"parentUuid":"b","message":{"model":"claude-opus-4-7","role":"assistant","usage":{"input_tokens":5,"cache_creation_input_tokens":0,"cache_read_input_tokens":3000,"output_tokens":80}},"timestamp":"2026-04-23T17:55:46.072Z"}
{"parentUuid":"c","message":{"model":"claude-opus-4-7","role":"assistant","content":[{"type":"text","text":"hello"}]}}
malformed not json
`

func writeTranscript(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

// ── ParseTranscriptTokens ───────────────────────────────────────

func TestParseTranscriptTokens_SumsAllAxes(t *testing.T) {
	got, err := ParseTranscriptTokens(strings.NewReader(sampleTranscript))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := TokenBreakdown{
		Input:         15,   // 10 + 5
		Output:        130,  // 50 + 80
		CacheCreation: 1000, // 1000 + 0
		CacheRead:     5000, // 2000 + 3000
	}
	if got != want {
		t.Errorf("breakdown:\n got %+v\nwant %+v", got, want)
	}
	if got.Total() != 6145 {
		t.Errorf("Total = %d, want 6145", got.Total())
	}
}

func TestParseTranscriptTokens_EmptyAndAllNoise(t *testing.T) {
	got, err := ParseTranscriptTokens(strings.NewReader(""))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Total() != 0 {
		t.Errorf("empty input: got Total=%d, want 0", got.Total())
	}
	got, err = ParseTranscriptTokens(strings.NewReader("not json\nalso not json\n"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Total() != 0 {
		t.Errorf("noise input: got Total=%d, want 0", got.Total())
	}
}

func TestParseTranscriptTokens_MalformedRowsSkipped(t *testing.T) {
	body := `{"parentUuid":"a","message":{"usage":{"input_tokens":7,"output_tokens":3}}}
{ this line breaks json }
{"parentUuid":"b","message":{"usage":{"input_tokens":2,"output_tokens":4}}}
`
	got, err := ParseTranscriptTokens(strings.NewReader(body))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Input != 9 || got.Output != 7 {
		t.Errorf("expected Input=9 Output=7; got %+v", got)
	}
}

// ── SynthesizeCostMeter ─────────────────────────────────────────

func TestSynthesizeCostMeter_HappyPath(t *testing.T) {
	path := writeTranscript(t, sampleTranscript)
	start := time.Date(2026, 4, 23, 17, 54, 0, 0, time.UTC)
	end := start.Add(2 * time.Minute)
	got := SynthesizeCostMeter(SessionTelemetry{
		StartedAt:      start,
		EndedAt:        end,
		TranscriptPath: path,
	}, nil)
	if got.WallclockSeconds != 120 {
		t.Errorf("WallclockSeconds = %v, want 120", got.WallclockSeconds)
	}
	if got.TokensTotal != 6145 {
		t.Errorf("TokensTotal = %d, want 6145", got.TokensTotal)
	}
	if got.DollarsEst != 0 {
		t.Errorf("DollarsEst = %v, want 0 (no pricing)", got.DollarsEst)
	}
}

func TestSynthesizeCostMeter_MissingTranscriptDegradesGracefully(t *testing.T) {
	start := time.Date(2026, 4, 23, 17, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	got := SynthesizeCostMeter(SessionTelemetry{
		StartedAt:      start,
		EndedAt:        end,
		TranscriptPath: "/nope/does-not-exist.jsonl",
	}, nil)
	// Wallclock still ships; tokens degrade to 0 quietly.
	if got.WallclockSeconds != 3600 {
		t.Errorf("WallclockSeconds = %v, want 3600", got.WallclockSeconds)
	}
	if got.TokensTotal != 0 {
		t.Errorf("TokensTotal = %d, want 0 with missing transcript", got.TokensTotal)
	}
}

func TestSynthesizeCostMeter_OpenSessionUsesNow(t *testing.T) {
	path := writeTranscript(t, sampleTranscript)
	start := time.Date(2026, 4, 23, 17, 0, 0, 0, time.UTC)
	frozen := start.Add(45 * time.Second)
	got := SynthesizeCostMeter(SessionTelemetry{
		StartedAt:      start,
		TranscriptPath: path,
	}, func() time.Time { return frozen })
	if got.WallclockSeconds != 45 {
		t.Errorf("WallclockSeconds = %v, want 45 (from now())", got.WallclockSeconds)
	}
}

func TestSynthesizeCostMeter_ZeroStartedAtNoWallclock(t *testing.T) {
	path := writeTranscript(t, sampleTranscript)
	got := SynthesizeCostMeter(SessionTelemetry{
		TranscriptPath: path,
	}, nil)
	if got.WallclockSeconds != 0 {
		t.Errorf("WallclockSeconds = %v, want 0 with zero start", got.WallclockSeconds)
	}
	// Tokens still ship.
	if got.TokensTotal == 0 {
		t.Errorf("TokensTotal = 0; expected non-zero from transcript")
	}
}

func TestSynthesizeCostMeter_PricingDrivesDollars(t *testing.T) {
	path := writeTranscript(t, sampleTranscript)
	got := SynthesizeCostMeter(SessionTelemetry{
		StartedAt:      time.Now().Add(-time.Minute),
		TranscriptPath: path,
		Pricing: &Pricing{
			InputDollarsPerMillion:         15,
			OutputDollarsPerMillion:        75,
			CacheCreationDollarsPerMillion: 18.75,
			CacheReadDollarsPerMillion:     1.5,
		},
	}, nil)
	// Hand-computed expectation:
	//   input        15 / 1e6 * 15           = 0.000225
	//   output      130 / 1e6 * 75           = 0.00975
	//   cache_create 1000 / 1e6 * 18.75      = 0.01875
	//   cache_read   5000 / 1e6 * 1.5        = 0.0075
	//   total                                  = 0.036225
	want := 0.036225
	if absDiff(got.DollarsEst, want) > 0.0001 {
		t.Errorf("DollarsEst = %v, want %v (±0.0001)", got.DollarsEst, want)
	}
}

func absDiff(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}

// ── SynthesizeCostSamples ───────────────────────────────────────

func TestSynthesizeCostSamples_ThreeAxesWhenAllPresent(t *testing.T) {
	path := writeTranscript(t, sampleTranscript)
	frozen := time.Date(2026, 4, 23, 17, 56, 0, 0, time.UTC)
	got := SynthesizeCostSamples(SessionTelemetry{
		StartedAt:      time.Date(2026, 4, 23, 17, 54, 0, 0, time.UTC),
		EndedAt:        frozen,
		TranscriptPath: path,
		Pricing:        &Pricing{InputDollarsPerMillion: 1, OutputDollarsPerMillion: 1, CacheCreationDollarsPerMillion: 1, CacheReadDollarsPerMillion: 1},
	}, nil)
	if len(got) != 3 {
		t.Fatalf("expected 3 samples; got %d", len(got))
	}
	hasTokens, hasWall, hasDollars := false, false, false
	for _, s := range got {
		if s.Tokens != nil {
			hasTokens = true
		}
		if s.WallclockSeconds != nil {
			hasWall = true
		}
		if s.DollarsEst != nil {
			hasDollars = true
		}
		if !s.At.Equal(frozen) {
			t.Errorf("sample At = %v, want %v", s.At, frozen)
		}
	}
	if !hasTokens || !hasWall || !hasDollars {
		t.Errorf("expected all three axes; got tokens=%v wall=%v dollars=%v",
			hasTokens, hasWall, hasDollars)
	}
}

func TestSynthesizeCostSamples_OmitsZeroAxes(t *testing.T) {
	got := SynthesizeCostSamples(SessionTelemetry{
		// No StartedAt → no wallclock; no transcript → no tokens.
	}, func() time.Time { return time.Date(2026, 4, 23, 17, 56, 0, 0, time.UTC) })
	if len(got) != 0 {
		t.Errorf("expected zero samples for zero telemetry; got %d", len(got))
	}
}

// ── LoadDefaultPricing ──────────────────────────────────────────

func TestLoadDefaultPricing_NoneSetReturnsNil(t *testing.T) {
	t.Setenv("GEMBA_GASTOWN_PRICE_INPUT_PER_MILLION", "")
	t.Setenv("GEMBA_GASTOWN_PRICE_OUTPUT_PER_MILLION", "")
	t.Setenv("GEMBA_GASTOWN_PRICE_CACHE_CREATION_PER_MILLION", "")
	t.Setenv("GEMBA_GASTOWN_PRICE_CACHE_READ_PER_MILLION", "")
	if p := LoadDefaultPricing(); p != nil {
		t.Errorf("expected nil pricing when no env set; got %+v", p)
	}
}

func TestLoadDefaultPricing_SinglePartialEnableActivates(t *testing.T) {
	t.Setenv("GEMBA_GASTOWN_PRICE_INPUT_PER_MILLION", "15")
	t.Setenv("GEMBA_GASTOWN_PRICE_OUTPUT_PER_MILLION", "")
	t.Setenv("GEMBA_GASTOWN_PRICE_CACHE_CREATION_PER_MILLION", "")
	t.Setenv("GEMBA_GASTOWN_PRICE_CACHE_READ_PER_MILLION", "")
	p := LoadDefaultPricing()
	if p == nil {
		t.Fatal("expected non-nil pricing with partial config")
	}
	if p.InputDollarsPerMillion != 15 {
		t.Errorf("InputDollarsPerMillion = %v, want 15", p.InputDollarsPerMillion)
	}
	if p.OutputDollarsPerMillion != 0 {
		t.Errorf("Output rate should be 0 when unset; got %v", p.OutputDollarsPerMillion)
	}
}

func TestLoadDefaultPricing_RejectsMalformed(t *testing.T) {
	t.Setenv("GEMBA_GASTOWN_PRICE_INPUT_PER_MILLION", "not-a-number")
	if p := LoadDefaultPricing(); p != nil {
		t.Errorf("expected nil pricing when value is malformed; got %+v", p)
	}
}

// ── manifest declares CostTokens ────────────────────────────────

func TestManifestDeclaresCostTokens(t *testing.T) {
	m := (&OrchestrationPlane{}).Describe()
	found := false
	for _, axis := range m.CostAxes {
		if axis == core.CostTokens {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("manifest must declare CostTokens; got %v", m.CostAxes)
	}
}
