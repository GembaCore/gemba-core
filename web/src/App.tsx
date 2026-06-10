import { Navigate, Route, Routes } from 'react-router-dom';
import type { ReactNode } from 'react';
import { AppShell } from '@/layouts/AppShell';
import { BoardPage } from '@/pages/BoardPage';
import { RefinePage } from '@/pages/RefinePage';
import { SessionsPage } from '@/pages/SessionsPage';
import { GraphPage } from '@/pages/GraphPage';
import CapabilitiesPage from '@/pages/CapabilitiesPage';
import SettingsPage from '@/pages/SettingsPage';
// gm-s47n.16: Pool Dispatch editor — adaptor-aware sub-route off
// /settings rather than a tab on /settings, per the spec's slight
// preference (§11 q1).
import PoolsPage from '@/pages/PoolsPage';
import { CoachPage } from '@/pages/CoachPage';
import { RecentPage } from '@/pages/RecentPage';
import { WalkPage } from '@/pages/WalkPage';
import { WalkDetailPage } from '@/pages/WalkDetailPage';
import { ProjectConfigPage } from '@/pages/ProjectConfigPage';
import { BootstrapPage } from '@/pages/BootstrapPage';
// gm-e12.21.3: /new now redirects to /board and auto-opens the
// unified Create-project modal (the standalone NewProjectPage form
// folded into the modal alongside the adopt path).
import { NewProjectRedirect } from '@/pages/NewProjectRedirect';
import { OnboardPage } from '@/pages/OnboardPage';
import { InsightsPersonasPage } from '@/pages/InsightsPersonasPage';
// gm-e12.17.1: real recharts-driven /insights with three first-viewport tiles.
import { InsightsPage } from '@/pages/InsightsPage';
import { DriftPage } from '@/pages/DriftPage';
// gm-e12.15: provider-aware agent detail view (Workspace.kind switch).
import { AgentDetailPage } from '@/pages/agents/AgentDetailPage';
// gm-e12.8: AgentGroup board view (mode: static | pool | graph).
import { AgentGroupsPage } from '@/pages/agent-groups/AgentGroupsPage';
// gm-e11.5: sprint roster + detail pages (token-budget surface deferred).
import { SprintsPage } from '@/pages/SprintsPage';
import { SprintDetailPage } from '@/pages/SprintDetailPage';
// gm-e11.8.1: real Escalations inbox replaces the placeholder.
import { EscalationsPage } from '@/pages/EscalationsPage';
import {
  HealthPage,
  MailPage,
  NotFoundPage,
} from '@/pages/placeholders';
import { features } from '@/lib/features';
import { useCapabilities } from '@/capabilities';

export default function App() {
  return (
    <Routes>
      <Route element={<AppShell />}>
        <Route index element={<Navigate to="/board" replace />} />
        <Route path="/board" element={<BoardPage />} />
        {/* Wildcard so workspace-prefixed bead ids ("gemba/gemba/gm-e1")
            match end-to-end — :epicId would only catch a single path
            segment and the dolt adaptor canonically prefixes ids with
            "<workspace>/<repo>/". useParams()['*'] surfaces the rest. */}
        <Route path="/board/*" element={<BoardPage />} />
        {/* /refine — dedicated triage + refinement surface (gm-3ofd).
            Reclaims dense planning modes: deferred table, hierarchy,
            and swimlanes. Existing /backlog links land here. */}
        <Route path="/refine" element={<RefinePage />} />
        <Route path="/backlog" element={<Navigate to="/refine" replace />} />
        {/* /grid now folds into Refine's table mode. */}
        <Route
          path="/grid"
          element={<Navigate to="/refine" replace />}
        />
        <Route path="/sessions" element={<UnavailableInBeadsOnly><SessionsPage /></UnavailableInBeadsOnly>} />
        {/* gm-e12.15: provider-aware agent detail. :id is matched
            against session.id, agent.id, or assignment_id — see
            AgentDetailPage.findContext for the lookup order. */}
        <Route path="/agents/:id" element={<UnavailableInBeadsOnly><AgentDetailPage /></UnavailableInBeadsOnly>} />
        {/* gm-e12.8: AgentGroup board (mode-dispatched: static | pool
            | graph). Empty when no orchestration plane is bound. */}
        <Route path="/agent-groups" element={<UnavailableInBeadsOnly><AgentGroupsPage /></UnavailableInBeadsOnly>} />
        {/* gm-e11.5: sprint roster + per-sprint detail. Token-budget
            gauges + enforcement deferred to a follow-up under gm-root.14. */}
        <Route path="/sprints" element={<SprintsPage />} />
        <Route path="/sprints/:id" element={<SprintDetailPage />} />
        <Route path="/graph" element={<GraphPage />} />
        <Route path="/insights" element={<InsightsPage />} />
        {/* gm-twp2: persona consult activity. The first concrete
            /insights/* surface; the placeholder /insights stays as a
            landing while other /insights/* tabs land. */}
        <Route path="/insights/personas" element={<InsightsPersonasPage />} />
        <Route path="/escalations" element={<UnavailableInBeadsOnly><EscalationsPage /></UnavailableInBeadsOnly>} />
        <Route path="/capabilities" element={<CapabilitiesPage />} />
        <Route path="/settings" element={<SettingsPage />} />
        <Route path="/settings/pools" element={<PoolsPage />} />
        {/* gm-e12.13: desired-vs-actual drift dashboard. */}
        <Route path="/drift" element={<UnavailableInBeadsOnly><DriftPage /></UnavailableInBeadsOnly>} />
        <Route path="/coach" element={<UnavailableInBeadsOnly><CoachPage /></UnavailableInBeadsOnly>} />
        <Route path="/walk" element={<UnavailableInBeadsOnly><WalkPage /></UnavailableInBeadsOnly>} />
        {/* gm-g5xz.2: /recent — operator-facing view of beads created
            in the selected time window (default 24h). Backed by
            ?created_since= on GET /api/work-items. Watermark is
            per-browser localStorage; no per-bead reviewed state. */}
        <Route path="/recent" element={<RecentPage />} />
        <Route path="/walks/:id" element={<UnavailableInBeadsOnly><WalkDetailPage /></UnavailableInBeadsOnly>} />
        <Route path="/project/config" element={<ProjectConfigPage />} />
        {/* gm-uipx.7: /bootstrap 4-step wizard. Each step has its own
            slug so back/forward navigation works and tests can land
            on a specific step. Bare /bootstrap redirects to Step 1. */}
        <Route path="/bootstrap" element={<Navigate to="/bootstrap/source" replace />} />
        <Route path="/bootstrap/:step" element={<BootstrapPage />} />
        {/* gm-e12.21.3: /new opens the unified Create-project modal
            on top of /board. The standalone /new page folded into the
            modal alongside the adopt path so there's one entry point
            for every (DB, Repo) combination. */}
        <Route path="/new" element={<NewProjectRedirect />} />
        {/* gm-root.17.13: /onboard — the conversational planner the
            old /new hosted (gm-root.17.3). Reachable from the board's
            empty-state CTA when an LLM client is configured, or by
            direct navigation. Two-pane (conversation + plan preview)
            layout with a persistent Ratify button. One-shot session —
            refresh discards. See docs/design/newproject.md. */}
        <Route path="/onboard" element={<OnboardPage />} />
        {features.mail && <Route path="/mail" element={<MailPage />} />}
        <Route path="/health" element={<HealthPage />} />
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  );
}

function UnavailableInBeadsOnly({ children }: { children: ReactNode }) {
  const { beadsOnly } = useCapabilities();
  if (!beadsOnly) return <>{children}</>;
  return (
    <div
      data-testid="beads-only-unavailable"
      className="flex h-full flex-col items-center justify-center gap-2 p-8 text-center"
    >
      <h1 className="text-sm font-semibold text-neutral-900 dark:text-neutral-100">
        Hidden in Beads-only mode
      </h1>
      <p className="max-w-md text-xs text-neutral-500 dark:text-neutral-400">
        This surface depends on orchestration or live sessions. Board, Refine, detail tabs,
        and Graph remain available for Beads viewing and management.
      </p>
    </div>
  );
}
