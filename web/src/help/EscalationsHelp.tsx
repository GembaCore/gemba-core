// EscalationsHelp — help content for the /escalations route (gm-root.22.3).

import { Link } from 'react-router-dom';

export function RouteHelp() {
  return (
    <div
      data-testid="help-escalations"
      className="p-4 space-y-4 text-sm text-neutral-700 dark:text-neutral-300"
    >
      <section>
        <h2 className="font-semibold text-neutral-900 dark:text-neutral-100 mb-1">Escalations</h2>
        <p>
          The Escalations inbox collects issues raised by agents that require
          operator attention — blockers, ambiguities, and decisions that AI
          cannot resolve autonomously.
        </p>
      </section>

      <section>
        <h2 className="font-semibold text-neutral-900 dark:text-neutral-100 mb-1">
          What you can do here
        </h2>
        <ul className="space-y-1 list-disc list-inside">
          <li>
            Select one or more escalations using the checkboxes, then use the
            bulk triage toolbar to dismiss or move them to a Gemba walk
          </li>
          <li>
            Use the <strong>Hand off</strong> secondary action to delegate an
            escalation to a persona for a consult
          </li>
          <li>
            Filter by kind, severity, or agent using the filter pill row
          </li>
          <li>
            Use the free-text search to find escalations by title or prompt
          </li>
          <li>
            <Link
              to="/walk"
              className="underline hover:text-neutral-900 dark:hover:text-neutral-100"
            >
              Move escalations to a Gemba walk for structured review
            </Link>
          </li>
          <li>
            <Link
              to="/sessions"
              className="underline hover:text-neutral-900 dark:hover:text-neutral-100"
            >
              View the agent session that raised an escalation
            </Link>
          </li>
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
              Agent operating docs — how agents escalate
            </a>
          </li>
          <li>
            <a
              href="https://gembacore.github.io/gemba-core/ui-spec/"
              target="_blank"
              rel="noreferrer"
              className="underline hover:text-neutral-900 dark:hover:text-neutral-100"
            >
              UI spec — Escalations inbox
            </a>
          </li>
        </ul>
      </section>
    </div>
  );
}
