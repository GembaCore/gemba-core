package firecracker

import (
	"context"
	"errors"
	"os"
	"runtime"
	"testing"
	"time"
)

// fakeVault is a tiny VaultInjector for tests.
type fakeVault struct {
	secrets map[string]map[string]string // wsid -> secrets
	err     error
	calls   int
}

func (f *fakeVault) Inject(_ context.Context, wsid string) (map[string]string, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[string]string)
	for k, v := range f.secrets[wsid] {
		out[k] = v
	}
	return out, nil
}

// TestFallback_VaultSecretsReachSubprocess covers gm-o9t8.3.2.5 — when
// a vault is wired, its secrets land in the subprocess env.
func TestFallback_VaultSecretsReachSubprocess(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("fallback-only test")
	}
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	v := &fakeVault{secrets: map[string]map[string]string{
		"vault-ws": {"GEMBA_VAULT_TOKEN": "from-vault"},
	}}
	sup := NewSupervisorWithOptions(Options{Vault: v})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	spec := Spec{
		WorkspaceID:     "vault-ws",
		FallbackCommand: `test "$GEMBA_VAULT_TOKEN" = "from-vault"`,
	}
	vm, err := sup.Start(ctx, spec)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case waitErr, ok := <-sup.Wait(ctx, vm):
		if ok && waitErr != nil {
			t.Fatalf("subprocess did not see vault secret: %v", waitErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("Wait did not fire")
	}
	if v.calls != 1 {
		t.Fatalf("expected 1 vault.Inject call, got %d", v.calls)
	}
	_ = sup.Stop(ctx, vm, time.Second)
}

// TestFallback_ExplicitSecretsOverrideVault covers the merge rule —
// Spec.Secrets shadow vault values with the same key.
func TestFallback_ExplicitSecretsOverrideVault(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("fallback-only test")
	}
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	v := &fakeVault{secrets: map[string]map[string]string{
		"ws": {"SHARED": "from-vault"},
	}}
	sup := NewSupervisorWithOptions(Options{Vault: v})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	spec := Spec{
		WorkspaceID:     "ws",
		Secrets:         map[string]string{"SHARED": "explicit-wins"},
		FallbackCommand: `test "$SHARED" = "explicit-wins"`,
	}
	vm, err := sup.Start(ctx, spec)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case waitErr, ok := <-sup.Wait(ctx, vm):
		if ok && waitErr != nil {
			t.Fatalf("explicit secret did not override vault: %v", waitErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("Wait did not fire")
	}
	_ = sup.Stop(ctx, vm, time.Second)
}

// TestFallback_NilVaultUsesOnlyExplicit asserts nil vault is a no-op
// path (no panic) and explicit Spec.Secrets still propagate.
func TestFallback_NilVaultUsesOnlyExplicit(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("fallback-only test")
	}
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	sup := NewSupervisorWithOptions(Options{}) // nil vault
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	spec := Spec{
		WorkspaceID:     "ws",
		Secrets:         map[string]string{"ONLY": "explicit"},
		FallbackCommand: `test "$ONLY" = "explicit"`,
	}
	vm, err := sup.Start(ctx, spec)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case waitErr, ok := <-sup.Wait(ctx, vm):
		if ok && waitErr != nil {
			t.Fatalf("explicit secret did not propagate without vault: %v", waitErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("Wait did not fire")
	}
	_ = sup.Stop(ctx, vm, time.Second)
}

// TestFallback_VaultErrorBubblesUp covers the failure path — Inject
// errors abort the Start.
func TestFallback_VaultErrorBubblesUp(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("fallback-only test")
	}
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	v := &fakeVault{err: errors.New("vault: kaboom")}
	sup := NewSupervisorWithOptions(Options{Vault: v})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := sup.Start(ctx, Spec{WorkspaceID: "ws", FallbackCommand: "exec cat"})
	if err == nil {
		t.Fatalf("expected vault error, got nil")
	}
}
