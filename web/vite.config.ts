/// <reference types="vitest" />
import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import path from 'node:path';

// Vite config for Gemba. In dev mode, the frontend runs on :5173
// and proxies API + SSE requests back to the Go binary on :7666 (or
// wherever BC_BACKEND points).
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      '/api': {
        target: process.env.BC_BACKEND ?? 'http://localhost:7666',
        changeOrigin: true,
      },
      '/events': {
        target: process.env.BC_BACKEND ?? 'http://localhost:7666',
        changeOrigin: true,
        // SSE needs an open connection; don't buffer.
        ws: false,
      },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    // Small sourcemaps shipped in the binary are fine for a dev tool;
    // flip to false for shipping if binary size becomes a concern.
    sourcemap: true,
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./vitest.setup.ts'],
  },
});
