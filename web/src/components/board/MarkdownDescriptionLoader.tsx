// Suspense-wrapped lazy import of the real MarkdownDescription chunk.
// Kept in its own file so descriptionRenderers.tsx (the registry) can
// stay free of component declarations and dodge the
// react-refresh/only-export-components lint rule.

import { lazy, Suspense } from 'react';
import type { DescriptionRendererProps } from './descriptionRenderers';

const MarkdownDescriptionInner = lazy(() => import('./MarkdownDescription'));

export function MarkdownDescriptionLoader(props: DescriptionRendererProps) {
  return (
    <Suspense
      fallback={
        <div className="text-xs text-neutral-500" data-testid="description-markdown-loading">
          rendering…
        </div>
      }
    >
      <MarkdownDescriptionInner {...props} />
    </Suspense>
  );
}
