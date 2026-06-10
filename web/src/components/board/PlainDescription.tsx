// PlainDescription — whitespace-preserved prose renderer (the default
// fallback in the description registry). Lives in its own file so the
// registry module can export helpers without tripping the
// react-refresh/only-export-components lint.

import type { DescriptionRendererProps } from './descriptionRenderers';

export function PlainDescription({ source }: DescriptionRendererProps) {
  return (
    <pre
      className="whitespace-pre-wrap break-words font-sans text-sm text-neutral-800 dark:text-neutral-200"
      data-testid="description-plain"
    >
      {source}
    </pre>
  );
}
