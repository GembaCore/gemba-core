// WalkHelp — help content for the /walk route (Review) (gm-root.22.3).

import { Link } from 'react-router-dom';

export function RouteHelp() {
  return (
    <div
      data-testid="help-walk"
      className="p-4 space-y-4 text-sm text-neutral-700 dark:text-neutral-300"
    >
      <section>
        <h2 className="font-semibold text-neutral-900 dark:text-neutral-100 mb-1">
          Gemba walk — Review
        </h2>
        <p>
          A Gemba walk is a structured review of work in progress. Gemba opens
          each item in the agenda and surfaces AI-suggested actions; you
          ratify, modify, reject, or defer each one.
        </p>
      </section>

      <section>
        <h2 className="font-semibold text-neutral-900 dark:text-neutral-100 mb-1">
          What you can do here
        </h2>
        <ul className="space-y-1 list-disc list-inside">
          <li>Press <kbd>r</kbd> to ratify the active agenda item</li>
          <li>Press <kbd>m</kbd> to modify the active agenda item</li>
          <li>Press <kbd>x</kbd> to reject the active agenda item</li>
          <li>Press <kbd>d</kbd> to defer the active agenda item</li>
          <li>Press <kbd>n</kbd> to move to the next agenda item</li>
          <li>Press <kbd>Enter</kbd> to apply the focused suggested action</li>
          <li>
            <Link
              to="/board"
              className="underline hover:text-neutral-900 dark:hover:text-neutral-100"
            >
              Return to the Plan board
            </Link>
          </li>
          <li>
            <Link
              to="/escalations"
              className="underline hover:text-neutral-900 dark:hover:text-neutral-100"
            >
              Review escalations raised during this walk
            </Link>
          </li>
        </ul>
      </section>

      <section>
        <h2 className="font-semibold text-neutral-900 dark:text-neutral-100 mb-1">Learn more</h2>
        <ul className="space-y-1 list-disc list-inside">
          <li>
            <a
              href="https://gembacore.github.io/gemba-core/design/"
              target="_blank"
              rel="noreferrer"
              className="underline hover:text-neutral-900 dark:hover:text-neutral-100"
            >
              Gemba walk design — the review ceremony
            </a>
          </li>
          <li>
            <a
              href="https://gembacore.github.io/gemba-core/ui-spec/"
              target="_blank"
              rel="noreferrer"
              className="underline hover:text-neutral-900 dark:hover:text-neutral-100"
            >
              UI spec — Walk surface
            </a>
          </li>
        </ul>
      </section>
    </div>
  );
}
