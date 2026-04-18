import type { Config } from 'tailwindcss';
import animate from 'tailwindcss-animate';

// Minimal scaffold config. The app-shell polecat (gm-e4.1) extends this
// with shadcn/ui design tokens. tailwindcss-animate ships with shadcn so
// component primitives (Dialog, DropdownMenu, …) animate correctly.
export default {
  darkMode: ['class'],
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {},
  },
  plugins: [animate],
} satisfies Config;
