// events.go: SSE transport helper for the work-item /events stream.
//
// The generated client at gen/client.gen.go treats text/event-stream
// as an opaque body and doesn't expose a streaming-friendly method.
// To keep the SSE attach path on the same auth + User-Agent injection
// pipeline as every other CLI verb (gm-o9t8.1.11), we hand-build the
// GET request through c.httpDoer with the wrapper's request editors
// applied — identical to how work_items.go threads its JSON helpers.
//
// The returned io.ReadCloser is intentionally raw: the SSE parser
// in internal/sse owns frame decoding. This file is strictly the
// transport seam.
//
// gm-o9t8.1.11

package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// WorkItemEvents opens GET <server>/events?work_item_id=<runID> as an
// SSE stream and returns the response body for the caller to parse.
//
// When lastEventID is non-empty it is sent as the Last-Event-ID header
// so the server can replay missed frames on reconnect. Bearer auth and
// User-Agent are injected via the wrapper's request editors. Confirm
// header injection is a no-op for GET.
//
// On non-2xx the body is read for the error envelope and a typed
// sentinel (e.g. ErrUnauthorized, ErrNotFound) is returned via mapErr.
// On 2xx the caller owns Close() on the returned ReadCloser.
func (c *Client) WorkItemEvents(ctx context.Context, runID, lastEventID string) (io.ReadCloser, error) {
	if strings.TrimSpace(runID) == "" {
		return nil, fmt.Errorf("work item events: runID is required")
	}
	full := strings.TrimRight(c.server, "/") + "/events"
	u, err := url.Parse(full)
	if err != nil {
		return nil, fmt.Errorf("work item events: parse url: %w", err)
	}
	q := u.Query()
	q.Set("work_item_id", runID)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("work item events: build request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	if err := c.addAuth(ctx, req); err != nil {
		return nil, err
	}
	if err := c.addUserAgent(ctx, req); err != nil {
		return nil, err
	}
	// addConfirm is a no-op on GET, but call it for parity with the
	// JSON path so the request editor pipeline is identical.
	if err := c.addConfirm(ctx, req); err != nil {
		return nil, err
	}
	resp, err := c.httpDoer.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", u.String(), err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, mapErr(resp, raw)
	}
	return resp.Body, nil
}
