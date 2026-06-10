package analyzer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// llmRequest is the JSON contract sent to GEMBA_ANALYZER_LLM. The server
// is expected to return a `ProposalDoc` (same shape returned by
// heuristicClassify) — or an error JSON body. The contract is
// intentionally minimal so any small HTTP-fronted LLM (llama.cpp server,
// Ollama, a private endpoint) can satisfy it.
type llmRequest struct {
	Schema    string       `json:"schema"`    // pinned to "gemba.analyzer.v1"
	Markdown  string       `json:"markdown"`  // raw extracted payload text
	Heuristic *ProposalDoc `json:"heuristic"` // first-pass proposal
}

type llmResponse struct {
	Proposal *ProposalDoc `json:"proposal"`
	Error    string       `json:"error,omitempty"`
}

// llmRefine POSTs the heuristic proposal + raw markdown to endpoint and
// returns the LLM-refined proposal. Non-2xx responses, network errors,
// timeouts, and missing/empty `proposal` fields all yield (nil, err) so
// the caller falls back to the heuristic.
func llmRefine(ctx context.Context, endpoint string, timeoutMS int, markdown string, heuristic *ProposalDoc) (*ProposalDoc, error) {
	if timeoutMS <= 0 {
		timeoutMS = 10_000
	}
	body, err := json.Marshal(llmRequest{
		Schema:    "gemba.analyzer.v1",
		Markdown:  markdown,
		Heuristic: heuristic,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal llm request: %w", err)
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build llm request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("llm http status %d", resp.StatusCode)
	}
	var out llmResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode llm response: %w", err)
	}
	if out.Error != "" {
		return nil, fmt.Errorf("llm error: %s", out.Error)
	}
	if out.Proposal == nil || len(out.Proposal.Beads) == 0 {
		return nil, fmt.Errorf("llm returned empty proposal")
	}
	return out.Proposal, nil
}
