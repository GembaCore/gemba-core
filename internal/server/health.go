// Productization probes (gm-o9t8.4 / M4 slice c).
//
// /healthz is a pure liveness probe — it returns 200 as long as the Go
// process can serve HTTP. Orchestrators use this as the kubelet-style
// "is the binary alive" check.
//
// /readyz is the structured readiness probe. It runs every attached
// component check and aggregates the results into:
//
//	{
//	  "ok": false,
//	  "components": {
//	    "kvm":   {"ok": true, "skipped": "non-linux"},
//	    "vault": {"ok": true},
//	    "dolt":  {"ok": false, "error": "embedded supervisor not ready"},
//	    "audit": {"ok": true}
//	  }
//	}
//
// HTTP status is 200 when every component is ok, 503 otherwise. The
// existing /api/readyz handler (readyz.go) is left untouched — it is
// the dolt-only narrow probe that the SPA's degraded banner already
// depends on. This file adds the broader, component-structured probes
// at the root mux.

package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// ComponentResult is the per-component health entry surfaced under the
// "components" key. Skipped is non-empty when a check did not run for a
// structural reason (e.g. KVM on macOS); a skipped check counts as ok.
type ComponentResult struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Skipped string `json:"skipped,omitempty"`
}

// VaultProber is the surface health.go uses to round-trip a sentinel
// through the secrets vault. The real *vault.boltVault satisfies this
// directly; tests inject fakes that simulate Put/Inject failure.
//
// We deliberately do NOT depend on internal/vault.Vault here — that
// interface requires per-workspace ids and would force a workspace
// allocation just to probe readiness. A narrow surface keeps the probe
// cheap and the test seam clean.
type VaultProber interface {
	Probe(ctx context.Context) error
}

// DoltProber is the surface health.go uses to verify the bd/dolt
// adapter is reachable. Implementations should return nil only when
// a SELECT-style round-trip would succeed; any non-nil error fails the
// dolt component. The existing *supervisor.Supervisor already exposes
// Ready() — wire it via DoltProberFunc.
type DoltProber interface {
	Probe(ctx context.Context) error
}

// DoltProberFunc adapts a plain function to DoltProber.
type DoltProberFunc func(ctx context.Context) error

// Probe satisfies DoltProber.
func (f DoltProberFunc) Probe(ctx context.Context) error { return f(ctx) }

// VaultProberFunc adapts a plain function to VaultProber.
type VaultProberFunc func(ctx context.Context) error

// Probe satisfies VaultProber.
func (f VaultProberFunc) Probe(ctx context.Context) error { return f(ctx) }

// healthState is the dependency-injection bag for the /readyz probes.
// All fields are optional; nil components are skipped (and the skip is
// reported in the JSON response, not treated as a silent pass).
type healthState struct {
	mu       sync.RWMutex
	vault    VaultProber
	dolt     DoltProber
	auditDir string
	// kvmPath is the device path the linux KVM check stats. Overridable
	// for tests so they can simulate "no KVM" without root.
	kvmPath string
}

// AttachHealthChecks wires the readiness probes onto the router. Any
// argument may be empty / nil and is reported as skipped or failing
// per the contract above. Calling AttachHealthChecks more than once
// replaces the prior wiring atomically.
func (r *Router) AttachHealthChecks(vault VaultProber, dolt DoltProber, auditDir string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.healthState.vault = vault
	r.dolt = dolt
	r.auditDir = auditDir
	if r.kvmPath == "" {
		r.kvmPath = "/dev/kvm"
	}
}

// setHealthKVMPath is a test seam — production callers never override
// the default /dev/kvm path.
func (r *Router) setHealthKVMPath(p string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.kvmPath = p
}

// healthzPlain is the /healthz handler. It returns 200 OK with
// {ok: true} unconditionally — orchestrators distinguish "process is
// alive" (this) from "dependencies are happy" (/readyz).
func (r *Router) healthzPlain(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// readyzPlain is the structured /readyz handler. It runs each
// component check, aggregates the results, and returns 200 only when
// every component reports ok.
func (r *Router) readyzPlain(w http.ResponseWriter, req *http.Request) {
	ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
	defer cancel()

	r.mu.RLock()
	v := r.healthState.vault
	d := r.dolt
	auditDir := r.auditDir
	kvmPath := r.kvmPath
	r.mu.RUnlock()

	if kvmPath == "" {
		kvmPath = "/dev/kvm"
	}

	components := map[string]ComponentResult{
		"kvm":   probeKVM(kvmPath),
		"vault": probeVault(ctx, v),
		"dolt":  probeDolt(ctx, d),
		"audit": probeAuditDir(auditDir),
	}

	overall := true
	for _, c := range components {
		if !c.OK {
			overall = false
			break
		}
	}

	status := http.StatusOK
	if !overall {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{
		"ok":         overall,
		"components": components,
	})
}

// probeKVM stats the KVM device on Linux and reports a structured
// skip on every other GOOS. On Linux a missing /dev/kvm is a hard
// failure — we don't try to distinguish "kernel without KVM" from
// "no virtualisation extensions" because the operator-visible effect
// is the same: no Firecracker.
func probeKVM(path string) ComponentResult {
	if runtime.GOOS != "linux" {
		return ComponentResult{OK: true, Skipped: "non-linux"}
	}
	if _, err := os.Stat(path); err != nil {
		return ComponentResult{OK: false, Error: err.Error()}
	}
	return ComponentResult{OK: true}
}

// probeVault round-trips a fresh sentinel through the prober and
// reports the result. A nil prober is reported as a skip — readiness
// degrades to true with a "skipped" annotation rather than silently
// passing, so operators can distinguish "no vault attached" from
// "vault is working".
func probeVault(ctx context.Context, v VaultProber) ComponentResult {
	if v == nil {
		return ComponentResult{OK: true, Skipped: "not-attached"}
	}
	if err := v.Probe(ctx); err != nil {
		return ComponentResult{OK: false, Error: err.Error()}
	}
	return ComponentResult{OK: true}
}

// probeDolt invokes the dolt prober. Same skip semantics as
// probeVault — a nil prober is "skipped" rather than a silent pass.
func probeDolt(ctx context.Context, d DoltProber) ComponentResult {
	if d == nil {
		return ComponentResult{OK: true, Skipped: "not-attached"}
	}
	if err := d.Probe(ctx); err != nil {
		return ComponentResult{OK: false, Error: err.Error()}
	}
	return ComponentResult{OK: true}
}

// probeAuditDir verifies the audit directory is writable by creating
// and immediately removing a sentinel file. An empty path is reported
// as skipped — operators who run gemba without audit logging see the
// skip in the response and know it isn't a misconfiguration.
func probeAuditDir(dir string) ComponentResult {
	if dir == "" {
		return ComponentResult{OK: true, Skipped: "not-configured"}
	}
	info, err := os.Stat(dir)
	if err != nil {
		return ComponentResult{OK: false, Error: err.Error()}
	}
	if !info.IsDir() {
		return ComponentResult{OK: false, Error: "audit path is not a directory"}
	}
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		return ComponentResult{OK: false, Error: err.Error()}
	}
	sentinel := filepath.Join(dir, ".healthz-"+hex.EncodeToString(suffix))
	f, err := os.OpenFile(sentinel, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o600)
	if err != nil {
		return ComponentResult{OK: false, Error: err.Error()}
	}
	if _, err := f.Write([]byte("ok")); err != nil {
		_ = f.Close()
		_ = os.Remove(sentinel)
		return ComponentResult{OK: false, Error: err.Error()}
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(sentinel)
		return ComponentResult{OK: false, Error: err.Error()}
	}
	if err := os.Remove(sentinel); err != nil {
		return ComponentResult{OK: false, Error: err.Error()}
	}
	return ComponentResult{OK: true}
}

// ErrProbeUnconfigured is the sentinel a prober can return to signal
// "I am attached but my upstream is intentionally not wired" — the
// readyz handler treats this as a failure (the operator wired the
// prober, so they expect it to work). It exists so callers don't
// have to invent string error messages for the same condition.
var ErrProbeUnconfigured = errors.New("health: prober upstream not configured")
