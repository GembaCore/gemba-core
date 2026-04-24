import React from 'react';
import ReactDOM from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter } from 'react-router-dom';
import App from './App';
import { ThemeProvider } from '@/lib/theme';
import { CapabilitiesProvider } from '@/capabilities';
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

// Open the /events SSE consumer once at boot. Connection lifetime is
// the app's lifetime — EventSource auto-reconnects on transient
// disconnects so this is a fire-and-forget call. Returns a cleanup
// closure we keep in module scope so HMR teardown closes the socket.
const stopSSE = startSSE(queryClient);
if (import.meta.hot) {
  import.meta.hot.dispose(() => stopSSE());
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <CapabilitiesProvider>
          <BrowserRouter>
            <HotkeysProvider>
              <App />
            </HotkeysProvider>
          </BrowserRouter>
        </CapabilitiesProvider>
      </ThemeProvider>
    </QueryClientProvider>
  </React.StrictMode>
);
