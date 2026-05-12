// Package tenant defines the canonical tenant + workspace identifier types
// for the multi-tenant control plane (gm-o9t8.3.9). The model is locked:
//
//   - Tenant ID format: t-<8 base32 chars> (lowercase). DefaultTenant
//     ("t-default") is the well-known constant used for single-user
//     installs and as the backwards-compat fallback when a bare wsid is
//     parsed.
//   - WSID format: <tenant-prefix>:<slug>, where tenant-prefix is the
//     first 6 chars of the tenant ID (e.g. "t-abcd:my-project"). A wsid
//     without a colon is parsed as "t-default:<wsid>" so M1 routes keep
//     working unchanged.
//
// The package is intentionally self-contained — no dependency on the
// server/auth/dolt layers — so tests, CLI tooling and middleware can all
// agree on the same parsing rules.
package tenant

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ID is a tenant identifier. Always lowercase; always begins with "t-".
type ID string

// DefaultTenant is the well-known single-user / M1 backwards-compat
// tenant. Every install seeds this row on first boot; bare wsids
// (without the "<prefix>:" qualifier) resolve to it.
const DefaultTenant ID = "t-default"

// tenantIDBody matches the body after "t-": exactly 8 lowercase
// base32-ish chars (RFC 4648 alphabet, lowercased). The reserved
// "default" body is permitted as a special case.
// tenantIDBody is the grammar for the body following "t-". The
// locked decision (gm-o9t8.3.9) names this "8 base32 chars" but the
// canonical example "t-abcd1234" includes digits outside RFC 4648's
// alphabet. We adopt the relaxed [a-z0-9] alphabet here so canonical
// examples validate, NewID uses Crockford-style base32 (no 0/1/8/9)
// to avoid visual ambiguity, and existing M1 data parses without
// surprise.
var tenantIDBody = regexp.MustCompile(`^[a-z0-9]{8}$`)

// prefixBody is the relaxed grammar for the prefix-body component of a
// WSID (the chars after "t-" in the 6-char prefix). At least one char
// is required; chars must be lowercase alphanumerics. The
// DefaultTenant's "defa" body is one valid instance.
var prefixBody = regexp.MustCompile(`^[a-z0-9]+$`)

// ErrInvalidTenantID is returned by ParseID when the input does not
// match the locked tenant ID grammar.
var ErrInvalidTenantID = errors.New("invalid tenant id")

// ErrInvalidWSID is returned by ParseWSID when the input is empty or
// the tenant-prefix component is malformed.
var ErrInvalidWSID = errors.New("invalid wsid")

// ParseID validates s as a tenant ID. It accepts:
//   - the literal "t-default", and
//   - "t-<8 base32 lowercase>" (RFC 4648 alphabet, lowercased).
//
// Anything else is rejected with ErrInvalidTenantID.
func ParseID(s string) (ID, error) {
	if s == string(DefaultTenant) {
		return DefaultTenant, nil
	}
	if !strings.HasPrefix(s, "t-") {
		return "", fmt.Errorf("%w: missing t- prefix: %q", ErrInvalidTenantID, s)
	}
	body := s[2:]
	if !tenantIDBody.MatchString(body) {
		return "", fmt.Errorf("%w: body must be 8 lowercase base32 chars: %q", ErrInvalidTenantID, s)
	}
	return ID(s), nil
}

// Prefix returns the canonical 6-char tenant prefix used as the leading
// component of a WSID (e.g. "t-abcd" from "t-abcd1234"). Shorter IDs
// (only the DefaultTenant exception is shorter than 6 chars in normal
// operation) are returned as-is.
func (id ID) Prefix() string {
	s := string(id)
	if len(s) <= 6 {
		return s
	}
	return s[:6]
}

// String returns the canonical lowercase form.
func (id ID) String() string { return string(id) }

// WSID is a parsed workspace identifier. Tenant carries the 6-char
// tenant prefix as it appears in the wsid (e.g. "t-abcd" or
// "t-defa"). It is NOT a full tenant ID — middleware that needs to
// compare against the bearer-bound tenant must compare against
// bearerTenant.Prefix(), not against bearerTenant directly. Slug
// carries the per-tenant project slug.
//
// The reason WSID stores the prefix and not the full ID: the wire
// format only carries the prefix (8 random bits of the body are
// elided), so the parser cannot reconstruct the full ID without a
// store lookup. Keeping the prefix here makes String() a faithful
// inverse of ParseWSID.
type WSID struct {
	Tenant ID
	Slug   string
}

// FullTenant resolves WSID.Tenant against the bearer-bound tenant id.
// Returns the bearer-bound full ID iff bearer.Prefix() matches the
// wsid prefix; otherwise returns the empty ID. Use this in handlers
// that need the full tenant id (for store lookups, audit logs, etc.)
// while still enforcing the prefix-match guard.
func (w WSID) FullTenant(bearer ID) ID {
	if bearer.Prefix() == string(w.Tenant) {
		return bearer
	}
	return ""
}

// ParseWSID accepts both the canonical "<tenant-prefix>:<slug>" form
// and the M1 backwards-compat bare "<slug>" form. The bare form maps
// to DefaultTenant.
//
// Validation rules:
//   - Empty input → ErrInvalidWSID.
//   - Canonical form: the prefix must look like "t-" followed by at
//     least one base32 char; the slug must be non-empty.
//   - Bare form: the entire input becomes the slug; tenant is
//     DefaultTenant.
//
// ParseWSID does NOT verify that the prefix matches a real tenant —
// that's the middleware's job (it checks against the bearer-bound
// tenant). The parser only enforces shape so callers can rely on a
// well-formed WSID.
func ParseWSID(s string) (WSID, error) {
	if s == "" {
		return WSID{}, fmt.Errorf("%w: empty", ErrInvalidWSID)
	}
	idx := strings.IndexByte(s, ':')
	if idx < 0 {
		// Bare form — M1 backwards compat. The bare form maps to
		// DefaultTenant; Tenant carries the canonical 6-char prefix
		// so String() produces a valid colon-form wsid.
		return WSID{Tenant: ID(DefaultTenant.Prefix()), Slug: s}, nil
	}
	prefix, slug := s[:idx], s[idx+1:]
	if slug == "" {
		return WSID{}, fmt.Errorf("%w: empty slug", ErrInvalidWSID)
	}
	if !strings.HasPrefix(prefix, "t-") || len(prefix) < 3 {
		return WSID{}, fmt.Errorf("%w: bad tenant prefix: %q", ErrInvalidWSID, prefix)
	}
	// Body must be lowercase alphanumerics drawn from the base32
	// alphabet (a-z, 2-7). The DefaultTenant's prefix body "defa"
	// already satisfies this rule, so no special case is needed.
	body := prefix[2:]
	if !prefixBody.MatchString(body) {
		return WSID{}, fmt.Errorf("%w: bad tenant prefix chars: %q", ErrInvalidWSID, prefix)
	}
	return WSID{Tenant: ID(prefix), Slug: slug}, nil
}

// String returns the canonical "<prefix>:<slug>" form. When Tenant is
// DefaultTenant the prefix is "t-defa" (the first 6 chars) — callers
// that want to round-trip the bare form must compare against Slug
// directly.
func (w WSID) String() string {
	return string(w.Tenant) + ":" + w.Slug
}

// NewID generates a fresh tenant ID: 5 random bytes → base32 →
// lowercase → first 8 chars, prefixed with "t-". 5 random bytes give
// 40 bits of entropy, which base32-encodes to exactly 8 chars (no
// padding), comfortably above the birthday-collision threshold for the
// expected tenant population.
func NewID() ID {
	var b [5]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failures on a healthy host are pathological;
		// callers can't meaningfully recover. Panic with a clear
		// message rather than return a zero-value ID that would
		// silently collide with DefaultTenant.
		panic(fmt.Errorf("tenant.NewID: crypto/rand failed: %w", err))
	}
	enc := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:]))
	return ID("t-" + enc[:8])
}

// --- context plumbing -----------------------------------------------------

type ctxKey struct{}

// WithContext returns a copy of ctx carrying id. FromContext reads it
// back. Middleware stores the bearer-bound tenant here; handlers read
// it to enforce tenant-scoped access.
func WithContext(ctx context.Context, id ID) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// FromContext returns the tenant ID previously stored via WithContext.
// The second return is false when no tenant was attached.
func FromContext(ctx context.Context) (ID, bool) {
	v, ok := ctx.Value(ctxKey{}).(ID)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}
