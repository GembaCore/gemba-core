import type { ReactNode } from 'react';
import { useCapabilities } from './hooks';
import { lookupFieldRenderer, type FieldRendererProps } from './extension-registry';

// FieldExtensions renders every adaptor-declared extension field in
// the active work manifest (gm-root DD-4, gm-e11.4). Each field is
// dispatched to the adaptor-namespaced renderer registered at module
// load time; missing renderers fall back to a defensive text
// representation so a manifest declaration never blows up rendering.

function SafeDefaultFieldRenderer({ field, value }: FieldRendererProps): JSX.Element {
  // The fallback MUST be defensive: extension values come from an
  // adaptor we may not control, so render them as plain text and let
  // the adaptor ship a real renderer when it wants structure. A
  // null/undefined value drops the row rather than leaving a dangling
  // label — the manifest declared the field as *available*, not
  // *always populated*.
  if (value === null || value === undefined || value === '') {
    return <></>;
  }
  const display = typeof value === 'object' ? JSON.stringify(value) : String(value);
  return (
    <div className="text-sm" data-testid={`field-ext-${field.name}`}>
      <span className="text-neutral-500">{field.name}:</span>{' '}
      <span className="text-neutral-900 dark:text-neutral-100">{display}</span>
    </div>
  );
}

export function FieldExtensions({ custom }: { custom?: Record<string, unknown> }): ReactNode {
  const { manifests } = useCapabilities();
  const work = manifests.work;
  if (!work || !work.field_extensions || work.field_extensions.length === 0) {
    return null;
  }

  const adaptor = work.adaptor_name;
  const values = custom ?? {};

  return (
    <div className="flex flex-col gap-1" data-testid="field-extensions">
      {work.field_extensions.map((f) => {
        const Renderer = lookupFieldRenderer(adaptor, f.name) ?? SafeDefaultFieldRenderer;
        return <Renderer key={f.name} field={f} value={values[f.name]} />;
      })}
    </div>
  );
}
