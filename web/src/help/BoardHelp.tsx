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
        <h2 className="font-semibold text-neutral-900 dark:text-neutral-100 mb-1">Plan board</h2>
        <p>
          The Plan board shows your project's epics and work items in Ready, In Progress, and Done.
          Card pills keep the precise state and attention signals visible while agents execute work
          in parallel.
        </p>
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
          <li>
            Press <kbd>Mod+W</kbd> to toggle between Epic-primary and Work-item-flat views
          </li>
          <li>
            Press <kbd>Mod+Shift+L</kbd> to toggle between Kanban and List layouts
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
              UI spec — Plan board affordances
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
          Beads-only mode starts in Flat view so every milestone, epic, and bead is visible in one
          ordered list. Use the Order control to sort by modified time, created time, edited time,
          or ID.
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
          When to use Cascade
        </h2>
        <p>
          Once you start using milestone and epic wrappers, switch to Cascade to read the structure
          as milestone to epic to bead. Cascade is a planning map; Flat remains the quickest way to
          scan, filter, edit, and find recently changed beads.
        </p>
      </section>

      <section>
        <h2 className="mb-1 font-semibold text-neutral-900 dark:text-neutral-100">
          What you can do here
        </h2>
        <ul className="list-inside list-disc space-y-1">
          <li>Create and edit milestones, epics, beads, and decisions</li>
          <li>Use Flat view for the full ordered list</li>
          <li>Use Cascade view for milestone to epic to bead structure</li>
          <li>Open details in the right panel to edit content and tags</li>
          <li>Use the graph view to inspect dependency relationships</li>
          <li>Review Beads history in the right panel for recent changes</li>
        </ul>
      </section>
    </div>
  );
}
