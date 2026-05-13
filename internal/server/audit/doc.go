// Package audit is an append-only, hash-chained, signed audit log for
// agent actions on gemba-server. Each record commits to its predecessor
// via prev_hash, so tampering is detectable end-to-end without storing
// every signature externally.
//
// File format: one JSON record per line, newline-delimited (JSONL).
// Signing key: per-server Ed25519 keypair. The private key is supplied
// via the GEMBA_AUDIT_KEY environment variable (base64-encoded seed) or
// loaded from a configured key file. Use GenerateKey() to mint one.
//
// Tracks gemba-remote bead gm-o9t8.3.4 (M3 — append-only signed audit
// log).
package audit
