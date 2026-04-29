// RhpShell — visual chrome for the Right-Hand Panel (gm-root.22.2).
//
// Per docs/design/rhp.md, the shell renders to the right of <main>:
//
//   - Tab rail on the left (~40px wide, vertical Lucide icon column)
//   - Pinned tabs at the top, separator, then detail tabs in pop-order
//   - Active tab body fills the rest of the panel
//   - Drag handle on the LEFT edge for resize (320–800, default 384)
//   - Caret button at the top of the rail toggles collapse
//
// This bead ships ONLY the shell. Pinned-tab content (Help) and
// detail-tab content live in `gm-root.22.3` and `gm-root.22.4`
// respectively. The body falls back to a placeholder until those
// agents register content.

import {
  useCallback,
  useEffect,
  useRef,
  type PointerEvent as ReactPointerEvent,
} from 'react';
import { ChevronLeft, ChevronRight, X } from 'lucide-react';
import { cn } from '@/lib/utils';
import {
  RHP_WIDTH_MAX,
  RHP_WIDTH_MIN,
  useRhp,
  useRhpDetailRegistry,
  type RhpTab,
} from './RhpContext';
import { useRhpPinnedContent } from './RhpPinnedContent';
import { RhpFallbackRegistration } from './RhpDetail';

const RAIL_WIDTH_PX = 40;

export function RhpShell() {
  const {
    tabs,
    activeTabId,
    focusTab,
    closeTab,
    collapsed,
    setCollapsed,
    width,
    setWidth,
  } = useRhp();

  const railRef = useRef<HTMLDivElement | null>(null);
  const activeButtonRef = useRef<HTMLButtonElement | null>(null);

  // Active tab kept in view via scrollIntoView({block: 'nearest'}).
  // Per design doc this fires on focus change.
  useEffect(() => {
    activeButtonRef.current?.scrollIntoView({ block: 'nearest' });
  }, [activeTabId]);

  // Drag-resize. Pointer events let one handler drive mouse + touch.
  // We track the start position and starting width on pointerdown,
  // then update width on pointermove until pointerup.
  const dragState = useRef<{ startX: number; startWidth: number } | null>(null);
  const onHandlePointerDown = useCallback(
    (e: ReactPointerEvent<HTMLDivElement>) => {
      e.preventDefault();
      // Capture the pointer so we keep getting move/up events even if
      // the cursor leaves the handle bounds.
      (e.target as Element).setPointerCapture?.(e.pointerId);
      dragState.current = { startX: e.clientX, startWidth: width };

      const onMove = (ev: PointerEvent) => {
        if (!dragState.current) return;
        // Handle is on the LEFT edge of the panel — dragging left
        // grows the panel (window.innerWidth is fixed), so we invert
        // the delta vs the typical right-edge handle.
        const delta = dragState.current.startX - ev.clientX;
        setWidth(dragState.current.startWidth + delta);
      };
      const onUp = (ev: PointerEvent) => {
        dragState.current = null;
        (e.target as Element).releasePointerCapture?.(ev.pointerId);
        window.removeEventListener('pointermove', onMove);
        window.removeEventListener('pointerup', onUp);
      };
      window.addEventListener('pointermove', onMove);
      window.addEventListener('pointerup', onUp);
    },
    [setWidth, width]
  );

  // Active tab body — pinned-tab and detail-tab content registries
  // live in sibling beads. Until those land, the body shows a
  // placeholder ("This tab has no content registered yet").
  const activeTab = tabs.find((t) => t.id === activeTabId) ?? null;
  const railWidth = RAIL_WIDTH_PX;
  const totalWidth = collapsed ? railWidth : Math.max(width, RHP_WIDTH_MIN);

  // Insert a separator between pinned and detail tabs (per design
  // doc: 1px line). We compute the index where the first detail tab
  // begins so the rail can render the separator inline.
  const firstDetailIdx = tabs.findIndex((t) => !t.pinned);

  return (
    <aside
      data-testid="rhp-shell"
      data-collapsed={collapsed ? 'true' : 'false'}
      className={cn(
        'relative flex h-full shrink-0 border-l',
        'border-neutral-200 bg-white',
        'dark:border-neutral-800 dark:bg-neutral-950'
      )}
      style={{ width: totalWidth }}
      aria-label="Right-hand panel"
    >
      {/* Mount-once side-effect: registers the fallback icon for the
          rail so detail tabs of unregistered kinds still render with a
          valid icon. Renders nothing. */}
      <RhpFallbackRegistration />
      {/* Drag handle — sits flush with the panel's left edge. Only
          interactive when not collapsed; the rail's caret is the
          collapse toggle and the handle would be confusing otherwise. */}
      {!collapsed ? (
        <div
          data-testid="rhp-drag-handle"
          role="separator"
          aria-orientation="vertical"
          aria-label="Resize right-hand panel"
          aria-valuemin={RHP_WIDTH_MIN}
          aria-valuemax={RHP_WIDTH_MAX}
          aria-valuenow={width}
          onPointerDown={onHandlePointerDown}
          className={cn(
            'absolute left-0 top-0 z-10 h-full w-1 cursor-col-resize',
            'hover:bg-neutral-300 dark:hover:bg-neutral-700'
          )}
        />
      ) : null}

      {/* Tab rail — left column inside the panel. */}
      <div
        ref={railRef}
        data-testid="rhp-rail"
        className={cn(
          'flex h-full flex-col items-stretch overflow-y-auto',
          'border-r border-neutral-200 dark:border-neutral-800',
          'bg-neutral-50 dark:bg-neutral-900'
        )}
        style={{ width: railWidth, minWidth: railWidth }}
      >
        {/* Collapse caret at the top of the rail. */}
        <button
          type="button"
          data-testid="rhp-collapse-toggle"
          aria-label={collapsed ? 'Expand right-hand panel' : 'Collapse right-hand panel'}
          aria-expanded={!collapsed}
          onClick={() => setCollapsed(!collapsed)}
          className={cn(
            'flex h-9 w-full items-center justify-center',
            'text-neutral-500 hover:bg-neutral-100 hover:text-neutral-900',
            'dark:text-neutral-400 dark:hover:bg-neutral-800 dark:hover:text-neutral-100'
          )}
        >
          {collapsed ? <ChevronLeft size={16} /> : <ChevronRight size={16} />}
        </button>

        {tabs.map((tab, idx) => (
          <RailIcon
            key={tab.id}
            tab={tab}
            active={tab.id === activeTabId}
            buttonRef={tab.id === activeTabId ? activeButtonRef : undefined}
            showSeparatorBefore={!tab.pinned && idx === firstDetailIdx}
            onFocus={() => focusTab(tab.id)}
            onClose={tab.pinned ? null : () => closeTab(tab.id)}
          />
        ))}
      </div>

      {/* Body — only rendered when expanded. */}
      {!collapsed ? (
        <div
          data-testid="rhp-body"
          className="flex min-w-0 flex-1 flex-col overflow-hidden"
        >
          {activeTab ? (
            <>
              <div
                data-testid="rhp-body-title"
                className={cn(
                  'flex h-9 items-center px-3 text-sm font-medium',
                  'border-b border-neutral-200 dark:border-neutral-800'
                )}
              >
                {activeTab.label}
              </div>
              <div className="min-h-0 flex-1 overflow-auto text-sm text-neutral-700 dark:text-neutral-300">
                <RhpTabBody tab={activeTab} />
              </div>
            </>
          ) : (
            <div
              data-testid="rhp-empty"
              className="flex h-full items-center justify-center p-4 text-center text-sm text-neutral-500"
            >
              No tabs open.
            </div>
          )}
        </div>
      ) : null}
    </aside>
  );
}

interface RailIconProps {
  tab: RhpTab;
  active: boolean;
  showSeparatorBefore: boolean;
  buttonRef?: React.RefObject<HTMLButtonElement>;
  onFocus(): void;
  onClose: (() => void) | null;
}

function RailIcon({
  tab,
  active,
  showSeparatorBefore,
  buttonRef,
  onFocus,
  onClose,
}: RailIconProps) {
  const Icon = tab.icon;
  return (
    <>
      {showSeparatorBefore ? (
        <div
          data-testid="rhp-rail-separator"
          aria-hidden="true"
          className="mx-2 my-1 h-px bg-neutral-200 dark:bg-neutral-800"
        />
      ) : null}
      <div className="relative">
        <button
          ref={buttonRef}
          type="button"
          data-testid={`rhp-tab-${tab.id}`}
          data-active={active ? 'true' : 'false'}
          aria-label={tab.label}
          title={tab.label}
          onClick={onFocus}
          className={cn(
            'flex h-10 w-full items-center justify-center',
            'text-neutral-500 hover:bg-neutral-100 hover:text-neutral-900',
            'dark:text-neutral-400 dark:hover:bg-neutral-800 dark:hover:text-neutral-100',
            active &&
              cn(
                'bg-neutral-100 text-neutral-900',
                'dark:bg-neutral-800 dark:text-neutral-100',
                // Accent border on the right edge.
                'border-r-2 border-r-blue-500'
              )
          )}
        >
          {Icon ? <Icon size={18} /> : null}
        </button>
        {onClose ? (
          <button
            type="button"
            data-testid={`rhp-tab-close-${tab.id}`}
            aria-label={`Close ${tab.label}`}
            onClick={(e) => {
              e.stopPropagation();
              onClose();
            }}
            className={cn(
              'absolute right-0 top-0 hidden h-4 w-4 items-center justify-center',
              'rounded text-neutral-400 hover:bg-neutral-200 hover:text-neutral-900',
              'group-hover:flex',
              'dark:hover:bg-neutral-700 dark:hover:text-neutral-100'
            )}
          >
            <X size={10} />
          </button>
        ) : null}
      </div>
    </>
  );
}

// RhpTabBody — resolves body content for the active tab.
//
// Routing logic:
//   - Pinned tabs: looked up in RhpPinnedContentContext (registered by
//     HelpTab in gm-root.22.3; future pinned tabs follow the same pattern).
//   - Detail tabs: looked up in RhpDetailRegistryContext (registered by
//     kind owners in gm-root.22.5/.6/.7).
//   - Falls back to a placeholder when no content is registered yet.
function RhpTabBody({ tab }: { tab: RhpTab }) {
  const pinnedContent = useRhpPinnedContent();
  const detailRegistry = useRhpDetailRegistry();

  if (tab.pinned) {
    const render = pinnedContent.resolve(tab.id);
    if (render) return render();
  } else {
    const reg = detailRegistry.resolve(tab.kind);
    if (reg) {
      // id is 'kind:actualId'; strip the kind prefix.
      const colon = tab.id.indexOf(':');
      const actualId = colon >= 0 ? tab.id.slice(colon + 1) : tab.id;
      return <>{reg.render(actualId)}</>;
    }
  }

  return (
    <div data-testid="rhp-body-placeholder" className="p-3">
      <p>This tab has no content registered yet.</p>
      <p className="mt-2 text-xs text-neutral-500">
        kind: <code>{tab.kind}</code>
        {tab.id !== tab.kind ? (
          <>
            {' · id: '}
            <code>{tab.id.slice(tab.kind.length + 1)}</code>
          </>
        ) : null}
      </p>
    </div>
  );
}
