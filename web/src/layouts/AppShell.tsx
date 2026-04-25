import { Outlet } from 'react-router-dom';
import { AdaptorBanner } from '@/components/AdaptorBanner';
import { Sidebar } from '@/components/Sidebar';
import { Topbar } from '@/components/Topbar';
import { AppHotkeys } from '@/hotkeys/AppHotkeys';
import { PaletteProvider } from '@/components/palette/PaletteContext';
import { Palette } from '@/components/palette/Palette';

export function AppShell() {
  return (
    <PaletteProvider>
      <div className="flex h-screen w-screen bg-white text-neutral-900 dark:bg-neutral-950 dark:text-neutral-100">
        <Sidebar />
        <div className="flex min-w-0 flex-1 flex-col">
          <Topbar />
          <AdaptorBanner />
          <main className="min-h-0 flex-1 overflow-auto">
            <Outlet />
          </main>
        </div>
        <AppHotkeys />
        <Palette />
      </div>
    </PaletteProvider>
  );
}
