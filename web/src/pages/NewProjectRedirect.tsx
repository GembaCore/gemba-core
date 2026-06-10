// /new — redirects to / and auto-opens the unified Create-project
// modal (gm-e12.21.3). Replaces the standalone NewProjectPage form;
// the modal carries the same fields plus the Beads/Repository axes.

import { useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { useCreateProjectModal } from '@/components/projects/CreateProjectModalContext';

export function NewProjectRedirect(): JSX.Element | null {
  const navigate = useNavigate();
  const { open } = useCreateProjectModal();

  useEffect(() => {
    open();
    navigate('/board', { replace: true });
  }, [open, navigate]);

  return null;
}
