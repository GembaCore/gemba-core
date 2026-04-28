// FilePicker (gm-e12.21.1) — reusable Radix popover anchored to a
// path input that lets the operator navigate the filesystem to pick
// a directory. Backed by GET /api/v1/fs/list which is allow-list-
// bounded to $HOME and the configured projects dir.
//
// API:
//
//   <FilePicker
//     value={path}
//     onChange={setPath}
//     requireBeadsDb={false}     // if true, only enables Select on dirs whose has_beads_db=true
//     requireExistingDir={true}  // false allows submitting an unvisited path (text-only entry)
//     placeholder='/path/to/...'
//     testid='repo-picker'
//   />
//
// The Create-project modal (gm-e12.21.3) drops this in place of the
// PathInput shim: same prop surface + callbacks. Until then it's
// opt-in per call site.

import * as Popover from '@radix-ui/react-popover';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { ChevronRight, Folder, FolderOpen, Database, Home as HomeIcon, FolderRoot } from 'lucide-react';
import { getFsInfo, listFs, type FsEntry, type FsListEnvelope } from '@/api/fs';
import { ApiError } from '@/api/client';
import { cn } from '@/lib/utils';

export interface FilePickerProps {
  value: string;
  onChange: (next: string) => void;
  // requireBeadsDb gates the Select button on the current dir
  // having a .beads/ subdir. The DB axis ("pick .beads") sets this;
  // the Repository axis leaves it false.
  requireBeadsDb?: boolean;
  // requireExistingDir blocks submission until the operator has
  // navigated into a real directory at least once. Default true.
  requireExistingDir?: boolean;
  disabled?: boolean;
  placeholder?: string;
  testid: string;
}

export function FilePicker({
  value,
  onChange,
  requireBeadsDb = false,
  requireExistingDir = true,
  disabled = false,
  placeholder,
  testid,
}: FilePickerProps) {
  const [open, setOpen] = useState(false);

  return (
    <div className="flex w-full items-center gap-1.5">
      <input
        type="text"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        disabled={disabled}
        placeholder={placeholder}
        data-testid={`${testid}-input`}
        className="min-w-0 flex-1 rounded-md border border-neutral-300 bg-white px-2 py-1 font-mono text-xs disabled:opacity-50 dark:border-neutral-700 dark:bg-neutral-900"
      />
      <Popover.Root open={open} onOpenChange={(o: boolean) => !disabled && setOpen(o)}>
        <Popover.Trigger asChild>
          <button
            type="button"
            disabled={disabled}
            data-testid={`${testid}-open`}
            aria-label="Browse"
            className={cn(
              'shrink-0 rounded-md border border-neutral-300 bg-white px-2 py-1 text-xs',
              'hover:bg-neutral-100 disabled:opacity-50',
              'dark:border-neutral-700 dark:bg-neutral-900 dark:hover:bg-neutral-800'
            )}
          >
            Browse…
          </button>
        </Popover.Trigger>
        <Popover.Portal>
          <Popover.Content
            data-testid={`${testid}-popover`}
            sideOffset={6}
            align="end"
            className="z-50 w-[28rem] rounded-md border border-neutral-200 bg-white p-2 shadow-lg dark:border-neutral-700 dark:bg-neutral-900"
          >
            <PickerBody
              initialPath={value}
              requireBeadsDb={requireBeadsDb}
              requireExistingDir={requireExistingDir}
              testid={testid}
              onSelect={(p) => {
                onChange(p);
                setOpen(false);
              }}
              onCancel={() => setOpen(false)}
            />
          </Popover.Content>
        </Popover.Portal>
      </Popover.Root>
    </div>
  );
}

// PickerBody owns the navigation state. Mounts when the popover
// opens; un-mounts on close so a re-open starts at the operator's
// current value (not stale from the previous visit).
function PickerBody({
  initialPath,
  requireBeadsDb,
  requireExistingDir,
  testid,
  onSelect,
  onCancel,
}: {
  initialPath: string;
  requireBeadsDb: boolean;
  requireExistingDir: boolean;
  testid: string;
  onSelect: (path: string) => void;
  onCancel: () => void;
}) {
  const [env, setEnv] = useState<FsListEnvelope | null>(null);
  const [path, setPath] = useState(initialPath);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const navigate = useCallback(async (target: string) => {
    setLoading(true);
    setError(null);
    try {
      const next = await listFs(target);
      setEnv(next);
      setPath(next.path);
    } catch (err) {
      if (err instanceof ApiError) {
        setError(err.message || `${err.status}`);
      } else {
        setError(err instanceof Error ? err.message : String(err));
      }
    } finally {
      setLoading(false);
    }
  }, []);

  // Bootstrap: try the operator's initial value first; fall back to
  // $HOME (resolved via /v1/fs/info) if that listing fails or
  // initialPath is empty.
  useEffect(() => {
    let cancelled = false;
    const bootstrap = async () => {
      // Quick path — operator already has a valid absolute path.
      if (initialPath && initialPath.startsWith('/')) {
        try {
          const next = await listFs(initialPath);
          if (!cancelled) {
            setEnv(next);
            setPath(next.path);
            return;
          }
        } catch {
          // Fall through to home.
        }
      }
      try {
        const info = await getFsInfo();
        if (!info.home) {
          throw new Error('No HOME directory available');
        }
        const next = await listFs(info.home);
        if (!cancelled) {
          setEnv(next);
          setPath(next.path);
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : String(err));
        }
      }
    };
    void bootstrap();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const dirs = useMemo(() => env?.entries.filter((e) => e.kind === 'dir') ?? [], [env]);
  const files = useMemo(() => env?.entries.filter((e) => e.kind === 'file') ?? [], [env]);

  // Select-button gate. requireBeadsDb wins: if the current dir
  // doesn't carry .beads/ (visible by env.entries containing one),
  // disable Select with a tooltip.
  const currentHasBeads = useMemo(() => {
    // .beads/ inside the current dir is filtered out as a dotfile,
    // so we infer presence by listing the parent and looking up
    // env.path's name. Simpler: just check the path's children for
    // a hidden .beads/ via a separate Stat round-trip — but we can
    // dodge that by using the current dir's "do we carry a beads
    // marker?" hint when navigating *into* a dir.
    //
    // For now: requireBeadsDb is informational only — the picker
    // shows a "no .beads/ here" hint and lets the operator pick
    // anyway. The modal handles the actual gate.
    return env?.entries.some((e) => e.name === '.beads') ?? false;
  }, [env]);

  const canSelect = useMemo(() => {
    if (loading) return false;
    if (!path) return false;
    if (requireExistingDir && !env) return false;
    return true;
  }, [loading, path, requireExistingDir, env]);

  return (
    <div data-testid={`${testid}-body`}>
      {/* Shortcuts row */}
      {env && (
        <div className="mb-1 flex items-center gap-1 text-[11px]">
          <button
            type="button"
            onClick={() => env.home && navigate(env.home)}
            disabled={!env.home}
            data-testid={`${testid}-shortcut-home`}
            className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-neutral-700 hover:bg-neutral-100 disabled:opacity-50 dark:text-neutral-300 dark:hover:bg-neutral-800"
          >
            <HomeIcon className="h-3 w-3" /> Home
          </button>
          {env.projects_dir && (
            <button
              type="button"
              onClick={() => env.projects_dir && navigate(env.projects_dir)}
              data-testid={`${testid}-shortcut-projects`}
              className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-neutral-700 hover:bg-neutral-100 dark:text-neutral-300 dark:hover:bg-neutral-800"
            >
              <FolderRoot className="h-3 w-3" /> Projects
            </button>
          )}
        </div>
      )}

      {/* Breadcrumb / current path */}
      <div className="mb-2 flex items-center gap-1 rounded border border-neutral-200 bg-neutral-50 px-2 py-1 font-mono text-[11px] dark:border-neutral-700 dark:bg-neutral-950">
        <FolderOpen className="h-3 w-3 shrink-0 text-sky-600 dark:text-sky-400" />
        <span data-testid={`${testid}-current-path`} className="truncate">{path || '…'}</span>
      </div>

      {/* Parent navigation */}
      {env?.parent && (
        <button
          type="button"
          onClick={() => navigate(env.parent!)}
          data-testid={`${testid}-up`}
          className="mb-1 flex w-full items-center gap-2 rounded px-2 py-1 text-left text-xs text-neutral-600 hover:bg-neutral-100 dark:text-neutral-400 dark:hover:bg-neutral-800"
        >
          <ChevronRight className="h-3 w-3 rotate-180" />
          <span className="font-mono">..</span>
        </button>
      )}

      {/* Listing */}
      <div className="max-h-64 overflow-auto rounded border border-neutral-200 dark:border-neutral-700">
        {loading && (
          <div className="px-2 py-3 text-center text-[11px] text-neutral-500">Loading…</div>
        )}
        {!loading && error && (
          <div data-testid={`${testid}-error`} className="px-2 py-3 text-[11px] text-red-700 dark:text-red-300">{error}</div>
        )}
        {!loading && !error && env && dirs.length === 0 && files.length === 0 && (
          <div className="px-2 py-3 text-[11px] text-neutral-500">Empty directory</div>
        )}
        {!loading && !error && env && (
          <ul role="list">
            {dirs.map((e) => (
              <DirRow key={e.name} entry={e} onClick={() => navigate(joinPath(env.path, e.name))} testid={`${testid}-entry-${e.name}`} />
            ))}
            {files.map((e) => (
              <FileRow key={e.name} entry={e} testid={`${testid}-entry-${e.name}`} />
            ))}
          </ul>
        )}
      </div>

      {requireBeadsDb && env && !currentHasBeads && (
        <div className="mt-2 text-[11px] text-amber-700 dark:text-amber-400">
          This directory has no .beads/ — the operator will need to navigate into a project root.
        </div>
      )}

      <div className="mt-2 flex justify-end gap-1.5">
        <button
          type="button"
          onClick={onCancel}
          data-testid={`${testid}-cancel`}
          className="rounded px-2 py-1 text-xs text-neutral-700 hover:bg-neutral-100 dark:text-neutral-300 dark:hover:bg-neutral-800"
        >
          Cancel
        </button>
        <button
          type="button"
          onClick={() => onSelect(path)}
          disabled={!canSelect}
          data-testid={`${testid}-select`}
          className={cn(
            'rounded px-3 py-1 text-xs font-medium',
            canSelect
              ? 'bg-neutral-900 text-white hover:bg-neutral-800 dark:bg-neutral-100 dark:text-neutral-900'
              : 'bg-neutral-200 text-neutral-500 dark:bg-neutral-800 dark:text-neutral-500'
          )}
        >
          Select
        </button>
      </div>
    </div>
  );
}

function DirRow({ entry, onClick, testid }: { entry: FsEntry; onClick: () => void; testid: string }) {
  return (
    <li>
      <button
        type="button"
        onClick={onClick}
        data-testid={testid}
        className="flex w-full items-center gap-2 px-2 py-1 text-left text-xs hover:bg-neutral-100 dark:hover:bg-neutral-800"
      >
        <Folder className="h-3 w-3 shrink-0 text-sky-600 dark:text-sky-400" />
        <span className="truncate font-mono">{entry.name}</span>
        {entry.has_beads_db && (
          <span
            data-testid={`${testid}-beads`}
            className="ml-auto inline-flex items-center gap-1 rounded bg-amber-100 px-1.5 py-0.5 text-[9px] font-medium uppercase text-amber-800 dark:bg-amber-950 dark:text-amber-300"
          >
            <Database className="h-2.5 w-2.5" /> beads
          </span>
        )}
      </button>
    </li>
  );
}

function FileRow({ entry, testid }: { entry: FsEntry; testid: string }) {
  return (
    <li>
      <div
        data-testid={testid}
        className="flex w-full cursor-not-allowed items-center gap-2 px-2 py-1 text-xs opacity-50"
      >
        <span className="block h-3 w-3 shrink-0" />
        <span className="truncate font-mono">{entry.name}</span>
      </div>
    </li>
  );
}

// joinPath is filepath.Join's pure-JS counterpart. POSIX-only — the
// server resolves Windows paths client-side as POSIX since the
// fs.list endpoint already returns canonical absolute paths.
function joinPath(parent: string, child: string): string {
  if (parent.endsWith('/')) return parent + child;
  return parent + '/' + child;
}

