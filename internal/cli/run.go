// gm-o9t8.1.5.5 — `gemba run` CLI.
//
// One-shot operator command: dispatch the cascade leaves under <bead>,
// then attach to the live /events stream filtered to that work item.
// Ctrl+C detaches (cancels the SSE context) without killing the run,
// emits a "detached. resume with: gemba logs -f <run_id>" hint, and
// exits with code 130 (the SIGINT convention).
//
// Flags:
//
//	--base-url URL   target server (default http://localhost:7666)
//	--json           emit raw SSE data lines (one JSON event per line)
//	--no-attach      skip the SSE step; equivalent to `gemba agent dispatch`
//	--agent-type     forwarded to the cascade-dispatch request
//	--limit          forwarded to the cascade-dispatch request

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/GembaCore/gemba-core/internal/cli/serverconfig"
	"github.com/GembaCore/gemba-core/internal/sse"
)

// exitCodeDetached signals "interrupted by SIGINT" per the de-facto
// shell convention (128 + 2). Cobra has no native way to set the exit
// code, so we surface it through this sentinel — runRunCmd writes it
// to os.Exit before returning nil to keep cobra's error printing quiet.
const exitCodeDetached = 130

func newRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <bead-id>",
		Short: "Cascade-dispatch <bead-id> and attach to its live event stream",
		Long: `Dispatch the runnable leaves beneath <bead-id> (same call as
'gemba agent dispatch'), then attach to /events filtered to that work
item. Ctrl+C detaches without killing the run; resume later with:

  gemba logs -f <run-id>`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRunCmd(cmd, args[0])
		},
	}
	cmd.Flags().String("base-url", "", "base URL of a running gemba serve (default http://localhost:7666)")
	cmd.Flags().Bool("json", false, "print each event's raw JSON payload on its own line")
	cmd.Flags().Bool("no-attach", false, "skip the SSE attach (fire-and-forget)")
	cmd.Flags().String("agent-type", "", "agent_type to pass to cascade-dispatch")
	cmd.Flags().Int("limit", 0, "limit on dispatched leaves (0 = server default)")
	return cmd
}

func runRunCmd(cmd *cobra.Command, beadID string) error {
	baseURL, _ := cmd.Flags().GetString("base-url")
	resolved, err := serverconfig.Resolve(baseURL)
	if err != nil {
		return err
	}
	baseURL = resolved
	asJSON, _ := cmd.Flags().GetBool("json")
	noAttach, _ := cmd.Flags().GetBool("no-attach")
	agentType, _ := cmd.Flags().GetString("agent-type")
	limit, _ := cmd.Flags().GetInt("limit")

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	resp, err := postCascadeDispatch(ctx, baseURL, beadID, agentType, limit)
	if err != nil {
		return err
	}
	runID := resp.WrapperID
	if runID == "" {
		runID = beadID
	}
	fmt.Fprintf(cmd.OutOrStdout(), "dispatched: run_id=%s\n", runID)

	if noAttach {
		return nil
	}

	// SIGINT detaches: cancel the SSE context, print the resume hint,
	// and exit 130. Wrap in a fresh context so we own the cancel.
	sseCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	detached := false
	err = attachEvents(sseCtx, cmd, baseURL, runID, "", asJSON)
	if err != nil {
		// signal.NotifyContext fires ctx.Err() == context.Canceled on
		// SIGINT. Treat that path as a clean detach rather than an
		// error.
		if sseCtx.Err() != nil && ctx.Err() == nil {
			detached = true
		} else {
			return err
		}
	}
	if detached {
		fmt.Fprintf(cmd.OutOrStdout(),
			"detached. resume with: gemba logs -f %s\n", runID)
		// Cobra eats os.Exit-equivalent codes; the parent process gets
		// 130 by exiting directly. This is the SIGINT convention.
		os.Exit(exitCodeDetached)
	}
	return nil
}

// attachEvents opens GET /events filtered to work_item_id=runID and
// pipes parsed SSE events to cmd.OutOrStdout. When asJSON is true the
// raw data payload is emitted verbatim (one JSON object per line);
// otherwise we render a short human line "<event-type> <data>".
//
// lastEventID, if non-empty, is sent as Last-Event-ID so the server
// can (eventually) replay missed events. Today the server doesn't act
// on it (see events_stream.go), but sending it costs nothing and makes
// the client forward-compatible.
func attachEvents(ctx context.Context, cmd *cobra.Command, baseURL, runID, lastEventID string, asJSON bool) error {
	u, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("parse base-url: %w", err)
	}
	u.Path = "/events"
	q := u.Query()
	q.Set("work_item_id", runID)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", u.String(), err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return fmt.Errorf("GET %s: %s", u.String(), resp.Status)
	}

	ch := make(chan sse.Event, 32)
	done := make(chan error, 1)
	go func() { done <- sse.Consume(ctx, resp.Body, ch) }()

	out := cmd.OutOrStdout()
	for {
		select {
		case <-ctx.Done():
			// Wait for Consume to unwind so we don't leak the goroutine.
			<-done
			return ctx.Err()
		case ev, ok := <-ch:
			if !ok {
				return <-done
			}
			renderEvent(out, ev, asJSON)
		case err := <-done:
			// Drain any tail events the consumer pushed before exit.
			for {
				select {
				case ev := <-ch:
					renderEvent(out, ev, asJSON)
				default:
					return err
				}
			}
		}
	}
}

func renderEvent(out interface{ Write(p []byte) (int, error) }, ev sse.Event, asJSON bool) {
	if asJSON {
		fmt.Fprintf(out, "%s\n", ev.Data)
		return
	}
	typ := ev.Type
	if typ == "" {
		typ = "message"
	}
	// Try to surface a single useful field when the payload is a
	// structured GembaEvent; otherwise fall back to the raw bytes.
	var probe struct {
		Kind     string `json:"kind"`
		Plane    string `json:"plane"`
		Topic    string `json:"topic"`
		Summary  string `json:"summary"`
		Message  string `json:"message"`
		WorkItem string `json:"work_item_id"`
	}
	if err := json.Unmarshal(ev.Data, &probe); err == nil {
		switch {
		case probe.Summary != "":
			fmt.Fprintf(out, "[%s] %s\n", typ, probe.Summary)
			return
		case probe.Message != "":
			fmt.Fprintf(out, "[%s] %s\n", typ, probe.Message)
			return
		}
	}
	fmt.Fprintf(out, "[%s] %s\n", typ, string(ev.Data))
}
