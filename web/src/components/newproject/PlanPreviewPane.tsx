// PlanPreviewPane (gm-root.17.3 — see docs/design/newproject.md).
//
// Right pane of the /new route. Renders the live plan tree
// (Milestones → Epics → Beads) and the draft project description.
// In-place edits feed back into NewProjectState so the next skill
// turn sees them.
//
// Per the design: editable in-place. Edits do NOT call the skill
// (they're operator-typed text); they update local state and the
// host posts them as `edits` on the next /turn.

import type {
  ChangeRef,
  DraftBead,
  DraftEpic,
  DraftMilestone,
  NewProjectState,
} from '@/api/newproject';

export interface PlanPreviewPaneProps {
  state: NewProjectState;
  onEdit: (next: NewProjectState) => void;
  // True while a turn is committing or the route is in a terminal
  // state. Disables in-place edits.
  disabled: boolean;
}

export function PlanPreviewPane({
  state,
  onEdit,
  disabled,
}: PlanPreviewPaneProps): JSX.Element {
  const setProjectName = (v: string) => onEdit({ ...state, ProjectName: v });
  const setDescription = (v: string) => onEdit({ ...state, Description: v });
  const setDraftMD = (v: string) => onEdit({ ...state, DraftProjectMD: v });

  const setMilestone = (idx: number, next: DraftMilestone) => {
    const ms = state.Milestones.slice();
    ms[idx] = next;
    onEdit({ ...state, Milestones: ms });
  };

  return (
    <section
      data-testid="newproject-plan-pane"
      className="flex min-h-0 min-w-0 flex-1 flex-col"
    >
      <header className="border-b border-neutral-200 px-4 py-3 dark:border-neutral-800">
        <h2 className="text-sm font-semibold">Plan preview</h2>
        <p className="text-xs text-neutral-500 dark:text-neutral-400">
          Edit any field. The Onboarder picks up your edits on the next turn.
        </p>
      </header>

      <div className="flex-1 overflow-y-auto px-4 py-3">
        <DiffBadge change={state.LastChange} />

        <div className="mb-4 grid grid-cols-1 gap-3">
          <Field
            testid="newproject-name"
            label="Project name"
            value={state.ProjectName}
            onChange={setProjectName}
            disabled={disabled}
            placeholder="(no name yet — describe it in the conversation)"
          />
          <Field
            testid="newproject-description"
            label="Description"
            value={state.Description}
            onChange={setDescription}
            disabled={disabled}
            multiline
            placeholder="One-paragraph project description."
          />
        </div>

        <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-neutral-500 dark:text-neutral-400">
          Plan tree
        </h3>
        {state.Milestones.length === 0 ? (
          <p
            data-testid="newproject-plan-empty"
            className="rounded-md border border-dashed border-neutral-300 px-3 py-6 text-center text-xs italic text-neutral-500 dark:border-neutral-700 dark:text-neutral-400"
          >
            No milestones yet. Tell the Onboarder what you want to build.
          </p>
        ) : (
          <ol data-testid="newproject-milestones" className="space-y-3">
            {state.Milestones.map((m, i) => (
              <MilestoneRow
                key={`m-${i}`}
                index={i}
                milestone={m}
                disabled={disabled}
                onChange={(next) => setMilestone(i, next)}
              />
            ))}
          </ol>
        )}

        <h3 className="mb-2 mt-6 text-xs font-semibold uppercase tracking-wide text-neutral-500 dark:text-neutral-400">
          docs/project.md (draft)
        </h3>
        <Field
          testid="newproject-draft-md"
          label=""
          value={state.DraftProjectMD}
          onChange={setDraftMD}
          disabled={disabled}
          multiline
          rows={8}
          placeholder="The Onboarder writes a project narrative here as the conversation progresses."
        />
      </div>
    </section>
  );
}

function DiffBadge({ change }: { change: ChangeRef }): JSX.Element | null {
  if (!change.summary && !change.path) return null;
  const tone =
    change.kind === 'removed'
      ? 'bg-rose-100 text-rose-800 dark:bg-rose-950/40 dark:text-rose-200'
      : change.kind === 'added'
        ? 'bg-emerald-100 text-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-200'
        : 'bg-amber-100 text-amber-800 dark:bg-amber-950/40 dark:text-amber-200';
  return (
    <div
      data-testid="newproject-diff-badge"
      data-change-kind={change.kind || 'unspecified'}
      data-change-path={change.path}
      className={`mb-3 rounded px-2 py-1 text-[11px] ${tone}`}
    >
      <span className="font-mono text-[10px] opacity-80">{change.path || '(root)'}</span>{' '}
      <span>{change.summary || 'Updated.'}</span>
    </div>
  );
}

interface MilestoneRowProps {
  index: number;
  milestone: DraftMilestone;
  disabled: boolean;
  onChange: (next: DraftMilestone) => void;
}

function MilestoneRow({ index, milestone, disabled, onChange }: MilestoneRowProps): JSX.Element {
  const setEpic = (i: number, next: DraftEpic) => {
    const epics = milestone.Epics.slice();
    epics[i] = next;
    onChange({ ...milestone, Epics: epics });
  };
  return (
    <li
      data-testid={`newproject-milestone-${index}`}
      className="rounded-md border border-neutral-200 bg-neutral-50 p-3 dark:border-neutral-800 dark:bg-neutral-950"
    >
      <Field
        testid={`newproject-milestone-${index}-title`}
        label={`Milestone ${index + 1} title`}
        value={milestone.Title}
        onChange={(v) => onChange({ ...milestone, Title: v })}
        disabled={disabled}
      />
      <Field
        testid={`newproject-milestone-${index}-description`}
        label="Description"
        value={milestone.Description}
        onChange={(v) => onChange({ ...milestone, Description: v })}
        disabled={disabled}
        multiline
      />
      {milestone.Epics.length === 0 ? (
        <p className="mt-2 text-[11px] italic text-neutral-500 dark:text-neutral-400">
          No epics yet under this milestone.
        </p>
      ) : (
        <ol className="mt-2 space-y-2 pl-3">
          {milestone.Epics.map((e, i) => (
            <EpicRow
              key={`m-${index}-e-${i}`}
              milestoneIndex={index}
              index={i}
              epic={e}
              disabled={disabled}
              onChange={(next) => setEpic(i, next)}
            />
          ))}
        </ol>
      )}
    </li>
  );
}

interface EpicRowProps {
  milestoneIndex: number;
  index: number;
  epic: DraftEpic;
  disabled: boolean;
  onChange: (next: DraftEpic) => void;
}

function EpicRow({ milestoneIndex, index, epic, disabled, onChange }: EpicRowProps): JSX.Element {
  const setBead = (i: number, next: DraftBead) => {
    const beads = epic.Beads.slice();
    beads[i] = next;
    onChange({ ...epic, Beads: beads });
  };
  return (
    <li
      data-testid={`newproject-milestone-${milestoneIndex}-epic-${index}`}
      className="rounded border border-neutral-200 bg-white p-2 dark:border-neutral-800 dark:bg-neutral-900"
    >
      <Field
        testid={`newproject-milestone-${milestoneIndex}-epic-${index}-title`}
        label={`Epic ${index + 1} title`}
        value={epic.Title}
        onChange={(v) => onChange({ ...epic, Title: v })}
        disabled={disabled}
      />
      {epic.Beads.length > 0 && (
        <ol className="mt-2 space-y-1 pl-3">
          {epic.Beads.map((b, i) => (
            <BeadRow
              key={`m-${milestoneIndex}-e-${index}-b-${i}`}
              milestoneIndex={milestoneIndex}
              epicIndex={index}
              index={i}
              bead={b}
              disabled={disabled}
              onChange={(next) => setBead(i, next)}
            />
          ))}
        </ol>
      )}
    </li>
  );
}

interface BeadRowProps {
  milestoneIndex: number;
  epicIndex: number;
  index: number;
  bead: DraftBead;
  disabled: boolean;
  onChange: (next: DraftBead) => void;
}

function BeadRow({
  milestoneIndex,
  epicIndex,
  index,
  bead,
  disabled,
  onChange,
}: BeadRowProps): JSX.Element {
  return (
    <li
      data-testid={`newproject-milestone-${milestoneIndex}-epic-${epicIndex}-bead-${index}`}
      className="text-[11px]"
    >
      <Field
        testid={`newproject-milestone-${milestoneIndex}-epic-${epicIndex}-bead-${index}-title`}
        label={`Bead ${index + 1}`}
        value={bead.Title}
        onChange={(v) => onChange({ ...bead, Title: v })}
        disabled={disabled}
      />
    </li>
  );
}

interface FieldProps {
  testid: string;
  label: string;
  value: string;
  onChange: (v: string) => void;
  disabled: boolean;
  placeholder?: string;
  multiline?: boolean;
  rows?: number;
}

function Field({
  testid,
  label,
  value,
  onChange,
  disabled,
  placeholder,
  multiline,
  rows,
}: FieldProps): JSX.Element {
  const cls =
    'mt-1 w-full rounded border border-neutral-300 bg-white px-2 py-1 text-xs disabled:cursor-not-allowed disabled:opacity-60 dark:border-neutral-700 dark:bg-neutral-950';
  return (
    <label className="block">
      {label && (
        <span className="text-[10px] uppercase tracking-wide text-neutral-500 dark:text-neutral-400">
          {label}
        </span>
      )}
      {multiline ? (
        <textarea
          data-testid={testid}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          disabled={disabled}
          rows={rows ?? 2}
          placeholder={placeholder}
          className={cls}
        />
      ) : (
        <input
          data-testid={testid}
          type="text"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          disabled={disabled}
          placeholder={placeholder}
          className={cls}
        />
      )}
    </label>
  );
}
