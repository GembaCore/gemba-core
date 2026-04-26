import { Navigate, Route, Routes } from 'react-router-dom';
import { AppShell } from '@/layouts/AppShell';
import { BoardPage } from '@/pages/BoardPage';
import { GridPage } from '@/pages/GridPage';
import { SessionsPage } from '@/pages/SessionsPage';
import { AgentsPage } from '@/pages/AgentsPage';
import { GraphPage } from '@/pages/GraphPage';
import CapabilitiesPage from '@/pages/CapabilitiesPage';
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
          element={<Navigate to="/board?view=list&preset=backlog" replace />}
        />
        <Route path="/grid" element={<GridPage />} />
        <Route path="/sessions" element={<SessionsPage />} />
        <Route path="/agents" element={<AgentsPage />} />
        <Route path="/graph" element={<GraphPage />} />
        <Route path="/insights" element={<InsightsPage />} />
        <Route path="/escalations" element={<EscalationsPage />} />
        <Route path="/capabilities" element={<CapabilitiesPage />} />
        {features.mail && <Route path="/mail" element={<MailPage />} />}
        <Route path="/health" element={<HealthPage />} />
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  );
}
