export type HotkeyCategory =
  | 'navigation'
  | 'selection'
  | 'bulk'
  | 'creation'
  | 'views'
  | 'drawer'
  | 'help';

export type Hotkey = {
  id: string;
  keys: string[];
  description: string;
  category: HotkeyCategory;
  scope?: string;
};

export type HotkeyHandler = (hotkey: Hotkey, event: KeyboardEvent) => void;
