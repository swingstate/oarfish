// German is the default UI language; English is prepared as a sibling
// dictionary with the same keys (ORB-GEN-006). Components never contain UI text.
import { de, type Messages } from './locales/de';
import { en } from './locales/en';

export { de, en };
export type { Messages };
export const t: Messages = de;

/**
 * Looks up a message whose key is built from API data, for example
 * `limitation.memory_512mb`. An unknown key is shown as the key itself, so a
 * value the translations do not know yet is visible instead of blank.
 */
export function msg(key: string, dict: Readonly<Record<string, string>> = de): string {
  return dict[key] ?? key;
}
