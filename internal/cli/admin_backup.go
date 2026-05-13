// Package cli — admin backup/restore (gm-o9t8.4 / M4 slice c).
//
// Snapshots a gemba data directory into a single tar.gz containing:
//
//   - dolt/    — verbatim copy of the dolt data dir (an "everything"
//                snapshot per database). If a `dolt` binary is on PATH
//                and the data dir contains dolt databases, we also drop
//                a SQL dump per database alongside the file copy so
//                operators can replay the schema on a different engine.
//   - workspaces/  — verbatim copy of the workspaces dir, which
//                includes every workspace's secrets blob (vault.db
//                + per-workspace secrets.enc files).
//   - audit.jsonl — the audit log, if configured.
//   - manifest.json — timestamp, server version, per-component sha256
//                checksums.
//
// Restore reverses the operation, refusing to overwrite a non-empty
// data dir unless --force is supplied. The restore round-trip preserves
// every byte under the data dir, so vault.Inject and audit.Verify both
// succeed against the restored state — that's what
// admin_backup_test.go asserts.

package cli

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// BackupManifest is the JSON document embedded at the root of every
// backup tarball. The Checksums map is keyed by tar entry path and
// holds sha256 hex; restorers verify each entry against this map.
type BackupManifest struct {
	CreatedAt     time.Time         `json:"created_at"`
	ServerVersion string            `json:"server_version"`
	Checksums     map[string]string `json:"checksums"`
}

const (
	manifestEntry  = "manifest.json"
	doltPrefix     = "dolt/"
	wsPrefix       = "workspaces/"
	auditEntry     = "audit.jsonl"
	doltDumpPrefix = "dolt-dumps/"
)

func newAdminCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Administrative commands (backup, restore)",
	}
	cmd.AddCommand(newAdminBackupCmd())
	cmd.AddCommand(newAdminRestoreCmd())
	return cmd
}

type backupFlags struct {
	out         string
	dataDir     string
	auditLog    string
	version     string
	skipDoltDump bool
}

func newAdminBackupCmd() *cobra.Command {
	var flags backupFlags
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Snapshot gemba state into a single tar.gz",
		Long: `Snapshot a gemba data directory into a single tar.gz containing the
dolt data dir, per-workspace vault blobs, the audit log, and a
manifest.json with per-component sha256 checksums.

The output file is created with 0600. Restore on a fresh data-dir
with 'gemba admin restore --from <path>'.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if flags.out == "" {
				return errors.New("--out is required")
			}
			if flags.dataDir == "" {
				return errors.New("--data-dir is required")
			}
			return runAdminBackup(cmd.OutOrStdout(), flags)
		},
	}
	cmd.Flags().StringVar(&flags.out, "out", "", "output tar.gz path (required)")
	cmd.Flags().StringVar(&flags.dataDir, "data-dir", "", "gemba data directory (required); typically the parent of dolt/ and workspaces/")
	cmd.Flags().StringVar(&flags.auditLog, "audit-log", "", "audit log file path (optional)")
	cmd.Flags().StringVar(&flags.version, "server-version", "dev", "value stamped into manifest.json")
	cmd.Flags().BoolVar(&flags.skipDoltDump, "skip-dolt-dump", false, "do not shell out to `dolt dump` even if a dolt binary is present")
	return cmd
}

type restoreFlags struct {
	from    string
	dataDir string
	auditLog string
	force   bool
}

func newAdminRestoreCmd() *cobra.Command {
	var flags restoreFlags
	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Restore a gemba data directory from a backup tar.gz",
		Long: `Restore a gemba data directory from a tar.gz previously produced by
'gemba admin backup'.

Refuses to overwrite a non-empty target directory unless --force is
supplied. --force is the only auto-mode-friendly use of force in
gemba; it exists because restore is a recovery flow and the operator
has already accepted that the target will be replaced.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if flags.from == "" {
				return errors.New("--from is required")
			}
			if flags.dataDir == "" {
				return errors.New("--data-dir is required")
			}
			return runAdminRestore(cmd.OutOrStdout(), flags)
		},
	}
	cmd.Flags().StringVar(&flags.from, "from", "", "backup tar.gz path (required)")
	cmd.Flags().StringVar(&flags.dataDir, "data-dir", "", "target gemba data directory (required)")
	cmd.Flags().StringVar(&flags.auditLog, "audit-log", "", "target audit log file path (optional; defaults to <data-dir>/audit.jsonl)")
	cmd.Flags().BoolVar(&flags.force, "force", false, "overwrite a non-empty target data directory")
	return cmd
}

// runAdminBackup writes the snapshot tarball at flags.out and prints a
// summary to stdout. The implementation streams: every entry is hashed
// as it's written to the tar, so peak memory is O(buffer) rather than
// O(file).
func runAdminBackup(out io.Writer, flags backupFlags) error {
	if err := os.MkdirAll(filepath.Dir(flags.out), 0o700); err != nil {
		return fmt.Errorf("mkdir output dir: %w", err)
	}
	f, err := os.OpenFile(flags.out, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open output: %w", err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	manifest := BackupManifest{
		CreatedAt:     time.Now().UTC(),
		ServerVersion: flags.version,
		Checksums:     map[string]string{},
	}

	doltDir := filepath.Join(flags.dataDir, "dolt")
	if _, err := os.Stat(doltDir); err == nil {
		if err := tarTree(tw, doltDir, doltPrefix, manifest.Checksums); err != nil {
			return fmt.Errorf("backup dolt: %w", err)
		}
		if !flags.skipDoltDump {
			if err := maybeDumpDolt(tw, doltDir, manifest.Checksums, out); err != nil {
				// Non-fatal: the file-level snapshot is still authoritative.
				fmt.Fprintf(out, "warning: dolt dump skipped: %v\n", err)
			}
		}
	}
	wsDir := filepath.Join(flags.dataDir, "workspaces")
	if _, err := os.Stat(wsDir); err == nil {
		if err := tarTree(tw, wsDir, wsPrefix, manifest.Checksums); err != nil {
			return fmt.Errorf("backup workspaces: %w", err)
		}
	}
	// Also catch a top-level vault.db (the standard layout per
	// internal/config/bind.go).
	vaultDB := filepath.Join(flags.dataDir, "vault.db")
	if info, err := os.Stat(vaultDB); err == nil && !info.IsDir() {
		if err := tarFile(tw, vaultDB, "vault.db", manifest.Checksums); err != nil {
			return fmt.Errorf("backup vault.db: %w", err)
		}
	}
	if flags.auditLog != "" {
		if info, err := os.Stat(flags.auditLog); err == nil && !info.IsDir() {
			if err := tarFile(tw, flags.auditLog, auditEntry, manifest.Checksums); err != nil {
				return fmt.Errorf("backup audit log: %w", err)
			}
		}
	}

	// Manifest goes last so its checksum map covers every other entry.
	mbytes, err := json.MarshalIndent(&manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	if err := writeTarBytes(tw, manifestEntry, mbytes); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	if err := tw.Close(); err != nil {
		return fmt.Errorf("close tar: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("close gzip: %w", err)
	}
	fmt.Fprintf(out, "backup written: %s (%d entries)\n", flags.out, len(manifest.Checksums))
	return nil
}

// runAdminRestore extracts a backup tarball into flags.dataDir. Refuses
// to overwrite a non-empty data dir unless flags.force is set.
func runAdminRestore(out io.Writer, flags restoreFlags) error {
	if err := assertEmptyOrForce(flags.dataDir, flags.force); err != nil {
		return err
	}
	if err := os.MkdirAll(flags.dataDir, 0o700); err != nil {
		return fmt.Errorf("mkdir data dir: %w", err)
	}
	f, err := os.Open(flags.from)
	if err != nil {
		return fmt.Errorf("open backup: %w", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	auditOut := flags.auditLog
	if auditOut == "" {
		auditOut = filepath.Join(flags.dataDir, auditEntry)
	}

	checksums := map[string]string{}
	var manifest *BackupManifest

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar read: %w", err)
		}
		clean := filepath.Clean(hdr.Name)
		if strings.Contains(clean, "..") {
			return fmt.Errorf("tar entry escapes archive: %q", hdr.Name)
		}
		switch {
		case clean == manifestEntry:
			b, err := io.ReadAll(tr)
			if err != nil {
				return fmt.Errorf("read manifest: %w", err)
			}
			var m BackupManifest
			if err := json.Unmarshal(b, &m); err != nil {
				return fmt.Errorf("decode manifest: %w", err)
			}
			manifest = &m
		case clean == auditEntry:
			sum, err := writeFromTar(tr, auditOut, hdr.Mode)
			if err != nil {
				return fmt.Errorf("restore audit log: %w", err)
			}
			checksums[clean] = sum
		case strings.HasPrefix(clean, doltDumpPrefix):
			// Informational SQL dumps — restored alongside the
			// file-level dolt copy for operator inspection.
			dest := filepath.Join(flags.dataDir, clean)
			sum, err := writeFromTar(tr, dest, hdr.Mode)
			if err != nil {
				return fmt.Errorf("restore %s: %w", clean, err)
			}
			checksums[clean] = sum
		case strings.HasPrefix(clean, doltPrefix), strings.HasPrefix(clean, wsPrefix), clean == "vault.db":
			dest := filepath.Join(flags.dataDir, clean)
			if hdr.Typeflag == tar.TypeDir {
				if err := os.MkdirAll(dest, os.FileMode(hdr.Mode)&0o777); err != nil {
					return fmt.Errorf("mkdir %s: %w", dest, err)
				}
				continue
			}
			sum, err := writeFromTar(tr, dest, hdr.Mode)
			if err != nil {
				return fmt.Errorf("restore %s: %w", clean, err)
			}
			checksums[clean] = sum
		default:
			// Unknown entries are skipped rather than fatal so future
			// backup-format additions remain restorable by older
			// binaries. The manifest's checksum mismatch will surface
			// the missing entry to the operator.
			fmt.Fprintf(out, "skipping unknown entry: %s\n", clean)
		}
	}

	if manifest == nil {
		return errors.New("backup is missing manifest.json")
	}
	// Verify every checksum the manifest claimed. A mismatch is a
	// hard error — partial restores are dangerous.
	for name, want := range manifest.Checksums {
		got, ok := checksums[name]
		if !ok {
			return fmt.Errorf("manifest references missing entry %q", name)
		}
		if got != want {
			return fmt.Errorf("checksum mismatch for %q: got %s want %s", name, got, want)
		}
	}
	fmt.Fprintf(out, "restored %d entries to %s\n", len(manifest.Checksums), flags.dataDir)
	return nil
}

// assertEmptyOrForce checks whether dir is empty (or absent). When dir
// exists and contains entries, force must be true.
func assertEmptyOrForce(dir string, force bool) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read target dir: %w", err)
	}
	if len(entries) == 0 {
		return nil
	}
	if !force {
		return fmt.Errorf("target data dir %s is not empty; pass --force to overwrite", dir)
	}
	return nil
}

// tarTree walks root and appends every file under it to tw, prefixing
// each entry name with prefix. Each file's sha256 is recorded in sums.
func tarTree(tw *tar.Writer, root, prefix string, sums map[string]string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := prefix + filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if info.IsDir() {
			hdr := &tar.Header{
				Name:     name + "/",
				Mode:     int64(info.Mode().Perm()),
				Typeflag: tar.TypeDir,
				ModTime:  info.ModTime(),
			}
			return tw.WriteHeader(hdr)
		}
		if !info.Mode().IsRegular() {
			// Skip symlinks / sockets / devices — gemba data dirs
			// never contain these on the supported layouts.
			return nil
		}
		return tarFile(tw, path, name, sums)
	})
}

// tarFile appends a single file at src to tw under entry-name and
// records its sha256.
func tarFile(tw *tar.Writer, src, name string, sums map[string]string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	hdr := &tar.Header{
		Name:     name,
		Mode:     int64(info.Mode().Perm()),
		Size:     info.Size(),
		Typeflag: tar.TypeReg,
		ModTime:  info.ModTime(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tw, h), f); err != nil {
		return err
	}
	sums[name] = hex.EncodeToString(h.Sum(nil))
	return nil
}

// writeTarBytes writes b as a single tar entry under name with mode 0o600.
func writeTarBytes(tw *tar.Writer, name string, b []byte) error {
	hdr := &tar.Header{
		Name:     name,
		Mode:     0o600,
		Size:     int64(len(b)),
		Typeflag: tar.TypeReg,
		ModTime:  time.Now().UTC(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := tw.Write(b)
	return err
}

// writeFromTar extracts the current tar entry into dest. Returns the
// sha256 of the extracted bytes so the caller can verify against the
// manifest.
func writeFromTar(tr *tar.Reader, dest string, mode int64) (string, error) {
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return "", err
	}
	perm := os.FileMode(mode) & 0o777
	if perm == 0 {
		perm = 0o600
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), tr); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// maybeDumpDolt shells out to `dolt dump` per database directory and
// embeds each SQL dump under dolt-dumps/<db>.sql. Best-effort: when
// the dolt binary is missing or the dump fails for any database we
// log and continue; the file-level snapshot above is authoritative.
func maybeDumpDolt(tw *tar.Writer, doltDataDir string, sums map[string]string, out io.Writer) error {
	doltBin, err := exec.LookPath("dolt")
	if err != nil {
		return fmt.Errorf("dolt not on PATH: %w", err)
	}
	entries, err := os.ReadDir(doltDataDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// A "database" in dolt's data dir is a subdirectory that
		// contains a .dolt subdir. Skip anything else.
		dbDir := filepath.Join(doltDataDir, e.Name())
		if _, err := os.Stat(filepath.Join(dbDir, ".dolt")); err != nil {
			continue
		}
		cmd := exec.Command(doltBin, "dump", "--result-format", "sql", "-f", "--directory", dbDir)
		buf, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Fprintf(out, "warning: dolt dump %s failed: %v\n", e.Name(), err)
			continue
		}
		if err := writeTarBytes(tw, doltDumpPrefix+e.Name()+".sql", buf); err != nil {
			return err
		}
		h := sha256.Sum256(buf)
		sums[doltDumpPrefix+e.Name()+".sql"] = hex.EncodeToString(h[:])
	}
	return nil
}
