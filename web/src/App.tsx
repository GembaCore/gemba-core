import { Navigate, Route, Routes } from 'react-router-dom';
import { AppShell } from '@/layouts/AppShell';
import {
  BacklogPage,
  BoardPage,
  EscalationsPage,
  GraphPage,
  HealthPage,
  InsightsPage,
  MailPage,
  NotFoundPage,
} from '@/pages/placeholders';
import { CapabilityBrowser } from '@/pages/CapabilityBrowser';
import { features } from '@/lib/features';

export default function App() {
  return (
    <Routes>
      <Route element={<AppShell />}>
        <Route index element={<Navigate to="/board" replace />} />
        <Route path="/board" element={<BoardPage />} />
        <Route path="/backlog" element={<BacklogPage />} />
        <Route path="/graph" element={<GraphPage />} />
        <Route path="/insights" element={<InsightsPage />} />
        <Route path="/escalations" element={<EscalationsPage />} />
        <Route path="/capabilities" element={<CapabilityBrowser />} />
        {features.mail && <Route path="/mail" element={<MailPage />} />}
        <Route path="/health" element={<HealthPage />} />
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  );
}
