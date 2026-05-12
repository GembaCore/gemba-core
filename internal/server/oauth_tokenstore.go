// gm-o9t8.3.5.2 — auth_tokens persistence.
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
package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/GembaCore/gemba-core/internal/auth"
	"github.com/GembaCore/gemba-core/internal/tenant"
)

// CreateAuthTokensTable is the DDL the tenant package's Migrate would
// run on a multi-tenant rig. Idempotent. Mirrors the tenants table
// migration convention.
const CreateAuthTokensTable = `CREATE TABLE IF NOT EXISTS auth_tokens (
	tenant_id     VARCHAR(16)  NOT NULL,
	github_id     BIGINT       NOT NULL,
	device_label  VARCHAR(64)  NOT NULL,
	hash_phc      TEXT         NOT NULL,
	created_at    TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (tenant_id, device_label),
	KEY idx_auth_tokens_github_id (github_id)
)`

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
// stacking dead rows.
func (m *MemTokenStore) Put(_ context.Context, rec TokenRecord) error {
	if rec.TenantID == "" || rec.DeviceLabel == "" || rec.HashPHC == "" {
		return errors.New("oauth: TokenRecord requires TenantID, DeviceLabel, HashPHC")
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.records {
		if r.TenantID == rec.TenantID && r.DeviceLabel == rec.DeviceLabel {
			m.records[i] = rec
			return nil
		}
	}
	m.records = append(m.records, rec)
	return nil
}

// VerifyToken implements TokenStore. Iterates the stored records and
// runs argon2id Verify against each hash. Returns the matching
// tenant id on success, or ("", false, nil) on miss.
func (m *MemTokenStore) VerifyToken(_ context.Context, plaintext string) (tenant.ID, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, r := range m.records {
		ok, err := auth.VerifyHash(plaintext, r.HashPHC)
		if err != nil {
			// A malformed stored hash is an infra bug — surface it so
			// the operator notices. Don't continue scanning: a single
			// bad row should not silently accept a guess against a
			// later valid one.
			return "", false, fmt.Errorf("oauth: verify hash: %w", err)
		}
		if ok {
			return r.TenantID, true, nil
		}
	}
	return "", false, nil
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

// Put implements TokenStore via INSERT ... ON DUPLICATE KEY UPDATE
// (MySQL/Dolt upsert). Re-login from the same device replaces the
// hash; the created_at is bumped so the operator can see the most
// recent login time.
func (s *SQLTokenStore) Put(ctx context.Context, rec TokenRecord) error {
	if rec.TenantID == "" || rec.DeviceLabel == "" || rec.HashPHC == "" {
		return errors.New("oauth: TokenRecord requires TenantID, DeviceLabel, HashPHC")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO auth_tokens (tenant_id, github_id, device_label, hash_phc)
		VALUES (?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE hash_phc = VALUES(hash_phc),
		                        github_id = VALUES(github_id),
		                        created_at = CURRENT_TIMESTAMP`,
		string(rec.TenantID), rec.GitHubID, rec.DeviceLabel, rec.HashPHC)
	if err != nil {
		return fmt.Errorf("oauth: insert auth_tokens: %w", err)
	}
	return nil
}

// VerifyToken scans the auth_tokens table and runs argon2id verify
// against each row. For v1 this is acceptable — token count is small.
// When it grows beyond a few thousand rows we add an HMAC index or
// switch to a stateless JWT bearer.
func (s *SQLTokenStore) VerifyToken(ctx context.Context, plaintext string) (tenant.ID, bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT tenant_id, hash_phc FROM auth_tokens`)
	if err != nil {
		return "", false, fmt.Errorf("oauth: scan auth_tokens: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var tid, phc string
		if err := rows.Scan(&tid, &phc); err != nil {
			return "", false, fmt.Errorf("oauth: scan auth_tokens row: %w", err)
		}
		ok, err := auth.VerifyHash(plaintext, phc)
		if err != nil {
			return "", false, fmt.Errorf("oauth: verify hash: %w", err)
		}
		if ok {
			return tenant.ID(tid), true, nil
		}
	}
	return "", false, rows.Err()
}
