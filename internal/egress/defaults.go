package egress

// Defaults returns the hard-coded baseline egress allow-list plus the
// terminal default-deny rule. The list is returned in priority order
// (highest first), matching Effective()'s output contract.
//
// Default rules:
//   - carry an empty WorkspaceID (IsDefault() returns true)
//   - have stable, well-known IDs prefixed "default-" so the SPA and
//     CLI can render them consistently across versions
//   - are NEVER persisted to the egress_rules table and NEVER deletable
//     via the HTTP surface
//   - can be SHADOWED by a higher-priority workspace rule. Workspace
//     rules use Priority > 0; defaults occupy Priority ≤ 0 so any
//     positive-priority workspace rule outranks them.
//
// The allow-list is the minimum set of egress endpoints a typical
// Gemba agent session needs to (a) talk to model providers, (b) clone
// repos / install dependencies. The runtime enforcer (3.6.2) starts
// the workspace at default-deny and walks Effective() to decide
// whether to permit each outbound flow.
func Defaults() []Rule {
	rules := []Rule{
		allowTCP("default-anthropic", "api.anthropic.com", 443, 443, 0),
		allowTCP("default-openai", "api.openai.com", 443, 443, -1),
		allowTCP("default-github", "github.com", 443, 443, -2),
		allowTCP("default-github-api", "api.github.com", 443, 443, -3),
		allowTCP("default-ghcr", "ghcr.io", 443, 443, -4),
		allowTCP("default-npm", "registry.npmjs.org", 443, 443, -5),
		allowTCP("default-pypi", "pypi.org", 443, 443, -6),
		allowTCP("default-pypi-files", "files.pythonhosted.org", 443, 443, -7),
		allowTCP("default-debian", "deb.debian.org", 80, 443, -8),
		allowTCP("default-goproxy", "proxy.golang.org", 443, 443, -9),
		allowTCP("default-gosum", "sum.golang.org", 443, 443, -10),
		// Terminal: deny everything else. Highest-numbered (= most
		// negative) priority so any specific allow rule above wins.
		{
			ID:          "default-deny-all",
			Action:      ActionDeny,
			Proto:       ProtoAny,
			HostPattern: "**",
			PortStart:   0,
			PortEnd:     0,
			Priority:    -1_000_000,
		},
	}
	return rules
}

// IsDefaultID reports whether id refers to a baseline rule. The
// SQLStore.Delete path consults this so operators cannot remove a
// default by inserting and then deleting a stub row.
func IsDefaultID(id string) bool {
	for _, d := range Defaults() {
		if d.ID == id {
			return true
		}
	}
	return false
}

func allowTCP(id, host string, portStart, portEnd, priority int) Rule {
	return Rule{
		ID:          id,
		Action:      ActionAllow,
		Proto:       ProtoTCP,
		HostPattern: host,
		PortStart:   portStart,
		PortEnd:     portEnd,
		Priority:    priority,
	}
}
