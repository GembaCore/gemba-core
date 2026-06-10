// Package secrets provides a per-workspace encrypted-at-rest secret
// vault for gemba-server. Secrets are AES-256-GCM-encrypted files under
// data/workspaces/<id>/secrets.enc, encrypted with a per-workspace key
// derived from a server-wide master key via HKDF-SHA256 with the
// workspace ID as info.
//
// Secrets are injected into agent processes at dispatch time and are
// never written unencrypted to disk, never logged, and never returned
// from the API. The vault enforces this asymmetry by exposing Inject
// (returns a copy the caller must clear after use) rather than a Read
// that returns the raw value.
//
// Master key: 32 random bytes (base64-encoded) supplied via the
// GEMBA_VAULT_KEY environment variable. A development-only generator is
// shipped as `secrets.GenerateMasterKey()` for tests.
//
// Tracks gemba-remote bead gm-o9t8.3.3 (M3 — proprietary control plane).
package secrets
