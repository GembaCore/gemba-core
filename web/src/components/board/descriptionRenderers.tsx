// Description renderer registry — the "display adaptor" keyed by the
// WorkPlane's declared CapabilityManifest.description_format. Adding a
// format means (1) declare the string value from the Go side, (2) add an
// entry here. Unknown formats fall through to `plain` so an adaptor
// shipped ahead of the SPA never renders raw syntax as content.
//
// File deliberately contains only helpers + type exports (no component
// declarations) so consumers can import registry utilities without
// tripping react-refresh/only-export-components. Concrete renderers
// live in sibling files (PlainDescription.tsx, MarkdownDescription.tsx,
// MarkdownDescriptionLoader.tsx).

import type { ComponentType } from 'react';
import { PlainDescription } from './PlainDescription';
import { MarkdownDescriptionLoader } from './MarkdownDescriptionLoader';

// Matches the Go constants in internal/core/workplane.go. Keep in sync.
export const DESCRIPTION_FORMATS = ['plain', 'markdown'] as const;
export type DescriptionFormat = (typeof DESCRIPTION_FORMATS)[number];

export interface DescriptionRendererProps {
  source: string;
}

export type DescriptionRenderer = ComponentType<DescriptionRendererProps>;

const REGISTRY: Record<DescriptionFormat, DescriptionRenderer> = {
  plain: PlainDescription,
  markdown: MarkdownDescriptionLoader,
};

// rendererFor returns the renderer for the adaptor-declared format.
// Accepts the raw string off the manifest; unknown / undefined values
// fall through to the plain renderer so a format introduced server-side
// never crashes an older SPA build. The react-refresh lint rule treats
// functions returning a component type as if they were components; this
// is a pure lookup, so the disable is correct.
// eslint-disable-next-line react-refresh/only-export-components
export function rendererFor(format: string | undefined | null): DescriptionRenderer {
  if (format && isKnownFormat(format)) return REGISTRY[format];
  return PlainDescription;
}

// eslint-disable-next-line react-refresh/only-export-components
export function isKnownFormat(v: string): v is DescriptionFormat {
  return (DESCRIPTION_FORMATS as readonly string[]).includes(v);
}
