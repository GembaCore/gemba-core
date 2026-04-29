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
// gm-root.18: project picker context — provides the project list and
// active-workspace state to the Topbar picker and any future consumers
// (e.g. gm-root.17.7 start-planning handoff).
import { ProjectPickerProvider } from '@/components/projectpicker/ProjectPickerContext';
// gm-e12.21.3: unified Create-project modal lives at the AppShell
// level so the topbar '+' button, the picker dropdown, and the /new
// redirect all open the same instance.
import { CreateProjectModalProvider } from '@/components/projects/CreateProjectModalContext';

export function AppShell() {
  return (
    <PaletteProvider>
      <PmPanelProvider>
        <ProjectPickerProvider>
        <CreateProjectModalProvider>
        <WalkProvider>
          <div className="flex h-screen w-screen bg-white text-neutral-900 dark:bg-neutral-950 dark:text-neutral-100">
            <Sidebar />
            <div className="flex min-w-0 flex-1 flex-col">
              <Topbar />
              <AdaptorBanner />
              <ActiveWalkBanner />
              {/* tabIndex={-1} keeps the scrollable <main> keyboard-
                  accessible (axe scrollable-region-focusable, gm-root.17.12)
                  without dropping a stray tab stop into the natural focus
                  order — the route's content owns the focus order, not the
                  outer scroll container. -1 satisfies the rule because axe
                  accepts any focusable region; programmatic focus + native
                  arrow-key scroll still work. */}
              <main className="min-h-0 flex-1 overflow-auto" tabIndex={-1}>
                <Outlet />
              </main>
            </div>
            <AppHotkeys />
            <Palette />
            <PmPanel />
            <AppWalkBindings />
          </div>
        </WalkProvider>
        </CreateProjectModalProvider>
        </ProjectPickerProvider>
      </PmPanelProvider>
    </PaletteProvider>
  );
}
