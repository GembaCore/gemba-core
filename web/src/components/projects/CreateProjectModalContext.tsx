// CreateProjectModalContext — slim app-level state for the unified
// project-creation modal (gm-e12.21.3). The modal itself lives in
// AppShell so the topbar '+' button, the picker dropdown's adopt
// affordances, and the legacy /new redirect all open the same
// instance.

import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react';
import { CreateProjectModal, type CreateProjectModalProps } from './CreateProjectModal';

type Prefill = NonNullable<CreateProjectModalProps['prefill']>;

interface CreateProjectModalApi {
  open: (prefill?: Prefill) => void;
}

const Ctx = createContext<CreateProjectModalApi | null>(null);

export function CreateProjectModalProvider({ children }: { children: ReactNode }) {
  const [isOpen, setIsOpen] = useState(false);
  const [prefill, setPrefill] = useState<Prefill | undefined>(undefined);

  const open = useCallback((p?: Prefill) => {
    setPrefill(p);
    setIsOpen(true);
  }, []);

  const close = useCallback(() => {
    setIsOpen(false);
    // Keep prefill around briefly so the closing animation doesn't
    // flash an empty form before the dialog unmounts. The next open()
    // overwrites it.
  }, []);

  const value = useMemo<CreateProjectModalApi>(() => ({ open }), [open]);

  return (
    <Ctx.Provider value={value}>
      {children}
      <CreateProjectModal open={isOpen} onClose={close} prefill={prefill} />
    </Ctx.Provider>
  );
}

export function useCreateProjectModal(): CreateProjectModalApi {
  const ctx = useContext(Ctx);
  if (!ctx) {
    throw new Error('useCreateProjectModal must be used inside CreateProjectModalProvider');
  }
  return ctx;
}
