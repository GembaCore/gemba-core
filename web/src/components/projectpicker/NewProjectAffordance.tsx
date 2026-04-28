// NewProjectAffordance — "+" button rendered immediately to the LEFT of
// the ProjectPicker in the top-bar chrome (gm-root.17.2).
//
// Hover title / aria-label: "Create new project".
// Click: navigate to /new.
//
// Always visible alongside the picker — no route or workspace conditions.
// This component contains no project-creation logic; it is a pure
// navigation affordance.

import { useNavigate } from 'react-router-dom';
import { cn } from '@/lib/utils';

export function NewProjectAffordance() {
  const navigate = useNavigate();

  return (
    <button
      type="button"
      data-testid="new-project-affordance"
      title="Create new project"
      aria-label="Create new project"
      onClick={() => navigate('/new')}
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
