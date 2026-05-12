// gm-o9t8.1.5.5 — `gemba logs` CLI.
//
// Two modes:
//
//	gemba logs <run-id>          — print the persisted transcript and exit.
//	                                Currently NOT IMPLEMENTED: the server
//	                                doesn't expose a transcript-replay
//	                                endpoint yet (filed as follow-up).
//	                                Returns a helpful error pointing at
//	                                `-f` as the working surface.
//
//	gemba logs -f <run-id>       — attach to /events filtered to that
//	                                work item. If the connection drops
//	                                (read error, server close) we reconnect
//	                                with Last-Event-ID set to the last
//	                                event we saw, up to --max-retries
//	                                times. This reconnect loop is the
//	                                key differentiator vs `gemba run`,
//	                                which intentionally does NOT
//	                                reconnect on its own.

package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/spf13/cobra"

	"github.com/GembaCore/gemba-core/internal/sse"
)

func newLogsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs <run-id>",
		Short: "Print transcript for <run-id> or follow its live event stream",
		Long: `Without -f: print the persisted transcript for <run-id> (not yet
implemented — server lacks a replay endpoint; use -f for now).

With -f: attach to /events filtered to <run-id>. On a dropped
connection the client reconnects with Last-Event-ID and keeps
going, up to --max-retries times.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			follow, _ := cmd.Flags().GetBool("follow")
			if !follow {
				return errors.New(
					"transcript replay not yet implemented (server " +
						"lacks GET /api/runs/{id}/transcript). Use -f " +
						"to attach to the live stream instead.")
			}
			return runLogsFollow(cmd, args[0])
		},
	}
	cmd.Flags().BoolP("follow", "f", false, "follow the live event stream")
	cmd.Flags().String("base-url", "", "base URL of a running gemba serve (default http://localhost:7666)")
	cmd.Flags().Bool("json", false, "print each event's raw JSON payload on its own line")
	cmd.Flags().Int("max-retries", 5, "max reconnect attempts before giving up")
	return cmd
}

func runLogsFollow(cmd *cobra.Command, runID string) error {
	baseURL, _ := cmd.Flags().GetString("base-url")
	if baseURL == "" {
		baseURL = "http://localhost:7666"
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	maxRetries, _ := cmd.Flags().GetInt("max-retries")

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	lastID := ""
	backoff := 250 * time.Millisecond
	for attempt := 0; ; attempt++ {
		newLast, err := followOnce(ctx, cmd, baseURL, runID, lastID, asJSON)
		if newLast != "" {
			lastID = newLast
		}
		if err == nil || errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) {
			return nil
		}
		if attempt >= maxRetries {
			return fmt.Errorf("logs -f: giving up after %d retries: %w", attempt, err)
		}
		fmt.Fprintf(cmd.ErrOrStderr(),
			"logs: connection dropped (%v); reconnecting (attempt %d/%d, last-event-id=%q)\n",
			err, attempt+1, maxRetries, lastID)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		if backoff < 4*time.Second {
			backoff *= 2
		}
	}
}

// followOnce attaches once and returns the last-seen event id plus the
// error that ended the stream. A nil error means the server closed
// cleanly (EOF) — at that point the run is done and we stop retrying.
// Anything else (network error, non-2xx status) is retryable.
func followOnce(ctx context.Context, cmd *cobra.Command, baseURL, runID, lastID string, asJSON bool) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return lastID, fmt.Errorf("parse base-url: %w", err)
	}
	u.Path = "/events"
	q := u.Query()
	q.Set("work_item_id", runID)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return lastID, err
	}
	req.Header.Set("Accept", "text/event-stream")
	if lastID != "" {
		req.Header.Set("Last-Event-ID", lastID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return lastID, fmt.Errorf("GET %s: %w", u.String(), err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return lastID, fmt.Errorf("GET %s: %s", u.String(), resp.Status)
	}

	ch := make(chan sse.Event, 32)
	done := make(chan error, 1)
	go func() { done <- sse.Consume(ctx, resp.Body, ch) }()

	out := cmd.OutOrStdout()
	for {
		select {
		case <-ctx.Done():
			<-done
			return lastID, ctx.Err()
		case ev, ok := <-ch:
			if !ok {
				return lastID, <-done
			}
			if ev.ID != "" {
				lastID = ev.ID
			}
			renderEvent(out, ev, asJSON)
		case err := <-done:
			// Drain anything the parser pushed before exiting.
			for {
				select {
				case ev := <-ch:
					if ev.ID != "" {
						lastID = ev.ID
					}
					renderEvent(out, ev, asJSON)
				default:
					return lastID, err
				}
			}
		}
	}
}
