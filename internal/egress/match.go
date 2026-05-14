package egress

import "strings"

// MatchHost reports whether host satisfies pattern under the egress
// glob dialect. The dialect operates on the dot-separated labels of a
// DNS host:
//
//   - A literal label matches itself exactly (case-insensitive).
//   - "*" matches exactly one label.
//   - "**" matches one or more consecutive labels.
//   - A bare "*" pattern matches any single-label host; "**" matches
//     any host.
//
// Examples (matrix lives in match_test.go):
//
//	pattern               host                  match?
//	"api.anthropic.com"   "api.anthropic.com"   yes
//	"*.anthropic.com"     "api.anthropic.com"   yes
//	"*.anthropic.com"     "foo.bar.anthropic.com" no  (* is one label)
//	"**.anthropic.com"    "foo.bar.anthropic.com" yes
//	"**.anthropic.com"    "anthropic.com"       no  (** = one or more)
//	"github.com"          "GitHub.COM"          yes (case-insensitive)
//
// Matching is anchored on both ends — the pattern must consume the
// whole host. The matcher is purely syntactic; CIDR / IP literals are
// out of scope for this storage-only story (3.6.1).
func MatchHost(pattern, host string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	host = strings.ToLower(strings.TrimSpace(host))
	if pattern == "" || host == "" {
		return false
	}
	pLabels := strings.Split(pattern, ".")
	hLabels := strings.Split(host, ".")
	return matchLabels(pLabels, hLabels)
}

// matchLabels does the recursive backtracking walk over the label
// slices. "*" consumes exactly one label; "**" consumes one or more
// labels (greedy, with backtracking for trailing literals).
func matchLabels(p, h []string) bool {
	for len(p) > 0 {
		switch p[0] {
		case "**":
			// Must consume at least one label, may consume many.
			if len(h) == 0 {
				return false
			}
			// Try every possible split: ** absorbs 1..len(h) labels.
			rest := p[1:]
			for i := 1; i <= len(h); i++ {
				if matchLabels(rest, h[i:]) {
					return true
				}
			}
			return false
		case "*":
			if len(h) == 0 {
				return false
			}
			p = p[1:]
			h = h[1:]
		default:
			if len(h) == 0 || p[0] != h[0] {
				return false
			}
			p = p[1:]
			h = h[1:]
		}
	}
	return len(h) == 0
}

// MatchRule reports whether r matches the (proto, host, port) tuple.
// Used by the runtime enforcer in 3.6.2 — exposed here so the storage
// tests can prove the data model line up with the matcher.
func MatchRule(r Rule, proto Proto, host string, port int) bool {
	if r.Proto != ProtoAny && r.Proto != proto {
		return false
	}
	if !MatchHost(r.HostPattern, host) {
		return false
	}
	if r.PortStart == 0 && r.PortEnd == 0 {
		return true
	}
	return port >= r.PortStart && port <= r.PortEnd
}
