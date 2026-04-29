// ColdStartHelp — help content shown when no project is active (gm-root.22.3).
//
// Displayed by HelpTab whenever useProjectPicker().activeProject is null
// and the picker is not still loading. Does NOT mention any
// workspace-scoped surfaces — the operator has no workspace yet.

import { Link } from 'react-router-dom';

export function RouteHelp() {
  return (
    <div
      data-testid="help-cold-start"
      className="p-4 space-y-4 text-sm text-neutral-700 dark:text-neutral-300"
    >
      <section>
        <h2 className="font-semibold text-neutral-900 dark:text-neutral-100 mb-1">
          Welcome to Gemba
        </h2>
        <p>
          You don't have an active project yet. Create or open a project to
          start planning and running AI-assisted work.
        </p>
      </section>

      <section>
        <h2 className="font-semibold text-neutral-900 dark:text-neutral-100 mb-1">
          What you can do here
        </h2>
        <ul className="space-y-1 list-disc list-inside">
          <li>
            Use the <strong>+</strong> button in the top bar to create a new
            project or open an existing one.
          </li>
          <li>
            <Link
              to="/settings"
              className="underline hover:text-neutral-900 dark:hover:text-neutral-100"
            >
              Configure settings
            </Link>{' '}
            — adaptor credentials, LLM keys, and workspace preferences are
            available without a project.
          </li>
        </ul>
      </section>

      <section>
        <h2 className="font-semibold text-neutral-900 dark:text-neutral-100 mb-1">
          Learn more
        </h2>
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
              href="https://gembacore.github.io/gemba-core/adaptors/"
              target="_blank"
              rel="noreferrer"
              className="underline hover:text-neutral-900 dark:hover:text-neutral-100"
            >
              Adaptors — connecting Gemba to your work plane
            </a>
          </li>
        </ul>
      </section>
    </div>
  );
}
