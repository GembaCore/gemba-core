// SessionsHelp — help content for the /sessions route (Agent Sessions) (gm-root.22.3).

import { Link } from 'react-router-dom';

export function RouteHelp() {
  return (
    <div
      data-testid="help-sessions"
      className="p-4 space-y-4 text-sm text-neutral-700 dark:text-neutral-300"
    >
      <section>
        <h2 className="font-semibold text-neutral-900 dark:text-neutral-100 mb-1">
          Agent Sessions
        </h2>
        <p>
          Agent Sessions shows all active and recent AI agent runs for your
          project. Monitor progress, review outputs, and launch new sessions
          from here.
        </p>
      </section>

      <section>
        <h2 className="font-semibold text-neutral-900 dark:text-neutral-100 mb-1">
          What you can do here
        </h2>
        <ul className="space-y-1 list-disc list-inside">
          <li>
            Press <kbd>Mod+Shift+S</kbd> to start a new session from anywhere
            in the app
          </li>
          <li>
            Click a session card to view its detail — outputs, logs, and
            cost breakdown
          </li>
          <li>
            <Link
              to="/board"
              className="underline hover:text-neutral-900 dark:hover:text-neutral-100"
            >
              Return to the Plan board
            </Link>{' '}
            to dispatch new work items
          </li>
          <li>
            <Link
              to="/escalations"
              className="underline hover:text-neutral-900 dark:hover:text-neutral-100"
            >
              Review escalations raised by sessions
            </Link>
          </li>
          <li>Press <kbd>j</kbd> / <kbd>k</kbd> to navigate session cards</li>
        </ul>
      </section>

      <section>
        <h2 className="font-semibold text-neutral-900 dark:text-neutral-100 mb-1">Learn more</h2>
        <ul className="space-y-1 list-disc list-inside">
          <li>
            <a
              href="https://gembacore.github.io/gemba-core/agents/"
              target="_blank"
              rel="noreferrer"
              className="underline hover:text-neutral-900 dark:hover:text-neutral-100"
            >
              Agent operating docs
            </a>
          </li>
          <li>
            <a
              href="https://gembacore.github.io/gemba-core/getting-started/parallelism/"
              target="_blank"
              rel="noreferrer"
              className="underline hover:text-neutral-900 dark:hover:text-neutral-100"
            >
              Parallelism guide — running multiple sessions
            </a>
          </li>
          <li>
            <a
              href="https://gembacore.github.io/gemba-core/ui-spec/"
              target="_blank"
              rel="noreferrer"
              className="underline hover:text-neutral-900 dark:hover:text-neutral-100"
            >
              UI spec — Sessions surface
            </a>
          </li>
        </ul>
      </section>
    </div>
  );
}
