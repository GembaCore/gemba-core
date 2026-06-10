// gm-o9t8.3.7 — wire the workspace secrets vault into `gemba serve`.
//
// Path resolution: --vault-path > <dirname(--dolt-data-dir)>/vault.db.
// KEK resolution: GEMBA_VAULT_KEY env (hex, 64 chars) > random
// ephemeral key with a WARN. The ephemeral path is the documented
// dev-loop default; production deployments are expected to set
// GEMBA_VAULT_KEY in the environment so secrets persist across
// restarts.

package cli

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/GembaCore/gemba-core/internal/config"
	"github.com/GembaCore/gemba-core/internal/vault"
)

// resolveVaultPath returns the on-disk path the vault file lives at.
// Empty cfg.VaultPath falls back to <dirname(DoltDataDir)>/vault.db,
// or "<cwd>/data/vault.db" when DoltDataDir is itself empty.
func resolveVaultPath(cfg *config.ServeConfig) string {
	if cfg.VaultPath != "" {
		return cfg.VaultPath
	}
	if cfg.DoltDataDir != "" {
		return filepath.Join(filepath.Dir(cfg.DoltDataDir), "vault.db")
	}
	return filepath.Join("data", "vault.db")
}

// buildVault constructs the vault for runServe. When GEMBA_VAULT_KEY
// is set, the KEK is read from there; otherwise an ephemeral key is
// generated and a WARN is logged so the operator knows secrets won't
// survive a restart.
func buildVault(cfg *config.ServeConfig) (vault.Vault, error) {
	path := resolveVaultPath(cfg)
	var kek []byte
	if raw := os.Getenv("GEMBA_VAULT_KEY"); raw != "" {
		decoded, err := hex.DecodeString(raw)
		if err != nil {
			return nil, fmt.Errorf("GEMBA_VAULT_KEY hex decode: %w", err)
		}
		if len(decoded) != 32 {
			return nil, fmt.Errorf("GEMBA_VAULT_KEY must decode to 32 bytes, got %d", len(decoded))
		}
		kek = decoded
	} else {
		slog.Warn("GEMBA_VAULT_KEY not set; using ephemeral key — secrets will not persist across restarts")
		kek = make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, kek); err != nil {
			return nil, fmt.Errorf("ephemeral KEK seed: %w", err)
		}
	}
	return vault.New(vault.Options{Path: path, KEK: kek})
}
