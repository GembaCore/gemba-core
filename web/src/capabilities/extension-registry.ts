import type { ComponentType } from 'react';
import type { FieldExtension } from './types';

// Adaptor-namespaced field-extension registry (gm-root DD-4).
// Lives in a .ts file so the companion FieldExtensions component can
// keep a component-only .tsx file for fast-refresh.

export interface FieldRendererProps {
  field: FieldExtension;
  value: unknown;
}

export type FieldRenderer = ComponentType<FieldRendererProps>;

// The registry key is (adaptor_name, field_name). Adaptor widgets MUST
// namespace by adaptor_name to prevent two adaptors shipping an
// "owner" field from colliding — the manifest's adaptor_name ties the
// namespace.
type RegistryKey = `${string}/${string}`;
const fieldRegistry = new Map<RegistryKey, FieldRenderer>();

export function registerFieldRenderer(
  adaptorName: string,
  fieldName: string,
  renderer: FieldRenderer
): void {
  fieldRegistry.set(`${adaptorName}/${fieldName}`, renderer);
}

export function lookupFieldRenderer(
  adaptorName: string,
  fieldName: string
): FieldRenderer | undefined {
  return fieldRegistry.get(`${adaptorName}/${fieldName}`);
}

// clearFieldRegistry is test-only. Production adaptors register on
// their module's top-level load and never unregister.
export function clearFieldRegistry(): void {
  fieldRegistry.clear();
}
