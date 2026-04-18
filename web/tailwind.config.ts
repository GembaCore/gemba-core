import type { Config } from 'tailwindcss';

// Keep this config minimal; shadcn/ui initialization will extend it.
export default {
  darkMode: ['class'],
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      fontFamily: {
        // Intentionally not picking a font here; the frontend polecat
        // gets to make that call as part of gm-e4.1 (app shell). See
        // the frontend-design skill for taste guidance.
      },
    },
  },
  plugins: [],
} satisfies Config;
