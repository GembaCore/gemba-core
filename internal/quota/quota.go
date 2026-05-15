// gm-o9t8.4.2.1 — tier-aware quota model.
//
// This file defines the customer-facing tier enumeration (free / solo /
// teams / enterprise) and the Limits envelope each tier maps to. The
// numbers here are the source of truth for tier enforcement: the
// middleware in internal/server/middleware/quota.go consults
// LimitsForTier to decide which token-bucket parameters and which
// concurrent-resource ceilings to apply for a given request.
//
// The values default to conservative numbers small enough that free
// tier usage cannot accidentally exhaust shared infra. Production
// rollouts override the defaults by editing this file (so the change
// is reviewable in a single PR) rather than via runtime config — the
// tiers are a commercial commitment, not an operational knob.
package quota

import (
	"errors"
	"fmt"
	"strings"
)

// Tier is the customer subscription level. Stored as a TEXT column on
// the tenants table; defaults to TierFree for unprovisioned rows.
type Tier string

const (
	// TierFree is the no-cost tier. Hard caps on every dimension.
	TierFree Tier = "free"
	// TierSolo is the single-developer paid tier.
	TierSolo Tier = "solo"
	// TierTeams is the small-team paid tier.
	TierTeams Tier = "teams"
	// TierEnterprise is the enterprise tier; numbers here are floors —
	// individual enterprise deals can override via per-tenant config.
	TierEnterprise Tier = "enterprise"
)

// Limits is the tier envelope consulted by middleware and admission
// gates. Fields:
//
//	MaxConcurrentVMs   — how many sandbox VMs may be running at once.
//	RPS                — sustained tokens/sec for the request bucket.
//	Burst              — max bucket size (initial fill).
//	MonthlyVMMinutes   — soft cap on total VM-minutes billed per month.
//	MaxWorkspaces      — total live workspaces per tenant.
//
// All fields are non-negative; a zero value disables enforcement for
// that dimension (used by tests, not by production tiers).
type Limits struct {
	MaxConcurrentVMs int
	RPS              int
	Burst            int
	MonthlyVMMinutes int
	MaxWorkspaces    int
}

// ErrUnknownTier is returned by ParseTier for inputs that do not match
// one of the four canonical tier names.
var ErrUnknownTier = errors.New("quota: unknown tier")

// LimitsForTier returns the Limits envelope for t. An unrecognized
// tier maps to the free-tier defaults — this is intentional: a tenant
// row with a garbled tier column should degrade safe rather than
// fall through to unlimited.
func LimitsForTier(t Tier) Limits {
	switch t {
	case TierSolo:
		return Limits{
			MaxConcurrentVMs: 3,
			RPS:              10,
			Burst:            30,
			MonthlyVMMinutes: 600,
			MaxWorkspaces:    5,
		}
	case TierTeams:
		return Limits{
			MaxConcurrentVMs: 10,
			RPS:              50,
			Burst:            100,
			MonthlyVMMinutes: 3000,
			MaxWorkspaces:    25,
		}
	case TierEnterprise:
		return Limits{
			MaxConcurrentVMs: 100,
			RPS:              500,
			Burst:            1000,
			MonthlyVMMinutes: 30000,
			MaxWorkspaces:    200,
		}
	case TierFree:
		fallthrough
	default:
		return Limits{
			MaxConcurrentVMs: 1,
			RPS:              2,
			Burst:            5,
			MonthlyVMMinutes: 60,
			MaxWorkspaces:    1,
		}
	}
}

// ParseTier normalizes s (case-insensitive, whitespace-trimmed) into a
// Tier. The empty string parses as TierFree — convenient for migrating
// legacy rows that pre-date the tier column.
func ParseTier(s string) (Tier, error) {
	v := strings.ToLower(strings.TrimSpace(s))
	switch v {
	case "":
		return TierFree, nil
	case string(TierFree), string(TierSolo), string(TierTeams), string(TierEnterprise):
		return Tier(v), nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownTier, s)
	}
}

// String returns the canonical lowercase tier name.
func (t Tier) String() string { return string(t) }
