// German and English are sibling dictionaries with the same keys
// (ORB-GEN-006). Components never contain UI text. The language is chosen in
// lang.svelte.ts; `t`, `msg` and `fmt` answer in the chosen language, so a
// component that reads them in its markup follows a change without a reload.
import { lang } from './lang.svelte';
import { de, deApi, deUi, type Messages } from './locales/de';
import { en, enApi, enUi } from './locales/en';

export { de, deApi, deUi, en, enApi, enUi };
export type { Messages };

/** Every key of the dictionaries, with a plain string as the text. */
export type Texts = { readonly [K in keyof Messages]: string };

/** The dictionary of the chosen language. */
export function dict(): Texts {
  return lang() === 'en' ? en : de;
}

/** The UI texts in the chosen language. Read it where it is shown, not once at start. */
export const t: Texts = new Proxy<Texts>(de, {
  get: (_target, key) => (dict() as Record<string | symbol, unknown>)[key],
});

/**
 * Looks up a message whose key is built from API data, for example
 * `limitation.memory_512mb`. An unknown key is shown as the key itself, so a
 * value the translations do not know yet is visible instead of blank.
 */
export function msg(key: string, from: Readonly<Record<string, string>> = dict()): string {
  return from[key] ?? key;
}

/** Whether the dictionary has a text for a key that is built from API data. */
export function has(key: string, from: Readonly<Record<string, string>> = dict()): boolean {
  return Object.hasOwn(from, key);
}

/** Values a message can interpolate: numbers are formatted for the language. */
export type MessageParams = Readonly<Record<string, string | number>>;

const numberFormat = new Intl.NumberFormat('de-DE', { maximumFractionDigits: 1 });

/**
 * Looks up a message and fills in `{name}` placeholders from params. Numbers are
 * formatted with the German number format (decimal comma). A placeholder
 * without a parameter stays visible as `{name}`, so a gap in the data shows.
 */
export function fmt(key: string, params: MessageParams = {}, from: Readonly<Record<string, string>> = dict()): string {
  return msg(key, from).replace(/\{(\w+)\}/g, (whole, name: string) => {
    const v = params[name];
    if (v === undefined) return whole;
    return typeof v === 'number' ? numberFormat.format(v) : v;
  });
}
