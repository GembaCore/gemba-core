// MarkdownDescription — concrete react-markdown renderer for the
// "markdown" format slot. Lazily imported by the registry
// (descriptionRenderers.tsx) so the ~30 KB gzipped markdown chunk is
// only loaded when an adaptor declares markdown.
//
// GFM is enabled (tables, strikethrough, task lists, autolinked URLs)
// because beads descriptions use it. No HTML-in-markdown; that would
// make sanitization necessary and adaptors don't produce it.

import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import type { DescriptionRendererProps } from './descriptionRenderers';

export default function MarkdownDescription({ source }: DescriptionRendererProps) {
  return (
    <div
      className="prose prose-sm max-w-none text-neutral-800 dark:prose-invert dark:text-neutral-200"
      data-testid="description-markdown"
    >
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        skipHtml
        components={{
          // Tailwind prose styles already do most of the work; these
          // tweaks pull list / table / code-fence styling in line with
          // the rest of the drawer (mono for inline code, compact
          // bullets, borders on tables).
          code(props) {
            const { className, children, ...rest } = props;
            const isInline = !className;
            if (isInline) {
              return (
                <code
                  className="rounded bg-neutral-100 px-1 font-mono text-xs dark:bg-neutral-800"
                  {...rest}
                >
                  {children}
                </code>
              );
            }
            return (
              <code className="font-mono text-xs" {...rest}>
                {children}
              </code>
            );
          },
          pre(props) {
            return (
              <pre
                className="overflow-x-auto rounded bg-neutral-100 p-3 text-xs dark:bg-neutral-900"
                {...props}
              />
            );
          },
          a(props) {
            return <a target="_blank" rel="noreferrer noopener" {...props} />;
          },
        }}
      >
        {source}
      </ReactMarkdown>
    </div>
  );
}
