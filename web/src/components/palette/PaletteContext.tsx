// Palette open-state context (gm-e12.6).
//
// The palette has three concurrent openers — the Topbar pill (click),
// the 'p' hotkey, and Mod+K — and one closer (the palette itself when
// the user picks a command or hits Escape). A tiny context is the
// path of least friction: AppShell mounts the provider, Topbar and
// AppHotkeys consume the setter, and the Palette component reads the
// flag.
//
// Kept in its own file so the Provider component and the hook can
// live next to each other without tripping the eslint react-refresh
// rule we hit elsewhere in the SPA.

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react';

interface PaletteState {
  open: boolean;
  setOpen: (next: boolean) => void;
  toggle: () => void;
}

const PaletteContext = createContext<PaletteState | null>(null);

export function PaletteProvider({ children }: { children: ReactNode }) {
  const [open, setOpen] = useState(false);
  const toggle = useCallback(() => setOpen((v) => !v), []);

  // Mod+K (Cmd-K on macOS, Ctrl-K elsewhere) is the OS-conventional
  // command-palette key — Topbar advertises ⌘K next to the search
  // pill. Listening at the provider level (not inside the dialog
  // component) means the keystroke works whether or not the dialog
  // is currently mounted, and stays parallel to the existing 'p'
  // hotkey from gm-7hj.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const isMod = e.metaKey || e.ctrlKey;
      if (isMod && (e.key === 'k' || e.key === 'K')) {
        e.preventDefault();
        setOpen((v) => !v);
      }
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, []);

  const value = useMemo<PaletteState>(() => ({ open, setOpen, toggle }), [open, toggle]);
  return <PaletteContext.Provider value={value}>{children}</PaletteContext.Provider>;
}

export function usePalette(): PaletteState {
  const ctx = useContext(PaletteContext);
  if (!ctx) {
    // Programmer error — same posture as useCapabilities. Throw so a
    // missing provider surfaces under test rather than at the user's
    // first Cmd+K.
    throw new Error('usePalette: no PaletteProvider in tree');
  }
  return ctx;
}
