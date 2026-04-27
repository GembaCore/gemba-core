// Step 1 — Source tiles (gm-uipx.7 — ui-spec §5.15).
//
// Four large clickable tiles: Jira / Beads / Source-code / Fresh.
// Click commits the source choice to wizard state and advances to
// Step 2 with the source pinned.

import { Database, FolderTree, ListTree, Sparkles } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import type { BootstrapSource } from '@/api/bootstrap';
import { SOURCE_TILES } from './types';

const ICONS: Record<BootstrapSource, LucideIcon> = {
  jira: ListTree,
  beads: Database,
  'source-code': FolderTree,
  fresh: Sparkles,
};

export function SourceTiles({ onSelect }: { onSelect: (s: BootstrapSource) => void }): JSX.Element {
  return (
    <div data-testid="bootstrap-source-tiles" className="grid grid-cols-1 gap-3 sm:grid-cols-2">
      {SOURCE_TILES.map((tile) => {
        const Icon = ICONS[tile.id];
        return (
          <button
            key={tile.id}
            type="button"
            data-testid={`bootstrap-source-tile-${tile.id}`}
            onClick={() => onSelect(tile.id)}
            className="flex flex-col items-start gap-2 rounded-lg border border-neutral-200 bg-white px-5 py-4 text-left transition-colors hover:border-sky-400 hover:bg-sky-50 focus:outline-none focus:ring-2 focus:ring-sky-400 dark:border-neutral-800 dark:bg-neutral-950 dark:hover:border-sky-500 dark:hover:bg-sky-950/30"
          >
            <Icon className="h-6 w-6 text-sky-600 dark:text-sky-400" aria-hidden />
            <div>
              <div className="text-sm font-semibold">{tile.label}</div>
              <p className="mt-1 text-xs text-neutral-600 dark:text-neutral-400">
                {tile.description}
              </p>
            </div>
          </button>
        );
      })}
    </div>
  );
}
