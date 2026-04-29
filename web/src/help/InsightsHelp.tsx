// InsightsHelp — help content for the /insights route (gm-root.22.3).

import { Link } from 'react-router-dom';

export function RouteHelp() {
  return (
    <div
      data-testid="help-insights"
      className="p-4 space-y-4 text-sm text-neutral-700 dark:text-neutral-300"
    >
      <section>
        <h2 className="font-semibold text-neutral-900 dark:text-neutral-100 mb-1">Insights</h2>
        <p>
          Insights surfaces analytics about your project — work-item throughput,
          agent activity, persona consult activity, and drift between desired
          and actual state.
        </p>
      </section>

      <section>
        <h2 className="font-semibold text-neutral-900 dark:text-neutral-100 mb-1">
          What you can do here
        </h2>
        <ul className="space-y-1 list-disc list-inside">
          <li>
            View throughput, completion rate, and WIP trends for your project
          </li>
          <li>
            <Link
              to="/insights/personas"
              className="underline hover:text-neutral-900 dark:hover:text-neutral-100"
            >
              Persona consult activity
            </Link>{' '}
            — see which personas have been consulted and what they recommended
          </li>
          <li>
            <Link
              to="/drift"
              className="underline hover:text-neutral-900 dark:hover:text-neutral-100"
            >
              Drift dashboard
            </Link>{' '}
            — desired-vs-actual state
          </li>
          <li>
            <Link
              to="/board"
              className="underline hover:text-neutral-900 dark:hover:text-neutral-100"
            >
              Return to the Plan board
            </Link>
          </li>
        </ul>
      </section>

      <section>
        <h2 className="font-semibold text-neutral-900 dark:text-neutral-100 mb-1">Learn more</h2>
        <ul className="space-y-1 list-disc list-inside">
          <li>
            <a
              href="https://gembacore.github.io/gemba-core/ui-spec/"
              target="_blank"
              rel="noreferrer"
              className="underline hover:text-neutral-900 dark:hover:text-neutral-100"
            >
              UI spec — Insights surface
            </a>
          </li>
          <li>
            <a
              href="https://gembacore.github.io/gemba-core/design/"
              target="_blank"
              rel="noreferrer"
              className="underline hover:text-neutral-900 dark:hover:text-neutral-100"
            >
              Design docs — persona PPPP model
            </a>
          </li>
        </ul>
      </section>
    </div>
  );
}
