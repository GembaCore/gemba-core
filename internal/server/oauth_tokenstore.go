// gm-o9t8.3.5.2/3.5.5 — auth_tokens persistence (with revoke + rotate).
//
// The OAuth login handler mints a 32-byte gemba bearer, hashes it with
// argon2id, and stores the (tenant_id, github_id, device_label, hash)
// tuple in the auth_tokens table. The plaintext is never persisted —
// it's returned to the CLI exactly once and never recoverable from
// disk.
//
// Two stores are provided:
//
//   - MemTokenStore — in-memory; used by tests and the no-Dolt fallback
//   - SQLTokenStore — Dolt/MySQL backed; cmd/gemba serve attaches one
//     against the bd Dolt pool when --dolt-url is configured.
//
// Verification is O(n) over the stored hashes because argon2id is
// per-record. n is small for v1 (one row per (tenant, device)); when
// it grows past the comfortable scan threshold we add a lookup index
// or move bearer verification to a stateless JWT.
//
// gm-o9t8.3.5.5 adds soft-delete (revoked_at), last_used_at touch,
// and an opaque per-token id so the management surface (list, revoke,
// rotate) can address rows without leaking either the plaintext or
// the argon2id hash.
package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/GembaCore/gemba-core/internal/auth"
	"github.com/GembaCore/gemba-core/internal/tenant"
)

// CreateAuthTokensTable is the DDL the tenant package's Migrate would
// run on a multi-tenant rig. Idempotent. Mirrors the tenants table
// migration convention.
//
// gm-o9t8.3.5.5 changes:
//   - id is the primary key (a 32-char opaque token id minted server
//     side). The composite (tenant_id, device_label) becomes a
//     secondary UNIQUE so re-login from the same device still upserts.
//   - revoked_at: soft-delete sentinel; VerifyToken skips non-null rows.
//   - last_used_at: best-effort touch on successful verify.
const CreateAuthTokensTable = `CREATE TABLE IF NOT EXISTS auth_tokens (
	id            VARCHAR(32)  NOT NULL,
	tenant_id     VARCHAR(16)  NOT NULL,
	github_id     BIGINT       NOT NULL,
	device_label  VARCHAR(64)  NOT NULL,
	hash_phc      TEXT         NOT NULL,
	created_at    TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
	last_used_at  TIMESTAMP    NULL,
	revoked_at    TIMESTAMP    NULL,
	PRIMARY KEY (id),
	UNIQUE KEY ux_auth_tokens_tenant_device (tenant_id, device_label),
	KEY idx_auth_tokens_github_id (github_id)
)`

// TokenInfo is the metadata view of a stored token, returned by
// List + Rotate. Carries no plaintext or hash material; safe to return
// over the wire.
type TokenInfo struct {
	ID          string     `json:"id"`
	DeviceLabel string     `json:"device_label"`
	CreatedAt   time.Time  `json:"created_at"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
}

// ErrTokenNotFound is returned by Revoke/Rotate when the (tenant,
// token_id) tuple is not present in the store (or already revoked).
// HTTP handlers translate this to 404.
var ErrTokenNotFound = errors.New("oauth: token not found")

// NewTokenID returns a 32-char hex id used as the auth_tokens primary
// key. Distinct from the plaintext bearer; safe to expose.
func NewTokenID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("oauth: token id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// MemTokenStore is the in-memory TokenStore — process-local, lost on
// restart. Tests and single-user fallback use this; production
// multi-tenant rigs attach SQLTokenStore.
type MemTokenStore struct {
	mu      sync.RWMutex
	records []TokenRecord
}

// NewMemTokenStore returns a zero-valued in-memory token store.
func NewMemTokenStore() *MemTokenStore { return &MemTokenStore{} }

// Put implements TokenStore. Upserts on (tenant_id, device_label) so a
// re-login from the same device replaces the prior hash rather than
// stacking dead rows. If the incoming record has no ID, one is minted.
func (m *MemTokenStore) Put(_ context.Context, rec TokenRecord) error {
	if rec.TenantID == "" || rec.DeviceLabel == "" || rec.HashPHC == "" {
		return errors.New("oauth: TokenRecord requires TenantID, DeviceLabel, HashPHC")
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	if rec.ID == "" {
		id, err := NewTokenID()
		if err != nil {
			return err
		}
		rec.ID = id
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.records {
		if r.TenantID == rec.TenantID && r.DeviceLabel == rec.DeviceLabel {
			// Preserve the prior ID on upsert so rotate-after-relogin
			// addresses the same row.
			if rec.ID == "" {
				rec.ID = r.ID
			}
			m.records[i] = rec
			return nil
		}
	}
	m.records = append(m.records, rec)
	return nil
}

// VerifyToken implements TokenStore. Iterates the stored records and
// runs argon2id Verify against each hash. Returns the matching
// tenant id on success, or ("", false, nil) on miss. Skips rows whose
// RevokedAt is non-nil. Best-effort touches LastUsedAt on success.
func (m *MemTokenStore) VerifyToken(_ context.Context, plaintext string) (tenant.ID, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.records {
		if r.RevokedAt != nil {
			continue
		}
		ok, err := auth.VerifyHash(plaintext, r.HashPHC)
		if err != nil {
			// A malformed stored hash is an infra bug — surface it so
			// the operator notices. Don't continue scanning: a single
			// bad row should not silently accept a guess against a
			// later valid one.
			return "", false, fmt.Errorf("oauth: verify hash: %w", err)
		}
		if ok {
			now := time.Now().UTC()
			m.records[i].LastUsedAt = &now
			return r.TenantID, true, nil
		}
	}
	return "", false, nil
}

// List returns metadata for non-revoked tokens belonging to tenantID.
func (m *MemTokenStore) List(_ context.Context, tenantID string) ([]TokenInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []TokenInfo{}
	for _, r := range m.records {
		if string(r.TenantID) != tenantID {
			continue
		}
		if r.RevokedAt != nil {
			continue
		}
		out = append(out, TokenInfo{
			ID:          r.ID,
			DeviceLabel: r.DeviceLabel,
			CreatedAt:   r.CreatedAt,
			LastUsedAt:  r.LastUsedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

// Revoke soft-deletes the record identified by (tenantID, tokenID). The
// tenant scope is enforced here so a cross-tenant request hits
// ErrTokenNotFound rather than silently revoking a sibling row.
func (m *MemTokenStore) Revoke(_ context.Context, tenantID, tokenID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.records {
		if r.ID == tokenID && string(r.TenantID) == tenantID && r.RevokedAt == nil {
			now := time.Now().UTC()
			m.records[i].RevokedAt = &now
			return nil
		}
	}
	return ErrTokenNotFound
}

// Rotate mints a fresh bearer for the (tenantID, tokenID) row, hashes
// it, revokes the old row, and inserts a new row with a fresh id. The
// new row inherits device_label + github_id from the prior row.
// Returns (newBearer, newID, deviceLabel).
func (m *MemTokenStore) Rotate(_ context.Context, tenantID, tokenID string) (string, string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.records {
		if r.ID == tokenID && string(r.TenantID) == tenantID && r.RevokedAt == nil {
			plaintext, err := mintToken()
			if err != nil {
				return "", "", "", err
			}
			hash, err := auth.HashToken(plaintext)
			if err != nil {
				return "", "", "", err
			}
			newID, err := NewTokenID()
			if err != nil {
				return "", "", "", err
			}
			now := time.Now().UTC()
			m.records[i].RevokedAt = &now
			// Append the replacement record. The old device_label is
			// preserved so the operator sees rotation history under
			// a stable name; in practice device_label is opaque.
			m.records = append(m.records, TokenRecord{
				ID:          newID,
				TenantID:    r.TenantID,
				GitHubID:    r.GitHubID,
				DeviceLabel: r.DeviceLabel,
				HashPHC:     hash,
				CreatedAt:   now,
			})
			return plaintext, newID, r.DeviceLabel, nil
		}
	}
	return "", "", "", ErrTokenNotFound
}

// Records returns a copy of the stored records — test-only accessor.
func (m *MemTokenStore) Records() []TokenRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]TokenRecord, len(m.records))
	copy(out, m.records)
	return out
}

// SQLTokenStore is the Dolt/MySQL-backed TokenStore. Same contract as
// MemTokenStore; persistence survives restart.
type SQLTokenStore struct {
	db *sql.DB
}

// NewSQLTokenStore wraps db. The pool's lifecycle is the caller's.
func NewSQLTokenStore(db *sql.DB) *SQLTokenStore { return &SQLTokenStore{db: db} }

// Migrate ensures the auth_tokens table exists. Idempotent.
func (s *SQLTokenStore) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, CreateAuthTokensTable); err != nil {
		return fmt.Errorf("oauth: create auth_tokens table: %w", err)
	}
	return nil
}

// Put implements TokenStore via INSERT ... ON DUPLICATE KEY UPDATE on
// the (tenant_id, device_label) UNIQUE key. Re-login from the same
// device replaces the hash; the row's id is preserved so prior
// rotate/revoke handles keep working. created_at is bumped so the
// operator can see the most recent login time; revoked_at is cleared
// (a fresh login un-revokes any prior soft delete on the same device).
func (s *SQLTokenStore) Put(ctx context.Context, rec TokenRecord) error {
	if rec.TenantID == "" || rec.DeviceLabel == "" || rec.HashPHC == "" {
		return errors.New("oauth: TokenRecord requires TenantID, DeviceLabel, HashPHC")
	}
	if rec.ID == "" {
		id, err := NewTokenID()
		if err != nil {
			return err
		}
		rec.ID = id
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO auth_tokens (id, tenant_id, github_id, device_label, hash_phc)
		VALUES (?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE hash_phc   = VALUES(hash_phc),
		                        github_id  = VALUES(github_id),
		                        created_at = CURRENT_TIMESTAMP,
		                        revoked_at = NULL`,
		rec.ID, string(rec.TenantID), rec.GitHubID, rec.DeviceLabel, rec.HashPHC)
	if err != nil {
		return fmt.Errorf("oauth: insert auth_tokens: %w", err)
	}
	return nil
}

// VerifyToken scans the auth_tokens table and runs argon2id verify
// against each non-revoked row. For v1 this is acceptable — token
// count is small. On a successful verify, last_used_at is best-effort
// updated; a failed update does not fail the auth check.
func (s *SQLTokenStore) VerifyToken(ctx context.Context, plaintext string) (tenant.ID, bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, tenant_id, hash_phc FROM auth_tokens WHERE revoked_at IS NULL`)
	if err != nil {
		return "", false, fmt.Errorf("oauth: scan auth_tokens: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, tid, phc string
		if err := rows.Scan(&id, &tid, &phc); err != nil {
			return "", false, fmt.Errorf("oauth: scan auth_tokens row: %w", err)
		}
		ok, err := auth.VerifyHash(plaintext, phc)
		if err != nil {
			return "", false, fmt.Errorf("oauth: verify hash: %w", err)
		}
		if ok {
			// Best-effort touch — close the rows iterator first so we
			// don't hold the connection across a second exec.
			_ = rows.Close()
			_, _ = s.db.ExecContext(ctx,
				`UPDATE auth_tokens SET last_used_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
			return tenant.ID(tid), true, nil
		}
	}
	return "", false, rows.Err()
}

// List returns metadata for non-revoked tokens belonging to tenantID.
func (s *SQLTokenStore) List(ctx context.Context, tenantID string) ([]TokenInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, device_label, created_at, last_used_at
		  FROM auth_tokens
		 WHERE tenant_id = ? AND revoked_at IS NULL
		 ORDER BY created_at ASC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("oauth: list auth_tokens: %w", err)
	}
	defer rows.Close()
	out := []TokenInfo{}
	for rows.Next() {
		var ti TokenInfo
		var last sql.NullTime
		if err := rows.Scan(&ti.ID, &ti.DeviceLabel, &ti.CreatedAt, &last); err != nil {
			return nil, fmt.Errorf("oauth: scan list row: %w", err)
		}
		if last.Valid {
			t := last.Time
			ti.LastUsedAt = &t
		}
		out = append(out, ti)
	}
	return out, rows.Err()
}

// Revoke soft-deletes (tenantID, tokenID).
func (s *SQLTokenStore) Revoke(ctx context.Context, tenantID, tokenID string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE auth_tokens
		   SET revoked_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND tenant_id = ? AND revoked_at IS NULL`,
		tokenID, tenantID)
	if err != nil {
		return fmt.Errorf("oauth: revoke auth_tokens: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("oauth: revoke rows affected: %w", err)
	}
	if n == 0 {
		return ErrTokenNotFound
	}
	return nil
}

// Rotate revokes (tenantID, tokenID) and inserts a fresh row carrying
// the same device_label + github_id in a single transaction so partial
// state is impossible. Returns (newBearer, newID, deviceLabel).
func (s *SQLTokenStore) Rotate(ctx context.Context, tenantID, tokenID string) (string, string, string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", "", fmt.Errorf("oauth: rotate begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var label string
	var ghID int64
	row := tx.QueryRowContext(ctx, `
		SELECT device_label, github_id
		  FROM auth_tokens
		 WHERE id = ? AND tenant_id = ? AND revoked_at IS NULL`,
		tokenID, tenantID)
	if err := row.Scan(&label, &ghID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", "", ErrTokenNotFound
		}
		return "", "", "", fmt.Errorf("oauth: rotate lookup: %w", err)
	}

	// Revoke the prior row before inserting the new one. The (tenant,
	// device_label) UNIQUE means we can't simultaneously hold two
	// non-revoked rows for the same device — Revoke first; INSERT
	// after; both inside the tx.
	if _, err := tx.ExecContext(ctx, `
		UPDATE auth_tokens SET revoked_at = CURRENT_TIMESTAMP
		 WHERE id = ?`, tokenID); err != nil {
		return "", "", "", fmt.Errorf("oauth: rotate revoke: %w", err)
	}

	plaintext, err := mintToken()
	if err != nil {
		return "", "", "", err
	}
	hash, err := auth.HashToken(plaintext)
	if err != nil {
		return "", "", "", err
	}
	newID, err := NewTokenID()
	if err != nil {
		return "", "", "", err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO auth_tokens (id, tenant_id, github_id, device_label, hash_phc)
		VALUES (?, ?, ?, ?, ?)`,
		newID, tenantID, ghID, label, hash); err != nil {
		return "", "", "", fmt.Errorf("oauth: rotate insert: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", "", "", fmt.Errorf("oauth: rotate commit: %w", err)
	}
	return plaintext, newID, label, nil
}
