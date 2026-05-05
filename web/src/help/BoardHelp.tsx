// BoardHelp — help content for the /board route (Plan) (gm-root.22.3).

import { Link } from 'react-router-dom';
import { useCapabilities } from '@/capabilities';

export function RouteHelp() {
  const { beadsOnly } = useCapabilities();

  if (beadsOnly) {
    return <BeadsOnlyBoardHelp />;
  }

  return (
    <div
      data-testid="help-board"
      className="p-4 space-y-4 text-sm text-neutral-700 dark:text-neutral-300"
    >
      <section>
        <h2 className="font-semibold text-neutral-900 dark:text-neutral-100 mb-1">Board</h2>
        <p>
          The Board shows your project's work in Ready, In Progress, and Done.
          Card pills keep the precise state and attention signals visible while agents execute work
          in parallel.
        </p>
      </section>

      <section>
        <h2 className="font-semibold text-neutral-900 dark:text-neutral-100 mb-1">
          Filters and views
        </h2>
        <p>
          Use the Milestone and Epic selectors to narrow the board to a wrapper and its child
          work. The funnel menu keeps board-only controls small: order and the optional Deferred
          lane.
        </p>
        <ul className="mt-2 space-y-1 list-disc list-inside">
          <li>Ready combines next-up and staged work.</li>
          <li>In Progress shows work currently marked started in the WorkPlane.</li>
          <li>Done shows completed work.</li>
          <li>The Deferred lane can be restored when parked work needs to be visible.</li>
          <li>Use Refine for table, hierarchy, and swimlane planning views.</li>
        </ul>
      </section>

      <section>
        <h2 className="font-semibold text-neutral-900 dark:text-neutral-100 mb-1">
          What you can do here
        </h2>
        <ul className="space-y-1 list-disc list-inside">
          <li>
            Press <kbd>n</kbd> to create a new work item
          </li>
          <li>Drag a runnable bead, epic, or milestone into In Progress to dispatch work</li>
          <li>Watch card pills for Staged, Triage, Needs input, Review, and Ready signals</li>
          <li>
            Press <kbd>j</kbd> / <kbd>k</kbd> to move focus up and down
          </li>
          <li>
            Press <kbd>o</kbd> to open the detail drawer for the focused item
          </li>
          <li>Use Refine when you need bulk editing, hierarchy, or grouped planning views</li>
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
          <li>
            Press <kbd>?</kbd> to see all keyboard shortcuts
          </li>
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
              UI spec — Board affordances
            </a>
          </li>
        </ul>
      </section>
    </div>
  );
}

function BeadsOnlyBoardHelp() {
  return (
    <div
      data-testid="help-board"
      className="space-y-4 p-4 text-sm text-neutral-700 dark:text-neutral-300"
    >
      <section>
        <h2 className="mb-1 font-semibold text-neutral-900 dark:text-neutral-100">Beads board</h2>
        <p>
          Beads-only mode shows the same status board as full Gemba. It reads Ready, In Progress,
          Done, and Deferred directly from Beads state without requiring an agent orchestrator.
        </p>
      </section>

      <section>
        <h2 className="mb-1 font-semibold text-neutral-900 dark:text-neutral-100">
          Milestones and epics
        </h2>
        <p>
          Milestone and epic beads are wrappers: they collect related child beads so a project can
          be read as larger goals, smaller threads, and concrete work. Put broad releases or phases
          in milestones, use epics for coherent feature areas, and keep individual beads small
          enough to inspect, edit, or later dispatch from full Gemba.
        </p>
      </section>

      <section>
        <h2 className="mb-1 font-semibold text-neutral-900 dark:text-neutral-100">
          When to use Refine
        </h2>
        <p>
          Once you start using milestone and epic wrappers, switch to Refine to read the structure
          as milestone to epic to bead. Refine also has the dense table for scanning, filtering,
          editing, and finding changed beads.
        </p>
      </section>

      <section>
        <h2 className="mb-1 font-semibold text-neutral-900 dark:text-neutral-100">
          Filters and views
        </h2>
        <p>
          Use the Milestone and Epic selectors to focus on a wrapper's children. The funnel menu
          changes sort order and can show the Deferred lane. Refine hosts hierarchy, swimlane, and
          table views when you need planning structure.
        </p>
      </section>

      <section>
        <h2 className="mb-1 font-semibold text-neutral-900 dark:text-neutral-100">
          What you can do here
        </h2>
        <ul className="list-inside list-disc space-y-1">
          <li>Create and edit milestones, epics, beads, and decisions</li>
          <li>Drag cards between state columns when writable; this updates Beads but does not dispatch agents</li>
          <li>Use Refine for milestone to epic to bead hierarchy, grouped swimlanes, and dense table editing</li>
          <li>Open details in the right panel to edit content and tags</li>
          <li>Use the graph view to inspect dependency relationships</li>
          <li>Review Beads history in the right panel for recent changes</li>
        </ul>
      </section>
    </div>
  );
}
