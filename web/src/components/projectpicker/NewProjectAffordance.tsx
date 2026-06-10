// NewProjectAffordance — "+" button rendered immediately to the LEFT of
// the ProjectPicker in the top-bar chrome (gm-root.17.2 / gm-e12.21.3).
//
// Hover title / aria-label: "Create new project".
// Click: opens the unified Create-project modal.
//
// Always visible alongside the picker — no route or workspace conditions.

import { useCreateProjectModal } from '@/components/projects/CreateProjectModalContext';
import { cn } from '@/lib/utils';

export function NewProjectAffordance() {
  const { open } = useCreateProjectModal();

  return (
    <button
      type="button"
      data-testid="new-project-affordance"
      title="Create new project"
      aria-label="Create new project"
      onClick={() => open()}
      className={cn(
        'inline-flex h-8 w-8 items-center justify-center rounded-md text-lg font-light',
        'text-neutral-600 hover:bg-neutral-100 hover:text-neutral-900',
        'dark:text-neutral-400 dark:hover:bg-neutral-900 dark:hover:text-neutral-100'
      )}
    >
      +
    </button>
  );
}
