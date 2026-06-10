import React, { useEffect } from 'react';
import ReactDOM from 'react-dom/client';
import { QueryClient, QueryClientProvider, useQueryClient } from '@tanstack/react-query';
import { BrowserRouter } from 'react-router-dom';
import App from './App';
import { ThemeProvider } from '@/lib/theme';
import { CapabilitiesProvider } from '@/capabilities';
import { AuthGate } from '@/auth/AuthGate';
import { HotkeysProvider } from '@/hotkeys';
import { startSSE } from '@/data/sse';
import './index.css';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // Default: data is fresh for 10s, garbage-collected after 5 min.
      // SSE events from gm-e2.5 will invalidate specific query keys,
      // so most caches don't need aggressive refetch intervals.
      staleTime: 10_000,
      gcTime: 5 * 60_000,
      refetchOnWindowFocus: false,
      retry: 1,
    },
  },
});

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <AuthGate>
          <CapabilitiesProvider>
            <SseBoot />
            <BrowserRouter>
              <HotkeysProvider>
                <App />
              </HotkeysProvider>
            </BrowserRouter>
          </CapabilitiesProvider>
        </AuthGate>
      </ThemeProvider>
    </QueryClientProvider>
  </React.StrictMode>
);

function SseBoot() {
  const qc = useQueryClient();
  useEffect(() => startSSE(qc), [qc]);
  return null;
}
