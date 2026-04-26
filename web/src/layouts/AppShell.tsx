import { Outlet } from 'react-router-dom';
import { AdaptorBanner } from '@/components/AdaptorBanner';
import { Sidebar } from '@/components/Sidebar';
import { Topbar } from '@/components/Topbar';
import { AppHotkeys } from '@/hotkeys/AppHotkeys';
import { PaletteProvider } from '@/components/palette/PaletteContext';
import { Palette } from '@/components/palette/Palette';
import { PmPanel } from '@/components/pm/PmPanel';
import { PmPanelProvider } from '@/components/pm/PmPanelContext';
import { ActiveWalkBanner } from '@/components/walk/ActiveWalkBanner';
import { AppWalkBindings } from '@/components/walk/AppWalkBindings';
import { WalkProvider } from '@/components/walk/WalkContext';

export function AppShell() {
  return (
    <PaletteProvider>
      <PmPanelProvider>
        <WalkProvider>
          <div className="flex h-screen w-screen bg-white text-neutral-900 dark:bg-neutral-950 dark:text-neutral-100">
            <Sidebar />
            <div className="flex min-w-0 flex-1 flex-col">
              <Topbar />
              <AdaptorBanner />
              <ActiveWalkBanner />
              <main className="min-h-0 flex-1 overflow-auto">
                <Outlet />
              </main>
            </div>
            <AppHotkeys />
            <Palette />
            <PmPanel />
            <AppWalkBindings />
          </div>
        </WalkProvider>
      </PmPanelProvider>
    </PaletteProvider>
  );
}
