package gembatesting

import (
	"fmt"
	"io"
	"strings"
)

// WriteTextReport emits a human-readable per-group summary of r to w.
// It is the default format for `gemba adaptor test` and mirrors what
// go test -v shows, except that skipped and not-applicable groups are
// called out explicitly so operators don't misread a missing fixture
// as a pass.
func WriteTextReport(w io.Writer, r *Report) {
	fmt.Fprintf(w, "== Gemba adaptor conformance: %s plane ==\n", r.Plane)
	if r.AdaptorName != "" {
		fmt.Fprintf(w, "adaptor:          %s %s\n", r.AdaptorName, r.AdaptorVersion)
	}
	fmt.Fprintf(w, "protocol_version: %s\n", r.ProtocolVersion)
	fmt.Fprintln(w)

	for _, g := range r.Groups {
		fmt.Fprintf(w, "Group %s\n", g.Name)
		if g.NotApplicable && len(g.Probes) == 0 {
			fmt.Fprintf(w, "  [n/a] %s\n", g.Note)
			continue
		}
		for _, p := range g.Probes {
			status := "PASS"
			switch {
			case p.Skipped:
				status = "SKIP"
			case !p.Passed:
				status = "FAIL"
			}
			fmt.Fprintf(w, "  [%s] %s\n", status, p.Name)
			for _, m := range p.Messages {
				for _, line := range strings.Split(m, "\n") {
					fmt.Fprintf(w, "        %s\n", line)
				}
			}
		}
	}
	fmt.Fprintln(w)

	passed, failed, skipped := r.Totals()
	verdict := "GREEN"
	if !r.Passed() {
		verdict = "RED"
	}
	fmt.Fprintf(w, "Summary: %s (passed=%d failed=%d skipped=%d)\n",
		verdict, passed, failed, skipped)
}
