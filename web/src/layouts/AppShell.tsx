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
// gm-root.22.2: Right-Hand Panel — persistent right-side surface
// hosting the Help tab and unified detail tabs (replacing drawers).
// The shell mounts here; pinned-tab content (Help) and detail-tab
// content land in sibling beads .3 and .4.
import { RhpProvider } from '@/components/rhp/RhpContext';
import { RhpShell } from '@/components/rhp/RhpShell';
// gm-root.22.3: Help pinned tab + pinned-content registry.
// HelpTab registers itself (rail icon) and its body renderer on mount.
// RhpPinnedContentProvider is the lightweight context that bridges the
// two without touching the public RhpAPI.
import { HelpTab } from '@/components/rhp/HelpTab';
import { StatusTab } from '@/components/rhp/StatusTab';
import { BeadsHistoryTab } from '@/components/rhp/BeadsHistoryTab';
import { RhpPinnedContentProvider } from '@/components/rhp/RhpPinnedContent';
// gm-root.22.5: WorkItem detail-tab kind registration. WorkItemDetailRegistration
// registers the 'workitem' kind with the RHP detail-content registry so
// popDetail({kind: 'workitem', id}) renders WorkItemDetail inside the tab.
import { WorkItemDetailRegistration } from '@/components/rhp/details/WorkItemDetailRegistration';
import { InteractionDetailRegistration } from '@/components/rhp/details/InteractionDetail';
// gm-root.26 item 2: minimal in-house toast system. Used by the
// session launcher to surface "session running in pane <id>" with a
// link to /sessions so first-time users discover the surface.
import { ToastProvider } from '@/components/ui/ToastContext';

export function AppShell() {
  return (
    <PaletteProvider>
      <PmPanelProvider>
        <ProjectPickerProvider>
        <CreateProjectModalProvider>
        <WalkProvider>
        <RhpProvider>
        <RhpPinnedContentProvider>
        <ToastProvider>
          <div className="flex h-screen w-screen bg-white text-neutral-900 dark:bg-neutral-950 dark:text-neutral-100">
            <Sidebar />
            <div className="flex min-w-0 flex-1 flex-col">
              <Topbar />
              <AdaptorBanner />
              <ActiveWalkBanner />
              {/* tabIndex={0} keeps the scrollable <main> keyboard-
                  accessible per axe scrollable-region-focusable: keyboard-
                  only users need to be able to focus the scroll region so
                  they can arrow-key through it. axe specifically rejects
                  tabindex=-1 here because that makes the region only
                  programmatically focusable, not reachable via Tab —
                  which doesn't help a keyboard-only user. The cost is one
                  extra Tab stop before route content; the benefit is WCAG
                  compliance for any loading skeleton (or otherwise empty
                  page) where there's no inner focusable content yet. */}
              <main className="min-h-0 flex-1 overflow-auto" tabIndex={0}>
                <Outlet />
              </main>
            </div>
            <RhpShell />
            <StatusTab />
            <BeadsHistoryTab />
            <HelpTab />
            <WorkItemDetailRegistration />
            <InteractionDetailRegistration />
            <AppHotkeys />
            <Palette />
            <PmPanel />
            <AppWalkBindings />
          </div>
        </ToastProvider>
        </RhpPinnedContentProvider>
        </RhpProvider>
        </WalkProvider>
        </CreateProjectModalProvider>
        </ProjectPickerProvider>
      </PmPanelProvider>
    </PaletteProvider>
  );
}
