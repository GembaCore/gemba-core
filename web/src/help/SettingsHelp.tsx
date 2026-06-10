// SettingsHelp — help content for the /settings route (gm-root.22.3).

import { Link } from 'react-router-dom';

export function RouteHelp() {
  return (
    <div
      data-testid="help-settings"
      className="p-4 space-y-4 text-sm text-neutral-700 dark:text-neutral-300"
    >
      <section>
        <h2 className="font-semibold text-neutral-900 dark:text-neutral-100 mb-1">Settings</h2>
        <p>
          Configure adaptor credentials, LLM client settings, workspace
          preferences, and project-level options. Settings are available even
          before a project is active.
        </p>
      </section>

      <section>
        <h2 className="font-semibold text-neutral-900 dark:text-neutral-100 mb-1">
          What you can do here
        </h2>
        <ul className="space-y-1 list-disc list-inside">
          <li>
            Connect or reconfigure your work plane adaptor (Linear, GitHub
            Issues, Jira, etc.)
          </li>
          <li>Set your LLM client — API key, model, and provider</li>
          <li>Configure parallelism budget and concurrency limits</li>
          <li>
            <Link
              to="/capabilities"
              className="underline hover:text-neutral-900 dark:hover:text-neutral-100"
            >
              Browse capabilities
            </Link>{' '}
            to see which skills and adaptors are available in your environment
          </li>
          <li>
            <Link
              to="/board"
              className="underline hover:text-neutral-900 dark:hover:text-neutral-100"
            >
              Return to the Board
            </Link>
          </li>
        </ul>
      </section>

      <section>
        <h2 className="font-semibold text-neutral-900 dark:text-neutral-100 mb-1">Learn more</h2>
        <ul className="space-y-1 list-disc list-inside">
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
          <li>
            <a
              href="https://gembacore.github.io/gemba-core/adaptors/workplane/"
              target="_blank"
              rel="noreferrer"
              className="underline hover:text-neutral-900 dark:hover:text-neutral-100"
            >
              WorkPlane authoring guide
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
        </ul>
      </section>
    </div>
  );
}
