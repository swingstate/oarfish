// German is the default UI language; English is prepared as a sibling
// dictionary with the same keys (ORB-GEN-006). Components never contain UI text.
import { de, deApi, deUi, type Messages } from './locales/de';
import { en, enApi, enUi } from './locales/en';

export { de, deApi, deUi, en, enApi, enUi };
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

/** Whether the dictionary has a text for a key that is built from API data. */
export function has(key: string, dict: Readonly<Record<string, string>> = de): boolean {
  return Object.hasOwn(dict, key);
}

/** Values a message can interpolate: numbers are formatted for the language. */
export type MessageParams = Readonly<Record<string, string | number>>;

const numberFormat = new Intl.NumberFormat('de-DE', { maximumFractionDigits: 1 });

/**
 * Looks up a message and fills in `{name}` placeholders from params. Numbers are
 * formatted with the German number format (decimal comma). A placeholder
 * without a parameter stays visible as `{name}`, so a gap in the data shows.
 */
export function fmt(key: string, params: MessageParams = {}, dict: Readonly<Record<string, string>> = de): string {
  return msg(key, dict).replace(/\{(\w+)\}/g, (whole, name: string) => {
    const v = params[name];
    if (v === undefined) return whole;
    return typeof v === 'number' ? numberFormat.format(v) : v;
  });
}
