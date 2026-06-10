// DefaultHelp — fallback help content for unknown routes (gm-root.22.3).
//
// Rendered by HelpTab when the current pathname does not match any known
// route module.

import { Link } from 'react-router-dom';

export function RouteHelp() {
  return (
    <div className="p-4 space-y-4 text-sm text-neutral-700 dark:text-neutral-300">
      <section>
        <h2 className="font-semibold text-neutral-900 dark:text-neutral-100 mb-1">About Gemba</h2>
        <p>
          Gemba is an AI-assisted project management surface. Each route has its own
          contextual help — navigate to a known route to see specific guidance.
        </p>
      </section>

      <section>
        <h2 className="font-semibold text-neutral-900 dark:text-neutral-100 mb-1">What you can do here</h2>
        <ul className="space-y-1 list-disc list-inside">
          <li>
            <Link to="/board" className="underline hover:text-neutral-900 dark:hover:text-neutral-100">
              Go to the Board
            </Link>
          </li>
          <li>
            <Link to="/walk" className="underline hover:text-neutral-900 dark:hover:text-neutral-100">
              Start a Gemba walk (Review)
            </Link>
          </li>
          <li>
            <Link to="/sessions" className="underline hover:text-neutral-900 dark:hover:text-neutral-100">
              View Agent Sessions
            </Link>
          </li>
          <li>
            <Link to="/escalations" className="underline hover:text-neutral-900 dark:hover:text-neutral-100">
              Review Escalations
            </Link>
          </li>
          <li>Press <kbd>?</kbd> to see all keyboard shortcuts</li>
        </ul>
      </section>

      <section>
        <h2 className="font-semibold text-neutral-900 dark:text-neutral-100 mb-1">Learn more</h2>
        <ul className="space-y-1 list-disc list-inside">
          <li>
            <a
              href="https://gembacore.github.io/gemba-core/getting-started/"
              target="_blank"
              rel="noreferrer"
              className="underline hover:text-neutral-900 dark:hover:text-neutral-100"
            >
              Getting Started guide
            </a>
          </li>
          <li>
            <a
              href="https://gembacore.github.io/gemba-core/"
              target="_blank"
              rel="noreferrer"
              className="underline hover:text-neutral-900 dark:hover:text-neutral-100"
            >
              Full docsite
            </a>
          </li>
        </ul>
      </section>
    </div>
  );
}
