// RefineHelp — help content for the /refine route.

export function RouteHelp() {
  return (
    <div
      data-testid="help-refine"
      className="space-y-4 p-4 text-sm text-neutral-700 dark:text-neutral-300"
    >
      <section>
        <h2 className="mb-1 font-semibold text-neutral-900 dark:text-neutral-100">Refine</h2>
        <p>
          Refine is where planning structure and dense editing live. Use it before or between
          execution passes to clean up Deferred work, inspect hierarchy, and group work by wrapper.
        </p>
      </section>

      <section>
        <h2 className="mb-1 font-semibold text-neutral-900 dark:text-neutral-100">Views</h2>
        <ul className="list-inside list-disc space-y-1">
          <li>Table shows Deferred work with dense columns, bulk edit, and planning fields.</li>
          <li>Hierarchy reads milestone to epic to bead structure.</li>
          <li>Swimlanes groups work by wrapper while preserving board state columns.</li>
        </ul>
      </section>

      <section>
        <h2 className="mb-1 font-semibold text-neutral-900 dark:text-neutral-100">
          What you can do here
        </h2>
        <ul className="list-inside list-disc space-y-1">
          <li>Search by title, id, or kind depending on the active view</li>
          <li>Bulk edit Deferred beads in Table</li>
          <li>Assign work to epics and defer or dismiss parked work</li>
          <li>Open detail tabs without losing the current planning view</li>
        </ul>
      </section>
    </div>
  );
}
