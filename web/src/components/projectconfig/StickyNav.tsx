// Sticky section nav for /project/config (gm-uipx.8). Per ui-spec
// §5.16: single-scroll page with a sticky nav on the left listing
// every section in order. Active highlight tracks the section
// currently in view via IntersectionObserver.

import { useEffect, useState } from 'react';
import { cn } from '@/lib/utils';
import { SECTIONS, type SectionID } from './types';

export function StickyNav(): JSX.Element {
  const [active, setActive] = useState<SectionID>(() => {
    const hash = (typeof window !== 'undefined' ? window.location.hash : '').replace(/^#/, '');
    const match = SECTIONS.find((s) => s.id === hash);
    return match?.id ?? SECTIONS[0].id;
  });

  // IntersectionObserver tracks which section is most-visible. We
  // pick the one whose top edge is closest to the viewport's top
  // (with a small offset so the heading itself counts).
  useEffect(() => {
    if (typeof window === 'undefined') return;
    // jsdom (vitest) doesn't ship IntersectionObserver. Guard so
    // unit tests don't crash; the active highlight just stays
    // pinned to whatever the URL hash resolved to.
    if (typeof IntersectionObserver === 'undefined') return;
    const observed = SECTIONS.map((s) =>
      document.getElementById(`section-${s.id}`)
    ).filter((n): n is HTMLElement => Boolean(n));
    if (observed.length === 0) return;
    const obs = new IntersectionObserver(
      (entries) => {
        // Pick the topmost intersecting section. Sorting by
        // boundingClientRect.top keeps the active highlight stable
        // when two sections touch the viewport simultaneously.
        const visible = entries
          .filter((e) => e.isIntersecting)
          .sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top);
        if (visible.length === 0) return;
        const id = visible[0].target.id.replace(/^section-/, '') as SectionID;
        setActive(id);
      },
      { rootMargin: '-20% 0px -60% 0px' }
    );
    observed.forEach((el) => obs.observe(el));
    return () => obs.disconnect();
  }, []);

  return (
    <nav
      data-testid="project-config-sticky-nav"
      aria-label="Project config sections"
      className="sticky top-0 w-52 shrink-0 border-r border-neutral-200 bg-neutral-50 p-3 dark:border-neutral-800 dark:bg-neutral-950"
    >
      <h2 className="mb-2 text-[10px] font-semibold uppercase tracking-wider text-neutral-500">
        Sections
      </h2>
      <ul className="space-y-0.5">
        {SECTIONS.map((s) => (
          <li key={s.id}>
            <a
              href={`#${s.id}`}
              data-testid={`project-config-nav-${s.id}`}
              data-active={active === s.id ? 'true' : 'false'}
              onClick={(e) => {
                e.preventDefault();
                const el = document.getElementById(`section-${s.id}`);
                if (el) {
                  el.scrollIntoView({ behavior: 'smooth', block: 'start' });
                  setActive(s.id);
                  // Update the URL hash so deep links survive a
                  // reload and the back button moves the operator
                  // through visited sections.
                  window.history.replaceState(null, '', `#${s.id}`);
                }
              }}
              className={cn(
                'block rounded px-2 py-1 text-sm transition-colors',
                active === s.id
                  ? 'bg-sky-100 font-semibold text-sky-900 dark:bg-sky-950/50 dark:text-sky-100'
                  : 'text-neutral-600 hover:bg-neutral-100 hover:text-neutral-900 dark:text-neutral-400 dark:hover:bg-neutral-900'
              )}
            >
              {s.label}
            </a>
          </li>
        ))}
      </ul>
    </nav>
  );
}
