import { Navigate, Route, Routes } from 'react-router-dom';
import { AppShell } from '@/layouts/AppShell';
import { BoardPage } from '@/pages/BoardPage';
import { SessionsPage } from '@/pages/SessionsPage';
import { GraphPage } from '@/pages/GraphPage';
import CapabilitiesPage from '@/pages/CapabilitiesPage';
import SettingsPage from '@/pages/SettingsPage';
import { CoachPage } from '@/pages/CoachPage';
import { WalkPage } from '@/pages/WalkPage';
import { WalkDetailPage } from '@/pages/WalkDetailPage';
import { ProjectConfigPage } from '@/pages/ProjectConfigPage';
import { BootstrapPage } from '@/pages/BootstrapPage';
import { InsightsPersonasPage } from '@/pages/InsightsPersonasPage';
import { DriftPage } from '@/pages/DriftPage';
// gm-e12.15: provider-aware agent detail view (Workspace.kind switch).
import { AgentDetailPage } from '@/pages/agents/AgentDetailPage';
// gm-e12.8: AgentGroup board view (mode: static | pool | graph).
import { AgentGroupsPage } from '@/pages/agent-groups/AgentGroupsPage';
// gm-e11.5: sprint roster + detail pages (token-budget surface deferred).
import { SprintsPage } from '@/pages/SprintsPage';
import { SprintDetailPage } from '@/pages/SprintDetailPage';
import {
  EscalationsPage,
  HealthPage,
  InsightsPage,
  MailPage,
  NotFoundPage,
} from '@/pages/placeholders';
import { features } from '@/lib/features';

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
        {/* /backlog folded into Board (gm-e12.19.1). Permanent
            redirect so existing links and bookmarks resolve to the
            Board's list mode + Backlog preset. */}
        <Route
          path="/backlog"
          element={<Navigate to="/board?layout=list&view=backlog" replace />}
        />
        {/* /grid folded into /board's list layout + power mode
            (gm-uipx.17). Permanent redirect so existing bookmarks
            land on the equivalent power-list URL. */}
        <Route
          path="/grid"
          element={<Navigate to="/board?layout=list&power=1" replace />}
        />
        <Route path="/sessions" element={<SessionsPage />} />
        {/* gm-e12.15: provider-aware agent detail. :id is matched
            against session.id, agent.id, or assignment_id — see
            AgentDetailPage.findContext for the lookup order. */}
        <Route path="/agents/:id" element={<AgentDetailPage />} />
        {/* gm-e12.8: AgentGroup board (mode-dispatched: static | pool
            | graph). Empty when no orchestration plane is bound. */}
        <Route path="/agent-groups" element={<AgentGroupsPage />} />
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
        <Route path="/escalations" element={<EscalationsPage />} />
        <Route path="/capabilities" element={<CapabilitiesPage />} />
        <Route path="/settings" element={<SettingsPage />} />
        {/* gm-e12.13: desired-vs-actual drift dashboard. */}
        <Route path="/drift" element={<DriftPage />} />
        <Route path="/coach" element={<CoachPage />} />
        <Route path="/walk" element={<WalkPage />} />
        <Route path="/walks/:id" element={<WalkDetailPage />} />
        <Route path="/project/config" element={<ProjectConfigPage />} />
        {/* gm-uipx.7: /bootstrap 4-step wizard. Each step has its own
            slug so back/forward navigation works and tests can land
            on a specific step. Bare /bootstrap redirects to Step 1. */}
        <Route path="/bootstrap" element={<Navigate to="/bootstrap/source" replace />} />
        <Route path="/bootstrap/:step" element={<BootstrapPage />} />
        {features.mail && <Route path="/mail" element={<MailPage />} />}
        <Route path="/health" element={<HealthPage />} />
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  );
}
