// Package config: see doc.go for the overview.
package config

import (
	"errors"
	"fmt"
	"net"
)

// ServeConfig captures every flag `gemba serve` accepts. Held on the CLI side
// and passed into the server; keep it a pure data type.
type ServeConfig struct {
	Listen string
	Port   int
	Open   bool

	AuthMode      string
	TLSCert       string
	TLSKey        string
	TLSSelfSigned bool

	// City is the path to a Gas City workspace (preferred).
	// Town is the path to a Gas Town HQ (legacy; kept for back-compat).
	// Exactly one may be set; an empty workspace defers to auto-detect.
	City string
	Town string

	DangerouslySkipPermissions bool
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
// This is the single most important security check. See gm-e3.1 for the
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
		return errors.New(
			"refusing to bind non-loopback interface without authentication\n" +
				"  Pass --auth=token to generate a token, or keep the default bind\n" +
				"  (127.0.0.1). See docs/remote-access.md for details.")
	}
	return nil
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
