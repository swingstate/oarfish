// The UI language: German or English. The start language follows the browser;
// a choice made in the UI is kept in the browser (D-C77). Only the language
// code is stored, under one fixed key, and anything but "de" or "en" in the
// store is ignored. The store may be unavailable (private window, blocked site
// data): then the app works and just does not remember.
export type Lang = 'de' | 'en';

export const LANG_KEY = 'oarfish.lang';

function isLang(value: unknown): value is Lang {
  return value === 'de' || value === 'en';
}

function readStored(): Lang | null {
  try {
    const value = localStorage.getItem(LANG_KEY);
    return isLang(value) ? value : null;
  } catch {
    return null;
  }
}

function writeStored(lang: Lang): void {
  try {
    localStorage.setItem(LANG_KEY, lang);
  } catch {
    // Not remembered; the choice still holds until the page is closed.
  }
}

/** German if the first language the browser lists that we know is German; English otherwise. */
export function browserLang(languages: readonly string[] = navigator.languages?.length ? navigator.languages : [navigator.language]): Lang {
  for (const tag of languages) {
    const primary = tag.toLowerCase().split('-')[0];
    if (primary === 'de') return 'de';
    if (primary === 'en') return 'en';
  }
  return 'en';
}

let current = $state<Lang>('de');

function apply(lang: Lang): void {
  current = lang;
  document.documentElement.lang = lang;
}

/** The chosen language. Reading it inside a template or `$derived` makes that code follow a change. */
export function lang(): Lang {
  return current;
}

/** Sets the language, shows it at once and keeps it in the browser. */
export function setLang(next: Lang): void {
  apply(next);
  writeStored(next);
}

/** Decides the language from the store, then from the browser. Runs once at start; tests call it again. */
export function initLang(): Lang {
  apply(readStored() ?? browserLang());
  return current;
}

initLang();
