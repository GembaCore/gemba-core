import { createContext } from 'react';
import type { HotkeyRegistry } from './registry';

export const HotkeysContext = createContext<HotkeyRegistry | null>(null);
