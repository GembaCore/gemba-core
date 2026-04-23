// Package config: see doc.go for the overview.
package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
)

// ServeConfig captures every flag `gemba serve` accepts. Held on the CLI side
// and passed into the server; keep it a pure data type.
type ServeConfig struct {
	Listen string
	Port   int
	Open   bool

	AuthMode string
	// AuthToken, when non-empty, is the plaintext bearer compared against
	// incoming requests. Set it for tests and for --auth-token overrides;
	// production runs leave it empty and let the server verify against the
	// hashed file at AuthTokenHashPath.
	AuthToken string
	// AuthTokenHashPath points at the argon2id PHC hash file written by
	// `gemba auth token rotate`. Defaults to ~/.gemba/tokens/primary.
	AuthTokenHashPath string
	TLSCert           string
	TLSKey            string
	TLSSelfSigned     bool

	// City is the path to a Gas City workspace (preferred).
	// Town is the path to a Gas Town HQ (legacy; kept for back-compat).
	// Exactly one may be set; an empty workspace defers to auto-detect.
	City string
	Town string

	// BeadsDir is the workspace directory the Beads WorkPlane adaptor
	// targets — bd subprocesses spawn with this as their cwd. Empty
	// means "use the gemba server's cwd," which is the right default
	// when gemba is launched from inside a beads workspace.
	BeadsDir string

	// DoltURL is a mysql://user[:password]@host:port/dbname connection
	// string pointing at a Dolt server that already hosts a beads
	// database. When set, the server skips the bd CLI path and opens
	// a direct read-only SQL connection instead (gm-0fd). Mutually
	// exclusive with BeadsDir.
	DoltURL string

	// ConfigPath is an explicit gemba.toml override. Empty means "probe
	// the standard locations." File loading lands with a later bead;
	// serve threads the path through today so the flag surface is stable.
	ConfigPath string

	DangerouslySkipPermissions bool
}

// NormalizeListen splits c.Listen if it is in host:port form (e.g.
// "127.0.0.1:7666" or "[::1]:7666"), routing the port half into c.Port and
// reducing c.Listen to the host. This lets users type the universal Unix
// host:port idiom for --listen instead of having to remember --port is a
// separate flag.
//
// portFlagSet should be true if the caller (CLI) saw an explicit --port flag;
// supplying both forms is an error so precedence is never ambiguous.
func (c *ServeConfig) NormalizeListen(portFlagSet bool) error {
	host, port, err := net.SplitHostPort(c.Listen)
	if err != nil {
		// Not host:port form (missing colon, IPv6 literal without brackets,
		// empty string, etc). Leave Listen alone.
		return nil
	}
	if portFlagSet {
		return fmt.Errorf(
			"port specified twice: --listen %q includes a port and --port "+
				"was also given; pass one or the other", c.Listen)
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return fmt.Errorf("invalid port in --listen %q", c.Listen)
	}
	c.Listen = host
	c.Port = p
	return nil
}

// EffectiveAuthMode returns the auth mode that will actually be applied,
// after defaulting to "none" and normalizing.
func (c ServeConfig) EffectiveAuthMode() string {
	if c.AuthMode == "" {
		return "none"
	}
	return c.AuthMode
}

// ValidateBindPolicy refuses to start the server if the configuration would
// expose the API on a non-loopback interface without authentication.
//
// This is the single most important security check. See gm-e5.1 for the
// full rationale and test matrix.
func (c ServeConfig) ValidateBindPolicy() error {
	loopback, err := isLoopback(c.Listen)
	if err != nil {
		return fmt.Errorf("cannot resolve --listen %q: %w", c.Listen, err)
	}

	if loopback {
		return nil // localhost-only; always ok
	}

	if c.EffectiveAuthMode() == "none" {
		return fmt.Errorf(
			"non-loopback bind requires --auth; got --listen %s\n"+
				"  Pass --auth=token to generate a token, or keep the default bind\n"+
				"  (127.0.0.1). See docs/remote-access.md for details.",
			c.listenDisplay())
	}
	return nil
}

// ValidateWorkPlaneFlags rejects mutually-exclusive workplane
// configurations before serve walks any further. --beads-dir and
// --dolt-url both select a beads backend but by different means; a
// server asked to honor both would have to pick one and ignore the
// other, which is a bad operator surprise — explicit failure is the
// right move.
func (c ServeConfig) ValidateWorkPlaneFlags() error {
	if c.BeadsDir != "" && c.DoltURL != "" {
		return fmt.Errorf(
			"--beads-dir and --dolt-url are mutually exclusive; " +
				"pass one or the other\n" +
				"  --beads-dir routes reads+writes through the bd CLI\n" +
				"  --dolt-url opens a direct read-only SQL connection to Dolt")
	}
	return nil
}

// ResolveBeadsDir validates c.BeadsDir and returns the directory `bd`
// should be invoked from. The bd CLI discovers its workspace by walking
// up from cwd looking for `.beads/`, so the returned path is the rig
// root: either c.BeadsDir itself (when it contains `.beads/`) or its
// parent (when c.BeadsDir *is* `.beads/`). Accepting both forms matches
// how users talk about rigs in practice — `--beads-dir ~/gt/gemba` and
// `--beads-dir ~/gt/gemba/.beads` both mean the same rig.
//
// An empty c.BeadsDir returns ("", nil); callers decide whether that's
// an error. Mutual exclusion with --dolt-url is handled separately by
// ValidateWorkPlaneFlags.
func (c ServeConfig) ResolveBeadsDir() (string, error) {
	if c.BeadsDir == "" {
		return "", nil
	}
	abs, err := filepath.Abs(c.BeadsDir)
	if err != nil {
		return "", fmt.Errorf("--beads-dir %q: %w", c.BeadsDir, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf(
				"--beads-dir %q: path does not exist", c.BeadsDir)
		}
		return "", fmt.Errorf("--beads-dir %q: %w", c.BeadsDir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf(
			"--beads-dir %q: not a directory", c.BeadsDir)
	}
	// Accept the rig root (contains .beads/) or the .beads/ dir itself.
	if filepath.Base(abs) == ".beads" {
		return filepath.Dir(abs), nil
	}
	beads := filepath.Join(abs, ".beads")
	binfo, err := os.Stat(beads)
	if err != nil || !binfo.IsDir() {
		return "", fmt.Errorf(
			"--beads-dir %q: no .beads/ directory found at %s\n"+
				"  Pass the rig root (containing .beads/) or the "+
				".beads/ directory itself.", c.BeadsDir, beads)
	}
	return abs, nil
}

// listenDisplay formats Listen+Port the way it would appear on the command
// line, so error messages can echo the user's intent verbatim.
func (c ServeConfig) listenDisplay() string {
	host := c.Listen
	if host == "" {
		host = "127.0.0.1"
	}
	if c.Port == 0 {
		return host
	}
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		// IPv6 literal — bracket it for the host:port form.
		return fmt.Sprintf("[%s]:%d", host, c.Port)
	}
	return fmt.Sprintf("%s:%d", host, c.Port)
}

// isLoopback returns true if the given host (which may be a hostname,
// IPv4 literal, or IPv6 literal) resolves to a loopback address.
//
// We treat "unspecified" addresses (0.0.0.0, ::) as non-loopback since
// that's the practical user concern — "I'm exposing this to the world."
func isLoopback(host string) (bool, error) {
	if host == "" {
		return true, nil // default
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsUnspecified() {
			return false, nil
		}
		return ip.IsLoopback(), nil
	}
	// Hostname — resolve it.
	ips, err := net.LookupIP(host)
	if err != nil {
		return false, err
	}
	// Strict: only loopback if every resolved IP is loopback. If any is
	// non-loopback, treat as non-loopback (principle of least surprise).
	for _, ip := range ips {
		if !ip.IsLoopback() {
			return false, nil
		}
	}
	return true, nil
}
