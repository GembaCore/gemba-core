// Package audit — event type constants.
//
// Catalog locked by decision gm-o9t8.3.8. Producers MUST use these
// constants when constructing Events so the type strings stay stable
// across the codebase. Sinks (file, future cloud) treat Type as opaque
// — only the producers care about the catalog.
package audit

// Bead lifecycle.
const (
	EventBeadCreate = "bead.create"
	EventBeadUpdate = "bead.update"
	EventBeadClose  = "bead.close"
	EventBeadNote   = "bead.note"
)

// Run lifecycle (M3.2 microVM stories will wire producers).
const (
	EventRunSpawn    = "run.spawn"
	EventRunStop     = "run.stop"
	EventRunComplete = "run.complete"
	EventRunFailed   = "run.failed"
)

// Workspace lifecycle.
const (
	EventWorkspaceRatify = "workspace.ratify"
	EventWorkspaceDelete = "workspace.delete"
)

// Spec lifecycle.
const (
	EventSpecReconcile = "spec.reconcile"
	EventSpecSnapshot  = "spec.snapshot"
)

// Vault. The vault subagent injects an Auditor and emits these.
const (
	EventVaultWrite    = "vault.write"
	EventVaultReadUser = "vault.read_user"
	EventVaultDelete   = "vault.delete"
)

// Auth.
const (
	EventAuthLogin       = "auth.login"
	EventAuthTokenRotate = "auth.token_rotate"
	EventAuthTokenRevoke = "auth.token_revoke"
)

// Tenant lifecycle.
const (
	EventTenantCreate = "tenant.create"
	EventTenantUpdate = "tenant.update"
	EventTenantDelete = "tenant.delete"
)

// VM lifecycle.
const (
	EventVMSpawn        = "vm.spawn"
	EventVMDestroy      = "vm.destroy"
	EventVMEgressDenied = "vm.egress_denied"
)

// Outcome values.
const (
	OutcomeOK     = "ok"
	OutcomeDenied = "denied"
	OutcomeError  = "error"
)

// SingleTenant is the placeholder tenant id used in single-user mode.
const SingleTenant = "_"
