import type { ReactNode } from 'react';
import { useCapabilities } from './context-internal';
import {
  resolveEdgeExtension,
  resolveFieldExtension,
  resolveRelFieldExtension,
} from './extension-registry';

// FieldExtensionSlot renders the adaptor-registered widget for a custom
// field ON A WorkItem (WorkItem.custom[name]). Three gates must pass:
//
//   1. The active work-plane manifest declares field_extensions[name].
//   2. A widget has been registered for (adaptor_name, name).
//   3. The value is present (undefined is treated as "no value, hide").
//
// All three missing the mark fall back to `fallback` (null by default).
// The strict gating matters: the UI MUST NOT render raw JSON for an
// extension that the manifest doesn't declare (DD-4 vocabulary leak).

export interface FieldExtensionSlotProps {
  name: string;
  value: unknown;
  fallback?: ReactNode;
}

export function FieldExtensionSlot({ name, value, fallback = null }: FieldExtensionSlotProps) {
  const { workPlane } = useCapabilities();
  if (!workPlane || value === undefined) return <>{fallback}</>;

  const declared = (workPlane.field_extensions ?? []).some((f) => f.name === name);
  if (!declared) return <>{fallback}</>;

  const Widget = resolveFieldExtension(workPlane.adaptor_name, name);
  if (!Widget) return <>{fallback}</>;

  return <Widget value={value} adaptor={workPlane.adaptor_name} />;
}

export interface EdgeExtensionSlotProps {
  name: string;
  payload: Record<string, unknown>;
  fallback?: ReactNode;
}

export function EdgeExtensionSlot({ name, payload, fallback = null }: EdgeExtensionSlotProps) {
  const { workPlane } = useCapabilities();
  if (!workPlane) return <>{fallback}</>;

  const declared = (workPlane.edge_extensions ?? []).some((e) => e.name === name);
  if (!declared) return <>{fallback}</>;

  const Widget = resolveEdgeExtension(workPlane.adaptor_name, name);
  if (!Widget) return <>{fallback}</>;

  return <Widget payload={payload} adaptor={workPlane.adaptor_name} />;
}

export interface RelFieldExtensionSlotProps {
  name: string;
  value: unknown;
  fallback?: ReactNode;
}

export function RelFieldExtensionSlot({
  name,
  value,
  fallback = null,
}: RelFieldExtensionSlotProps) {
  const { workPlane } = useCapabilities();
  if (!workPlane || value === undefined) return <>{fallback}</>;

  const declared = (workPlane.relationship_extensions ?? []).some((r) => r.name === name);
  if (!declared) return <>{fallback}</>;

  const Widget = resolveRelFieldExtension(workPlane.adaptor_name, name);
  if (!Widget) return <>{fallback}</>;

  return <Widget value={value} adaptor={workPlane.adaptor_name} />;
}
