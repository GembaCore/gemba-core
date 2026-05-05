// GraphHelp — help content for the /graph route.

import { Link } from 'react-router-dom';

export function RouteHelp() {
  return (
    <div
      data-testid="help-graph"
      className="space-y-4 p-4 text-sm text-neutral-700 dark:text-neutral-300"
    >
      <section>
        <h2 className="mb-1 font-semibold text-neutral-900 dark:text-neutral-100">Graph</h2>
        <p>
          The Graph view shows dependency and wrapper relationships across visible work. Click a
          node to focus it and open its detail in the right panel.
        </p>
      </section>

      <section>
        <h2 className="mb-1 font-semibold text-neutral-900 dark:text-neutral-100">
          Filters
        </h2>
        <p>
          The funnel menu filters the topology immediately. Use it to narrow by milestone, epic
          scope, title or id search, state, and kind. Clearing filters restores the full graph.
        </p>
      </section>

      <section>
        <h2 className="mb-1 font-semibold text-neutral-900 dark:text-neutral-100">
          Graph controls
        </h2>
        <ul className="list-inside list-disc space-y-1">
          <li>Items shows one node per bead; Epics rolls child work up to epic nodes.</li>
          <li>Auto switches to epic aggregation when the graph is too dense to read.</li>
          <li>Cycles highlights dependency loops that can block progress.</li>
          <li>Critical path highlights the longest dependency chain in the visible graph.</li>
          <li>Up and Down step through a focused node's dependency path when there is a clear next node.</li>
          <li>The minimap and zoom controls help navigate large workspaces.</li>
        </ul>
      </section>

      <section>
        <h2 className="mb-1 font-semibold text-neutral-900 dark:text-neutral-100">
          Edges
        </h2>
        <ul className="list-inside list-disc space-y-1">
          <li>blocks shows dependency ordering.</li>
          <li>parent_child shows milestone, epic, and bead hierarchy.</li>
          <li>relates_to shows non-ordering relationships.</li>
          <li>Adaptor-declared extension edges appear when the work plane exposes them.</li>
        </ul>
      </section>

      <section>
        <h2 className="mb-1 font-semibold text-neutral-900 dark:text-neutral-100">Related</h2>
        <p>
          Use the{' '}
          <Link to="/board" className="underline hover:text-neutral-900 dark:hover:text-neutral-100">
            Plan board
          </Link>{' '}
          to edit, dispatch, and triage the same beads.
        </p>
      </section>
    </div>
  );
}
