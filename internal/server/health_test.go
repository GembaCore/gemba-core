package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/GembaCore/gemba-core/internal/config"
)

// newHealthRouter builds a router with empty SPA + nil host, the same
// minimal wiring readyz_test.go uses. Tests then attach probes per-case.
func newHealthRouter(t *testing.T) *Router {
	t.Helper()
	return NewRouter(config.ServeConfig{}, emptyFS{}, nil)
}

// TestHealthz_AlwaysOK covers the liveness contract: /healthz is 200
// regardless of probe wiring or transient component failures.
func TestHealthz_AlwaysOK(t *testing.T) {
	r := newHealthRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["ok"] != true {
		t.Fatalf("ok=%v, want true", body["ok"])
	}
}

// TestReadyz_HappyPath wires all probes with passing implementations
// and asserts 200 + ok:true + every component reports ok.
func TestReadyz_HappyPath(t *testing.T) {
	r := newHealthRouter(t)
	auditDir := t.TempDir()
	r.AttachHealthChecks(
		VaultProberFunc(func(context.Context) error { return nil }),
		DoltProberFunc(func(context.Context) error { return nil }),
		auditDir,
	)
	// Force the KVM check into the "skipped: non-linux" branch on Linux
	// hosts too — the production path stats /dev/kvm, which is absent
	// on most CI runners. Tests cover the missing-device case
	// separately.
	if runtime.GOOS == "linux" {
		r.setHealthKVMPath(filepath.Join(t.TempDir(), "kvm-present"))
		// create a sentinel so Stat succeeds
		f, err := os.Create(filepath.Join(t.TempDir(), "ignored"))
		if err == nil {
			_ = f.Close()
		}
		// Use a real file as KVM sentinel.
		kvm := filepath.Join(auditDir, "kvm-sentinel")
		fp, err := os.Create(kvm)
		if err != nil {
			t.Fatalf("create kvm sentinel: %v", err)
		}
		_ = fp.Close()
		r.setHealthKVMPath(kvm)
	}

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var body struct {
		OK         bool                       `json:"ok"`
		Components map[string]ComponentResult `json:"components"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.OK {
		t.Fatalf("overall ok=false, components=%+v", body.Components)
	}
	for name, c := range body.Components {
		if !c.OK {
			t.Fatalf("component %s not ok: %+v", name, c)
		}
	}
	if runtime.GOOS != "linux" && body.Components["kvm"].Skipped != "non-linux" {
		t.Fatalf("kvm skipped=%q, want non-linux", body.Components["kvm"].Skipped)
	}
}

// TestReadyz_VaultFails covers the vault failure path: when the vault
// prober returns an error, /readyz returns 503 and the vault component
// reports the error verbatim.
func TestReadyz_VaultFails(t *testing.T) {
	r := newHealthRouter(t)
	r.AttachHealthChecks(
		VaultProberFunc(func(context.Context) error { return errors.New("vault: tampered") }),
		DoltProberFunc(func(context.Context) error { return nil }),
		t.TempDir(),
	)
	resp := callReadyz(t, r)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503", resp.Code)
	}
	body := decodeReadyz(t, resp)
	if body.OK {
		t.Fatalf("overall ok=true; want false")
	}
	if body.Components["vault"].OK {
		t.Fatalf("vault component reports ok; want failure")
	}
	if body.Components["vault"].Error == "" {
		t.Fatalf("vault component error empty; want a message")
	}
}

// TestReadyz_DoltFails covers the dolt failure path.
func TestReadyz_DoltFails(t *testing.T) {
	r := newHealthRouter(t)
	r.AttachHealthChecks(
		VaultProberFunc(func(context.Context) error { return nil }),
		DoltProberFunc(func(context.Context) error { return errors.New("dolt: connection refused") }),
		t.TempDir(),
	)
	resp := callReadyz(t, r)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503", resp.Code)
	}
	body := decodeReadyz(t, resp)
	if body.Components["dolt"].OK {
		t.Fatalf("dolt component reports ok; want failure")
	}
}

// TestReadyz_AuditDirFails covers the audit-writability failure path:
// a path that exists but is not a directory is rejected.
func TestReadyz_AuditDirFails(t *testing.T) {
	r := newHealthRouter(t)
	// Point audit dir at a file rather than a directory — the probe
	// must reject this as a misconfiguration.
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "not-a-dir"))
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	_ = f.Close()
	r.AttachHealthChecks(
		VaultProberFunc(func(context.Context) error { return nil }),
		DoltProberFunc(func(context.Context) error { return nil }),
		filepath.Join(dir, "not-a-dir"),
	)
	resp := callReadyz(t, r)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503", resp.Code)
	}
	body := decodeReadyz(t, resp)
	if body.Components["audit"].OK {
		t.Fatalf("audit component reports ok; want failure")
	}
}

// TestReadyz_KVMMissing_OnLinux covers the KVM failure path on Linux
// only (on macOS / Windows the KVM check structurally skips, so a
// failure case doesn't exist there). The check overrides kvmPath to
// point at a path that doesn't exist.
func TestReadyz_KVMMissing_OnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("KVM check is structurally skipped off Linux")
	}
	r := newHealthRouter(t)
	r.AttachHealthChecks(
		VaultProberFunc(func(context.Context) error { return nil }),
		DoltProberFunc(func(context.Context) error { return nil }),
		t.TempDir(),
	)
	r.setHealthKVMPath(filepath.Join(t.TempDir(), "definitely-not-here"))

	resp := callReadyz(t, r)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503", resp.Code)
	}
	body := decodeReadyz(t, resp)
	if body.Components["kvm"].OK {
		t.Fatalf("kvm component reports ok with missing device")
	}
}

// TestReadyz_Unattached covers the no-attach path: a freshly built
// router with no AttachHealthChecks call still answers /readyz. Each
// component reports a structured skip; the overall result is ok.
func TestReadyz_Unattached(t *testing.T) {
	r := newHealthRouter(t)
	resp := callReadyz(t, r)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", resp.Code)
	}
	body := decodeReadyz(t, resp)
	if !body.OK {
		t.Fatalf("overall ok=false; want true (unattached should be ok)")
	}
	// vault + dolt + audit should all be skipped; kvm is environment-
	// dependent so we only assert it's ok.
	for _, name := range []string{"vault", "dolt", "audit"} {
		if body.Components[name].Skipped == "" {
			t.Fatalf("component %s expected to be skipped; got %+v", name, body.Components[name])
		}
	}
}

// --- test helpers ---------------------------------------------------------

type readyzBody struct {
	OK         bool                       `json:"ok"`
	Components map[string]ComponentResult `json:"components"`
}

func callReadyz(t *testing.T, r *Router) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeReadyz(t *testing.T, w *httptest.ResponseRecorder) readyzBody {
	t.Helper()
	var body readyzBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v; raw=%s", err, w.Body.String())
	}
	return body
}
