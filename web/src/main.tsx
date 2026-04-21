import React from 'react';
import ReactDOM from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter } from 'react-router-dom';
import App from './App';
import { ThemeProvider } from '@/lib/theme';
import { CapabilitiesProvider } from '@/capabilities';
import { HotkeysProvider } from '@/hotkeys';
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
