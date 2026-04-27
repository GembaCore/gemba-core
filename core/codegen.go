// Package core: see doc.go for the overview.
package core

import (
	"fmt"
	"reflect"
	"strings"
	"time"
)

// CoreTypesTS is the adaptor-agnostic domain surface expressed in
// TypeScript. It is the single source of truth that drives the generated
// `web/src/types/core.gen.ts`.
//
// Most of it is emitted by walking Go structs via reflection (see
// GenerateTS); the small amount of hand-maintained text — enum unions,
// AdaptorError (whose wire shape is shaped by MarshalJSON, not the Go
// struct) and the FLAG_NAMES const — is concatenated around the
// reflection output.
//
// This value is written verbatim by `cmd/gen-core-types`; tests in this
// package assert shape-level parity against the Go types. The file under
// web/ should only be regenerated via `go generate ./core` (or `make gen`)
// (or `make gen`) so that humans don't hand-edit a generated artifact.
var CoreTypesTS = GenerateTS()

// GenerateTS renders the current TypeScript projection of the core
// domain types. Output is deterministic and byte-stable across runs so
// the file is safe to commit.
func GenerateTS() string {
	var b strings.Builder
	b.WriteString(tsPreamble)

	for _, e := range tsExports {
		b.WriteString("\n")
		if e.comment != "" {
			b.WriteString(e.comment)
			if !strings.HasSuffix(e.comment, "\n") {
				b.WriteString("\n")
			}
		}
		b.WriteString(emitInterface(e.goType, e.name))
	}

	b.WriteString(tsPostamble)
	return b.String()
}

// tsExport registers a Go struct type for reflection-based TS emission.
type tsExport struct {
	goType  reflect.Type
	name    string
	comment string // optional leading doc block (must include "//" line prefixes + trailing newline)
}

// tsExports controls both the set and the emission order of the
// reflection-driven TS interfaces. Adding a new core struct here is the
// full extension point — no hand-written shape below.
var tsExports = []tsExport{
	{goType: reflect.TypeOf(AgentRef{}), name: "AgentRef"},
	{goType: reflect.TypeOf(Relationship{}), name: "Relationship"},
	{goType: reflect.TypeOf(Evidence{}), name: "Evidence"},
	{goType: reflect.TypeOf(DefinitionOfDone{}), name: "DefinitionOfDone"},
	{goType: reflect.TypeOf(TokenBudget{}), name: "TokenBudget"},
	{goType: reflect.TypeOf(Sprint{}), name: "Sprint"},
	{
		goType: reflect.TypeOf(DerivedSignals{}),
		name:   "DerivedSignals",
		comment: `// DerivedSignals are UI-facing booleans computed by the server from a
// WorkItem plus its open escalations (gm-gsh). They answer the three
// questions the Kanban / backlog / agents dashboards ask most often;
// consumers MUST treat them as pure-derived cache values and re-derive
// locally rather than persisting them. Emitted on WorkItem.derived.`,
	},
	{
		goType: reflect.TypeOf(BeadBranch{}),
		name:   "BeadBranch",
		comment: `// BeadBranch maps a repository to the git branch a bead's work
// happens on inside it (gm-ou02). Empty WorkItem.branches falls back
// to a derived branch name.`,
	},
	{goType: reflect.TypeOf(WorkItem{}), name: "WorkItem"},
	{
		goType: reflect.TypeOf(Repository{}),
		name:   "Repository",
		comment: `// Repository is one git repository associated with the workspace
// (gm-26n4). Path is the canonical worktree root; BeadPrefix
// (gm-d2ts) is the prefix bead ids use to identify this repo in
// cross-repo contexts.`,
	},
	{
		goType: reflect.TypeOf(Flags{}),
		name:   "Flags",
		comment: `// Flags is the flat-boolean projection of a CapabilityManifest intended
// for UI gating (see core/manifest.go). The rich manifest is
// kept for server-side decisions; Flags is what <Capability has="..."/>
// consumes in the SPA. Deterministic: derive it from the manifest on
// every render rather than caching it across reconnects.`,
	},
	{
		goType: reflect.TypeOf(FieldExtension{}),
		name:   "FieldExtension",
		comment: `// FieldExtension declares an adaptor-native field that core does not
// know about. The UI only renders it inside
// web/src/extensions/<adaptor-id>/ (gm-root DD-4).`,
	},
	{
		goType: reflect.TypeOf(EdgeExtension{}),
		name:   "EdgeExtension",
		comment: `// EdgeExtension declares an adaptor-native relationship kind that is
// not one of the three core edges (gm-root DD-9). The UI falls back to
// "relates_to" semantics when the adaptor's extension widget is absent.`,
	},
	{
		goType: reflect.TypeOf(RelationshipExtension{}),
		name:   "RelationshipExtension",
		comment: `// RelationshipExtension declares an adaptor-native field on the
// Relationship record itself (not a new edge kind).`,
	},
	{
		goType: reflect.TypeOf(CapabilityManifest{}),
		name:   "CapabilityManifest",
		comment: `// CapabilityManifest is the declarative description every WorkPlane
// adaptor returns from Describe. It tells core (and the UI) which
// transport the adaptor speaks, how to normalize its native statuses,
// what extensions it carries beyond the core primitives, and which
// optional feature groups it opts into (gm-root DD-15).`,
	},
	{
		goType: reflect.TypeOf(GroupMembers{}),
		name:   "GroupMembers",
		comment: `// GroupMembers is the mode-tagged union of member descriptors on an
// AgentGroup (gm-root DD-7). Only the subset matching Mode is
// meaningful; the rest MUST be zero.`,
	},
	{
		goType: reflect.TypeOf(AgentGroup{}),
		name:   "AgentGroup",
		comment: `// AgentGroup is the adaptor-agnostic view of a collection of agents
// (gm-root DD-7). The Mode field picks the union arm carried by Members.`,
	},
}

// emitInterface renders a Go struct as a TypeScript interface. Field
// names come from the json tag; types come from tsType.
func emitInterface(t reflect.Type, name string) string {
	if t.Kind() != reflect.Struct {
		panic(fmt.Sprintf("core: emitInterface expects a struct, got %s", t.Kind()))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "export interface %s {\n", name)

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "-" || tag == "" {
			continue
		}
		parts := strings.Split(tag, ",")
		jsonName := parts[0]
		optional := false
		for _, opt := range parts[1:] {
			if opt == "omitempty" {
				optional = true
			}
		}
		nullable := f.Type.Kind() == reflect.Ptr
		tsTypeStr := tsType(f.Type)

		marker := ": "
		if optional {
			marker = "?: "
		}
		suffix := ";"
		if nullable {
			suffix = " | null;"
		}
		fmt.Fprintf(&b, "  %s%s%s%s\n", jsonName, marker, tsTypeStr, suffix)
	}

	b.WriteString("}\n")
	return b.String()
}

// tsType maps a reflect.Type to its TS projection.
//
// Rules (see bead gm-4lc):
//   - time.Time           -> string (ISO-8601)
//   - map[string]any      -> Record<string, unknown>
//   - map[K]V             -> Record<K, V>
//   - []T                 -> T[]  (byte slice -> string, base64)
//   - *T                  -> T    (nullability is encoded at field level)
//   - named string alias  -> its Go name (WorkItemID, StateCategory, …)
//   - primitives          -> string / number / boolean
//   - interface{}         -> unknown
func tsType(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Ptr:
		return tsType(t.Elem())
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number"
	case reflect.String:
		// Named string aliases (StateCategory, WorkItemID, …) surface as
		// their Go name so the TS aliases emitted in the preamble line up.
		if t.PkgPath() != "" && t.Name() != "" {
			return t.Name()
		}
		return "string"
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return "string" // []byte is base64-encoded by encoding/json
		}
		return tsType(t.Elem()) + "[]"
	case reflect.Map:
		// A named map alias (e.g. StateMap = Record<string, StateCategory>)
		// is emitted as its alias name so the hand-declared TS type in the
		// preamble stays referenced.
		if t.PkgPath() != "" && t.Name() != "" {
			return t.Name()
		}
		// map[string]any is the idiom for "free-form JSON blob"; emit the
		// TS equivalent literally.
		if t.Key().Kind() == reflect.String && t.Elem().Kind() == reflect.Interface {
			return "Record<string, unknown>"
		}
		return fmt.Sprintf("Record<%s, %s>", tsType(t.Key()), tsType(t.Elem()))
	case reflect.Struct:
		if t == reflect.TypeOf(time.Time{}) {
			return "string"
		}
		if t.Name() != "" {
			return t.Name()
		}
		return "unknown"
	case reflect.Interface:
		return "unknown"
	}
	return "unknown"
}

// tsPreamble is the hand-maintained top of the generated file: header,
// string-alias types, enum unions + their const arrays, and the
// AdaptorError interface (whose wire shape is synthesised by
// AdaptorError.MarshalJSON and so is not directly reflectable).
const tsPreamble = `// Code generated by cmd/gen-core-types. DO NOT EDIT.
//
// Source: core (WorkItem, AgentRef, Relationship, Evidence,
// DefinitionOfDone, Sprint, TokenBudget, StateCategory, Flags,
// CapabilityManifest + extensions).
//
// Regenerate with: make gen

export type WorkItemID = string;
export type AgentID = string;
export type RepositoryID = string;

export type StateCategory =
  | "backlog"
  | "unstarted"
  | "staged"
  | "started"
  | "completed"
  | "canceled";

export const STATE_CATEGORIES: readonly StateCategory[] = [
  "backlog",
  "unstarted",
  "staged",
  "started",
  "completed",
  "canceled",
] as const;

export type RelationshipKind =
  | "blocks"
  | "parent_child"
  | "relates_to";

export type EvidenceKind =
  | "commit"
  | "log"
  | "test_result"
  | "url"
  | "file"
  | "custom";

export type DispatchStatus =
  | ""
  | "ready"
  | "awaiting-design"
  | "awaiting-vendor"
  | "awaiting-review"
  | "not-now";

export const DISPATCH_STATUSES: readonly DispatchStatus[] = [
  "ready",
  "awaiting-design",
  "awaiting-vendor",
  "awaiting-review",
  "not-now",
] as const;

export type EstimatedSize =
  | ""
  | "small"
  | "medium"
  | "large";

export const ESTIMATED_SIZES: readonly EstimatedSize[] = [
  "small",
  "medium",
  "large",
] as const;

// KIND_MILESTONE is the canonical WorkItem.kind token for milestones
// (gm-root.3). WorkItem.kind is a free string because adaptors emit
// kinds beyond the core's knowledge, so this is an exported constant
// rather than a union variant. SPA code that gates on "is this a
// milestone?" MUST compare against KIND_MILESTONE rather than
// hardcoding the literal.
export const KIND_MILESTONE = "milestone";

// SessionStatus is the observable session lifecycle tag (gm-d044).
// The SPA uses it to render the per-session badge on the board + drawer
// (gm-r1l1). Suspended is the only non-observable value; it's kept for
// the pre-existing Pause/Resume flow and sits outside the
// initializing/ready/working/prompting observable set.
export type SessionStatus =
  | "initializing"
  | "ready"
  | "working"
  | "prompting"
  | "stalled"
  | "suspended"
  | "completed"
  | "failed";

export const SESSION_STATUSES: readonly SessionStatus[] = [
  "initializing",
  "ready",
  "working",
  "prompting",
  "stalled",
  "suspended",
  "completed",
  "failed",
] as const;

export type BudgetTier = "inform" | "warn" | "stop";

export type AgentKind = "agent" | "human";

export type Transport = "api" | "jsonl" | "mcp";

export const TRANSPORTS: readonly Transport[] = [
  "api",
  "jsonl",
  "mcp",
] as const;

// StateMap is the adaptor's declarative translation from its native
// status tokens to the five core StateCategory buckets. Required on
// every CapabilityManifest.
export type StateMap = Record<string, StateCategory>;

// Capability-manifest string-literal enums (R1–R8 fields, gm-knrm).
// Mirror the Go-side authoritative sets in core/workplane.go;
// keep in lockstep — the Validate() calls there enforce the same set.
//
// Generated as plain string-literal unions rather than full enums so
// the SPA can pattern-match exhaustively. New variants must land in
// both the Go const block AND here, in the same order, so the manifest
// round-trip parity tests stay honest.
export type SchemaEnforcement =
  | "native"
  | "synthesized";

export type QueryLanguage =
  | "filter-only"
  | "jsonpath"
  | "sql-subset"
  | "graphql";

export type VersioningTransport =
  | "none"
  | "git"
  | "dolt"
  | "jsonl"
  | "native-sqlite-export";

export type ConcurrencyModel =
  | "optimistic"
  | "mvcc"
  | "git-merge"
  | "dolt-merge";

export type AgentNativeAPI =
  | "cli"
  | "json-api"
  | "mcp"
  | "rest-only";

export type OrchestratorHook =
  | "ready-set-subscribe"
  | "claim-atomic"
  | "escalation-ingest"
  | "work-complete-ack"
  | "pool-bulk-dispatch";

// GroupMode discriminates an AgentGroup's membership shape (gm-root DD-7).
// Mirror core/orchestration.go's Group* constants — keep in lockstep.
export type GroupMode =
  | "static"
  | "pool"
  | "graph";

// AdaptorErrorKind is the closed discriminator every adaptor-boundary
// error carries (gm-faz). The SPA branches on _kind + retryable; it
// MUST NOT parse the human-readable message.
export type AdaptorErrorKind =
  | "validation"
  | "session_not_found"
  | "session_closed"
  | "request_failed"
  | "process_failed"
  | "rate_limited"
  | "unsupported"
  | "capability_denied"
  | "adaptor_degraded"
  | "read_only";

export const ADAPTOR_ERROR_KINDS: readonly AdaptorErrorKind[] = [
  "validation",
  "session_not_found",
  "session_closed",
  "request_failed",
  "process_failed",
  "rate_limited",
  "unsupported",
  "capability_denied",
  "adaptor_degraded",
  "read_only",
] as const;

// AdaptorError is the wire shape emitted by every Go adaptor-boundary
// method on failure. The Go MarshalJSON flattens the typed Cause to a
// plain string so the SPA never has to walk an unknown error tree.
//
// Consumers branch on _kind for category and retryable for retry
// intent; all other fields are advisory.
export interface AdaptorError {
  _kind: AdaptorErrorKind;
  retryable: boolean;
  message: string;
  cause?: string;
  detail?: Record<string, unknown>;
}
`

// tsPostamble trails the reflection-emitted blocks with the FLAG_NAMES
// const + FlagName alias. These depend on the string literals matching
// Flags' JSON tags, which is asserted by codegen_test.go.
const tsPostamble = `
// FLAG_NAMES is the exhaustive list of keys on Flags. Useful for
// exhaustiveness checks in the SPA's gating layer (so a new flag added
// on the Go side surfaces as a type error on the UI side).
export const FLAG_NAMES = [
  "has_sprints",
  "has_budget",
  "has_evidence",
  "has_declared_state",
  "has_extension_edges",
  "has_extension_fields",
  "has_extension_rel_fields",
] as const;

export type FlagName = (typeof FLAG_NAMES)[number];
`
