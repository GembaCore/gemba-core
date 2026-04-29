// BoardHelp — help content for the /board route (Plan) (gm-root.22.3).

import { Link } from 'react-router-dom';

export function RouteHelp() {
  return (
    <div
      data-testid="help-board"
      className="p-4 space-y-4 text-sm text-neutral-700 dark:text-neutral-300"
    >
      <section>
        <h2 className="font-semibold text-neutral-900 dark:text-neutral-100 mb-1">Plan board</h2>
        <p>
          The Plan board shows your project's epics and work items organised by
          status. Dispatch AI agents from here to execute work in parallel.
        </p>
      </section>

      <section>
        <h2 className="font-semibold text-neutral-900 dark:text-neutral-100 mb-1">
          What you can do here
        </h2>
        <ul className="space-y-1 list-disc list-inside">
          <li>Press <kbd>n</kbd> to create a new work item</li>
          <li>Press <kbd>j</kbd> / <kbd>k</kbd> to move focus up and down</li>
          <li>Press <kbd>o</kbd> to open the detail drawer for the focused item</li>
          <li>
            Press <kbd>Mod+W</kbd> to toggle between Epic-primary and
            Work-item-flat views
          </li>
          <li>
            Press <kbd>Mod+Shift+L</kbd> to toggle between Kanban and List
            layouts
          </li>
          <li>
            <Link
              to="/walk"
              className="underline hover:text-neutral-900 dark:hover:text-neutral-100"
            >
              Start a Gemba walk
            </Link>{' '}
            to review work in progress
          </li>
          <li>
            <Link
              to="/sessions"
              className="underline hover:text-neutral-900 dark:hover:text-neutral-100"
            >
              View active agent sessions
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
              href="https://gembacore.github.io/gemba-core/getting-started/running-against-your-work-items/"
              target="_blank"
              rel="noreferrer"
              className="underline hover:text-neutral-900 dark:hover:text-neutral-100"
            >
              Running Gemba against your work items
            </a>
          </li>
          <li>
            <a
              href="https://gembacore.github.io/gemba-core/concepts/dispatch-vs-planning/"
              target="_blank"
              rel="noreferrer"
              className="underline hover:text-neutral-900 dark:hover:text-neutral-100"
            >
              Dispatch vs Planning
            </a>
          </li>
          <li>
            <a
              href="https://gembacore.github.io/gemba-core/getting-started/parallelism/"
              target="_blank"
              rel="noreferrer"
              className="underline hover:text-neutral-900 dark:hover:text-neutral-100"
            >
              Parallelism guide
            </a>
          </li>
          <li>
            <a
              href="https://gembacore.github.io/gemba-core/ui-spec/"
              target="_blank"
              rel="noreferrer"
              className="underline hover:text-neutral-900 dark:hover:text-neutral-100"
            >
              UI spec — Plan board affordances
            </a>
          </li>
        </ul>
      </section>
    </div>
  );
}
