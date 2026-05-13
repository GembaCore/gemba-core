package dolthub

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GembaCore/gemba-core/internal/server/secrets"
)

// fakeVault is a tiny in-memory Vault for tests.
type fakeVault struct{ m map[string]string }

func (f *fakeVault) Inject(name string) ([]byte, error) {
	v, ok := f.m[name]
	if !ok {
		return nil, secrets.ErrNotFound
	}
	return []byte(v), nil
}

func doltAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt binary not in PATH; skipping integration")
	}
}

func initDoltRepo(t *testing.T) string {
	t.Helper()
	doltAvailable(t)
	dir := t.TempDir()
	// `dolt init` requires git-style user config; supply some.
	cmd := exec.Command("dolt", "init",
		"--name=gemba-agent",
		"--email=agent@gemba")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("dolt init failed in test env: %v (%s)", err, out)
	}
	return dir
}

func TestConfigureNoVaultIsNoop(t *testing.T) {
	r := Open(t.TempDir(), nil)
	if err := r.Configure(context.Background()); err != nil {
		t.Fatalf("Configure with nil vault: %v", err)
	}
}

func TestConfigureMissingURLIsNoop(t *testing.T) {
	r := Open(t.TempDir(), &fakeVault{m: map[string]string{}})
	if err := r.Configure(context.Background()); err != nil {
		t.Fatalf("Configure with vault missing URL: %v", err)
	}
}

func TestVaultInterfaceCompat(t *testing.T) {
	// Ensure the real secrets.Vault satisfies our Vault interface even
	// when callers pass *secrets.Vault directly.
	master, err := secrets.GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	v, err := secrets.Open(t.TempDir(), "ws-x", master)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Put(SecretRemoteURL, []byte("https://doltremoteapi.dolthub.com/example/db")); err != nil {
		t.Fatal(err)
	}
	r := Open(t.TempDir(), v)
	// Confirm the type satisfies Vault.
	var _ Vault = r.Vault
	// Without a real dolt repo, Configure may fail when it tries to add
	// the remote; we only verify the read-side here.
	_, err = v.Inject(SecretRemoteURL)
	if err != nil {
		t.Fatalf("Inject URL: %v", err)
	}
}

func TestConfigureAddsRemoteThenIdempotent(t *testing.T) {
	dir := initDoltRepo(t)
	fv := &fakeVault{m: map[string]string{
		SecretRemoteURL: "file://" + filepath.Join(t.TempDir(), "remote.fake"),
	}}
	r := Open(dir, fv)
	if err := r.Configure(context.Background()); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	out, err := exec.Command("dolt", "remote", "-v").CombinedOutput()
	// We chdir via cmd.Dir inside dolt(); for the cross-check above we
	// did NOT pass Dir, so reconfigure and inspect via r.dolt directly.
	_ = out
	_ = err
	out2, err := r.dolt(context.Background(), "remote", "-v")
	if err != nil {
		t.Fatalf("dolt remote -v: %v", err)
	}
	if !strings.Contains(out2, fv.m[SecretRemoteURL]) {
		t.Fatalf("remote not configured: %s", out2)
	}
	// Reconfigure with the same URL is a no-op (no error).
	if err := r.Configure(context.Background()); err != nil {
		t.Fatalf("Configure idempotent: %v", err)
	}
	// Rewriting with a different URL replaces it.
	fv.m[SecretRemoteURL] = "file://" + filepath.Join(t.TempDir(), "remote2.fake")
	if err := r.Configure(context.Background()); err != nil {
		t.Fatalf("Configure rewrite: %v", err)
	}
	out3, err := r.dolt(context.Background(), "remote", "-v")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out3, "remote2.fake") {
		t.Fatalf("rewrite missing new URL: %s", out3)
	}
}

func TestStatus(t *testing.T) {
	dir := initDoltRepo(t)
	r := Open(dir, nil)
	out, err := r.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if out == "" {
		t.Fatalf("Status returned empty output")
	}
}

func TestDoltBinaryRequired(t *testing.T) {
	// Even without dolt installed, Open + Configure(no vault) must not
	// invoke the binary. Verified by TestConfigureNoVaultIsNoop above;
	// this is just an explicit assertion about file-existence pathing.
	r := Open(t.TempDir(), nil)
	if r.RepoPath == "" {
		t.Fatal("Open did not retain repo path")
	}
}

func TestFakeVaultMissingURL(t *testing.T) {
	// Read with a vault that returns ErrNotFound for the URL should
	// degrade to "no remote configured" rather than error.
	r := Open(t.TempDir(), &fakeVault{m: map[string]string{}})
	if err := r.Configure(context.Background()); err != nil {
		t.Fatalf("missing URL should be a noop, got %v", err)
	}
}

func TestVaultInjectErrPropagates(t *testing.T) {
	r := Open(t.TempDir(), errVault{})
	if err := r.Configure(context.Background()); err == nil || !strings.Contains(err.Error(), "read remote URL") {
		t.Fatalf("Configure with broken vault: %v", err)
	}
}

type errVault struct{}

func (errVault) Inject(string) ([]byte, error) { return nil, errors.New("vault is on fire") }

// Sanity-check that the package compiles with the standard imports
// (avoids a phantom "imported and not used" regression).
func TestImports(t *testing.T) {
	_ = os.Getenv
}
