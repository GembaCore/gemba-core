// surfaceKind classifies an adaptor by which "surface" it belongs to:
//
//   * organization — team/org tools shared across projects (Jira, XRay,
//     linters, compliance/policy tooling). These describe what the team
//     already does outside this workspace.
//   * execution — per-workspace tools doing the actual work here
//     (Beads, Gas Town, LangGraph, per-workspace CI). These describe
//     what happens inside this workspace.
//
// Lookup is keyed on the `adaptor_name` field of CapabilityManifest
// (or `adaptor_id` for orchestration manifests). The set is hard-coded
// for v1 — adaptors today are few and known. Unknown adaptors default
// to 'execution' because that's where most of them live.
//
// Keep the patterns conservative: prefix matches for `lint-` and
// `policy-` so e.g. `lint-eslint`, `policy-soc2` slot under organization
// without each having to be enumerated.

export type SurfaceKind = 'organization' | 'execution';

const ORGANIZATION_NAMES: ReadonlySet<string> = new Set([
  'jira',
  'xray',
  'compliance',
]);

const EXECUTION_NAMES: ReadonlySet<string> = new Set([
  'beads',
  'gastown',
  'gas-town',
  'langgraph',
  'noop',
  'dolt',
  'bd',
]);

const ORGANIZATION_PREFIXES: readonly string[] = ['lint-', 'policy-'];

export function classifyAdaptor(adaptorName: string): SurfaceKind {
  const name = adaptorName.toLowerCase();
  if (ORGANIZATION_NAMES.has(name)) return 'organization';
  for (const prefix of ORGANIZATION_PREFIXES) {
    if (name.startsWith(prefix)) return 'organization';
  }
  if (EXECUTION_NAMES.has(name)) return 'execution';
  // Default fallback: most adaptors today are execution-surface.
  return 'execution';
}
