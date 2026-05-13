# ADR 0001: Spec Kit Integration Strategy

## Status

Accepted — 2026-05-13

## Context

GitHub's Spec Kit ships with `tasks.md` as its canonical work ledger:
the `/tasks` slash command, prompt templates, and constitution defaults
all assume a markdown task file lives alongside the spec. Gemba, by
contrast, ships with beads (`bd`) as its work ledger; tasks are first-class
issues in a Dolt-backed graph, not entries in a flat markdown file.

Phase 1 of ASDD (issues gm-v0sp.15 through gm-v0sp.19) has already
deployed a hook-layer interception strategy:

- `PreToolUse` hooks on `Write`, `Edit`, `NotebookEdit`, and `Bash`
  that refuse writes to `tasks.md` under spec directories.
- Slash command overrides that redirect `/tasks` to a beads-aware
  flow.
- A constitution clause, `spec_strict_no_tasks_md`, that documents
  the prohibition for downstream agents.

With Phase 1 in place, the remaining strategic question is how
gemba should relate to upstream Spec Kit going forward. Three
options were considered:

- **(a) Hook-layer only.** Keep the interception surgical. Gemba-aware
  projects install our hook config; Spec Kit itself stays untouched.
- **(b) Fork as `gemba-spec-kit`.** Replace Spec Kit's prompt templates
  and `tasks.md` assumptions wholesale; gemba projects use the fork.
- **(c) Upstream a `backend: tasks-md | bd | mcp` knob.** Push a PR to
  GitHub Spec Kit that makes the work-ledger backend pluggable.

## Decision

Adopt **option (a) — hook-layer only — as the v1 integration strategy.**

Option (c) — upstreaming a backend knob — is **deferred** as the
conditional next step, to be pursued when adoption justifies the
maintenance commitment.

Option (b) — forking Spec Kit — is treated as a **last resort**,
to be triggered only if upstream actively breaks our hook layer
and no quick upstream collaboration is possible.

## Triggers for Revisit

This decision will be revisited if any of the following occur:

- **Persistent LLM drift.** The first operator report of an LLM
  creating `tasks.md` despite Phase 1 hooks is already in scope for
  the analyzer seam (gm-v0sp.21). If drift continues *after* the
  analyzer ships, re-evaluate whether hooks alone are sufficient.
- **Upstream surface change.** Spec Kit ships a release that breaks
  hook-layer assumptions about its slash-command surface or its
  prompt-template layout.
- **Adoption inflection.** Adoption reaches roughly ten gemba-aware
  repos. At that scale, the maintenance cost of upstreaming
  (option c) is justified, and we should open the PR.

## Consequences

**Positive**

- Minimal surface area. We carry no fork, no patches, no template
  divergence.
- No upstream maintenance burden. Spec Kit releases land cleanly;
  gemba absorbs them at the hook boundary.
- Cheap to revisit. Hooks are local config; reversing or upgrading
  the strategy does not require migrating any upstream artifact.

**Negative**

- Gemba projects **must** install our hook config. A project that
  skips the hook install will silently fall back to Spec Kit's
  `tasks.md` default.
- Operators who deliberately bypass hooks (for example, by editing
  a sidecar file via a tool not covered by `PreToolUse`) defeat the
  guardrail. The constitution clause `spec_strict_no_tasks_md` and
  the preflight check mitigate but do not eliminate this risk.

**Operational**

- Any meaningful change to Spec Kit's `/tasks` slash-command output
  format risks silently breaking our interception. Mitigation:
  add a CI smoke test that exercises our hook against the embedded
  `templates/commands/tasks.md` from upstream Spec Kit, so a drift
  surfaces as a red build rather than a runtime drift in the field.

## Alternatives Considered

**(b) Fork Spec Kit as `gemba-spec-kit`.** Rejected for v1. A fork
gives us total control over prompts and templates, but at the cost
of perpetual upstream-tracking work for every Spec Kit release. The
fork also fragments the broader Spec Kit community: gemba operators
would have to choose between "the real Spec Kit" and "the gemba one,"
and any third-party Spec Kit improvements would need manual rebases.
Kept on the table only as a defensive fallback if upstream breaks
the hook layer and is unwilling to accept upstream patches.

**(c) Upstream a `backend` config knob.** Deferred. This is the
"right" long-term answer: a `backend: tasks-md | bd | mcp` setting
in Spec Kit's own config would make the work-ledger pluggable for
everyone, not just gemba. We are not pursuing it now because
(1) we do not yet have enough adoption to justify the upstream
conversation, and (2) Phase 1 hooks already deliver the user-visible
outcome. We will open the PR when adoption hits roughly ten
gemba-aware repos or when a Spec Kit maintainer signals receptivity,
whichever comes first.

## References

- [ASDD whitepaper](../asdd-whitepaper.md) — canonical reference for
  the gemba approach to spec-driven development.
- Phase 1 implementation beads: gm-v0sp.15, gm-v0sp.16, gm-v0sp.17,
  gm-v0sp.18, gm-v0sp.19 (Spec Kit interception layer — hooks,
  slash command overrides, constitution clause).
- gm-v0sp.21 — analyzer seam for detecting drift past the hook
  layer.
- gm-v0sp.20 — the decision bead this ADR resolves.
