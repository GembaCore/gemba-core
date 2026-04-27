import { Navigate, Route, Routes } from 'react-router-dom';
import { AppShell } from '@/layouts/AppShell';
import { BoardPage } from '@/pages/BoardPage';
import { SessionsPage } from '@/pages/SessionsPage';
import { GraphPage } from '@/pages/GraphPage';
import CapabilitiesPage from '@/pages/CapabilitiesPage';
import { CoachPage } from '@/pages/CoachPage';
import { WalkPage } from '@/pages/WalkPage';
import { WalkDetailPage } from '@/pages/WalkDetailPage';
import { ProjectConfigPage } from '@/pages/ProjectConfigPage';
import { InsightsPersonasPage } from '@/pages/InsightsPersonasPage';
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
        <Route path="/graph" element={<GraphPage />} />
        <Route path="/insights" element={<InsightsPage />} />
        {/* gm-twp2: persona consult activity. The first concrete
            /insights/* surface; the placeholder /insights stays as a
            landing while other /insights/* tabs land. */}
        <Route path="/insights/personas" element={<InsightsPersonasPage />} />
        <Route path="/escalations" element={<EscalationsPage />} />
        <Route path="/capabilities" element={<CapabilitiesPage />} />
        <Route path="/coach" element={<CoachPage />} />
        <Route path="/walk" element={<WalkPage />} />
        <Route path="/walks/:id" element={<WalkDetailPage />} />
        <Route path="/project/config" element={<ProjectConfigPage />} />
        {features.mail && <Route path="/mail" element={<MailPage />} />}
        <Route path="/health" element={<HealthPage />} />
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  );
}
