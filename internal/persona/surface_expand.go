package persona

import (
	"strings"
)

// ExpandPaths resolves environment-variable references ($HOME, $GOPATH,
// $GOROOT, …) inside every path of s against the supplied env map. It
// is the spawn-time companion to [DefaultToolingPaths]: the defaults
// are declared with $VAR placeholders so they live with the persona
// package (host-agnostic), and consumers expand them against the
// concrete host environment when materializing mounts (gm-utik).
//
// Behavior:
//
//   - Each occurrence of "$NAME" or "${NAME}" is replaced with
//     env["NAME"]. Unknown / unset variables drop the path entirely
//     (the "skips paths whose envvar is unset" half of the gm-utik
//     DoD) — passing such a path through to the container backend
//     would emit a literal "$HOME/..." mount source, which Docker
//     would happily accept as a relative path on the daemon and fail
//     in surprising ways.
//   - Empty or missing-from-env replacements drop the path. Concrete
//     paths (no $) round-trip unchanged.
//   - Path order is preserved on retained entries; dropped entries
//     don't shift later indices.
//
// Pure: no os.Getenv calls so tests can pin behavior with an
// explicit env map. Production callers pass [EnvFromOS].
func ExpandPaths(s Surface, env map[string]string) Surface {
	expanded := s
	expanded.Cwd = expandOne(s.Cwd, env)
	expanded.WorkspaceMetadata = expandOne(s.WorkspaceMetadata, env)
	expanded.AdditionalWrites = expandSlice(s.AdditionalWrites, env)
	expanded.AdditionalReads = expandSlice(s.AdditionalReads, env)
	expanded.SiblingReads = expandSlice(s.SiblingReads, env)
	expanded.ToolingPaths = expandSlice(s.ToolingPaths, env)
	return expanded
}

// EnvFromOS snapshots the variables ExpandPaths cares about from the
// process environment. Production code uses this; tests use a literal
// map. The variables tracked here are the closed set the
// [DefaultToolingPaths] template references — adding a new $VAR to
// that template means adding it here too.
func EnvFromOS(getenv func(string) string) map[string]string {
	keys := []string{"HOME", "GOPATH", "GOROOT", "USER"}
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		v := getenv(k)
		if v != "" {
			out[k] = v
		}
	}
	return out
}

// expandSlice runs expandOne over every entry, dropping the ones that
// expand to empty (unset variable). Returned slice is fresh — never
// shares storage with the input.
func expandSlice(in []string, env map[string]string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, p := range in {
		if e := expandOne(p, env); e != "" {
			out = append(out, e)
		}
	}
	return out
}

// expandOne replaces $NAME / ${NAME} in p against env. Returns "" if
// any reference resolves to "" (an unset variable invalidates the
// whole path — partial expansion would produce nonsense like
// "/pkg/mod" from "$GOPATH/pkg/mod" without GOPATH). p with no $ is
// returned verbatim.
func expandOne(p string, env map[string]string) string {
	if p == "" || !strings.ContainsRune(p, '$') {
		return p
	}
	out := make([]byte, 0, len(p))
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c != '$' {
			out = append(out, c)
			continue
		}
		// $ at end-of-string is a literal $ — emit and stop.
		if i == len(p)-1 {
			out = append(out, '$')
			break
		}
		next := p[i+1]
		if next == '{' {
			// ${NAME}
			end := strings.IndexByte(p[i+2:], '}')
			if end < 0 {
				// Unterminated brace — treat the rest as literal so we
				// don't silently swallow input.
				out = append(out, p[i:]...)
				return string(out)
			}
			name := p[i+2 : i+2+end]
			val, ok := env[name]
			if !ok || val == "" {
				return ""
			}
			out = append(out, val...)
			i += 2 + end // skip ${NAME}
			continue
		}
		// $NAME — name runs to the next non-identifier character.
		end := i + 1
		for end < len(p) && isIdentRune(p[end]) {
			end++
		}
		name := p[i+1 : end]
		if name == "" {
			// $ followed by non-identifier — emit the bare $ and
			// continue with the next char.
			out = append(out, '$')
			continue
		}
		val, ok := env[name]
		if !ok || val == "" {
			return ""
		}
		out = append(out, val...)
		i = end - 1
	}
	return string(out)
}

func isIdentRune(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') ||
		(b >= '0' && b <= '9') || b == '_'
}
