import type { ComponentType } from 'react';

// Extensions are adaptor-namespaced UI widgets that render an adaptor-
// native field, edge, or relationship attribute that core does not know
// about (gm-root DD-4). The namespacing rule is strict: an extension
// widget registered for adaptor "beads" MUST only render when the active
// work-plane manifest.adaptor_name === "beads". This keeps vocabulary
// leaks out of the UI — a Jira "story points" widget never surfaces
// when the active WorkPlane is beads.
//
// Widgets register themselves at module-load time by calling
// registerFieldExtension / registerEdgeExtension etc. The actual render
// happens inside ExtensionSlot components in this directory, which look
// up the right widget by (adaptor, kind, name).

export interface FieldExtensionProps {
  // value is whatever was emitted on WorkItem.custom[name]. JSON
  // scalar/object/array — the widget decodes it.
  value: unknown;
  // adaptor is the active WorkPlane manifest adaptor_name. Passed so
  // widgets registered once per adaptor id can render variants for
  // sibling products (e.g. one "beads" renderer per forked distro).
  adaptor: string;
}

export interface EdgeExtensionProps {
  // Arbitrary edge payload shape. Adaptors emit these inside
  // Relationship.payload; widgets decode per their own contract.
  payload: Record<string, unknown>;
  adaptor: string;
}

export interface RelFieldExtensionProps {
  value: unknown;
  adaptor: string;
}

type FieldRegistry = Map<string, Map<string, ComponentType<FieldExtensionProps>>>;
type EdgeRegistry = Map<string, Map<string, ComponentType<EdgeExtensionProps>>>;
type RelFieldRegistry = Map<string, Map<string, ComponentType<RelFieldExtensionProps>>>;

// Two-level maps: adaptorId -> name -> Component. A second lookup rather
// than a composite key keeps the "all extensions for this adaptor" walk
// cheap, which the capability browser (gm-e12.14) will want later.
const fieldRegistry: FieldRegistry = new Map();
const edgeRegistry: EdgeRegistry = new Map();
const relFieldRegistry: RelFieldRegistry = new Map();

function upsert<V>(m: Map<string, Map<string, V>>, a: string, k: string, v: V) {
  let inner = m.get(a);
  if (!inner) {
    inner = new Map();
    m.set(a, inner);
  }
  inner.set(k, v);
}

export function registerFieldExtension(
  adaptor: string,
  fieldName: string,
  component: ComponentType<FieldExtensionProps>
): void {
  upsert(fieldRegistry, adaptor, fieldName, component);
}

export function registerEdgeExtension(
  adaptor: string,
  edgeName: string,
  component: ComponentType<EdgeExtensionProps>
): void {
  upsert(edgeRegistry, adaptor, edgeName, component);
}

export function registerRelFieldExtension(
  adaptor: string,
  fieldName: string,
  component: ComponentType<RelFieldExtensionProps>
): void {
  upsert(relFieldRegistry, adaptor, fieldName, component);
}

export function resolveFieldExtension(
  adaptor: string,
  fieldName: string
): ComponentType<FieldExtensionProps> | null {
  return fieldRegistry.get(adaptor)?.get(fieldName) ?? null;
}

export function resolveEdgeExtension(
  adaptor: string,
  edgeName: string
): ComponentType<EdgeExtensionProps> | null {
  return edgeRegistry.get(adaptor)?.get(edgeName) ?? null;
}

export function resolveRelFieldExtension(
  adaptor: string,
  fieldName: string
): ComponentType<RelFieldExtensionProps> | null {
  return relFieldRegistry.get(adaptor)?.get(fieldName) ?? null;
}

// Test-only: wipe the registries so a vitest run isn't contaminated by
// prior registrations. Not exposed in the public `index.ts` barrel.
export function __resetExtensionRegistryForTests(): void {
  fieldRegistry.clear();
  edgeRegistry.clear();
  relFieldRegistry.clear();
}
