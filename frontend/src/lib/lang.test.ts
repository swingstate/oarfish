import { afterEach, describe, expect, it, vi } from 'vitest';
import { de, en, fmt, msg, t } from './i18n';
import { browserLang, initLang, lang, LANG_KEY, setLang } from './lang.svelte';

function browserLanguages(...languages: string[]): void {
  Object.defineProperty(navigator, 'languages', { value: languages, configurable: true });
}

afterEach(() => {
  vi.restoreAllMocks();
  browserLanguages('de-DE', 'de');
});

describe('start language', () => {
  it('follows the browser: German for German, English for anything else', () => {
    expect(browserLang(['de-DE', 'de'])).toBe('de');
    expect(browserLang(['de-AT'])).toBe('de');
    expect(browserLang(['en-US', 'de'])).toBe('en');
    expect(browserLang(['fr-FR'])).toBe('en');
    expect(browserLang(['fr-FR', 'de'])).toBe('de');
    expect(browserLang([])).toBe('en');
  });

  it('uses the browser language when nothing is stored', () => {
    browserLanguages('en-GB');
    expect(initLang()).toBe('en');
    expect(document.documentElement.lang).toBe('en');
    expect(t.navDashboard).toBe(en.navDashboard);
    expect(t.navGroupSystem).toBe('System');
  });

  it('does not write to the store just by starting', () => {
    initLang();
    expect(localStorage.getItem(LANG_KEY)).toBeNull();
  });
});

describe('language choice', () => {
  it('switches without a reload, updates html lang and keeps only the code under one key', () => {
    setLang('en');
    expect(lang()).toBe('en');
    expect(document.documentElement.lang).toBe('en');
    expect(t.skipLink).toBe(en.skipLink);
    expect(msg('tier.compatible')).toBe('Compatible');
    expect(fmt('versionLine', { version: 'v1' })).toBe('Version v1');
    expect(localStorage.length).toBe(1);
    expect(localStorage.getItem(LANG_KEY)).toBe('en');

    setLang('de');
    expect(document.documentElement.lang).toBe('de');
    expect(t.skipLink).toBe(de.skipLink);
    expect(localStorage.getItem(LANG_KEY)).toBe('de');
  });

  it('survives a reload: the stored choice wins over the browser', () => {
    browserLanguages('de-DE');
    setLang('en');
    expect(initLang()).toBe('en');
    browserLanguages('en-US');
    setLang('de');
    expect(initLang()).toBe('de');
  });

  it('ignores a stored value that is not exactly de or en', () => {
    browserLanguages('en-US');
    for (const bad of ['DE', 'de ', 'fr', '', 'en-US', '{"lang":"de"}', 'null']) {
      localStorage.setItem(LANG_KEY, bad);
      expect(initLang(), JSON.stringify(bad)).toBe('en');
    }
    browserLanguages('de-DE');
    localStorage.setItem(LANG_KEY, 'EN');
    expect(initLang()).toBe('de');
  });

  it('keeps working when the store is unavailable', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new DOMException('blocked', 'SecurityError');
    });
    const set = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new DOMException('full', 'QuotaExceededError');
    });
    browserLanguages('en-US');
    expect(() => initLang()).not.toThrow();
    expect(lang()).toBe('en');
    expect(() => setLang('de')).not.toThrow();
    expect(set).toHaveBeenCalled();
    expect(lang()).toBe('de');
    expect(document.documentElement.lang).toBe('de');
    expect(t.navDashboard).toBe(de.navDashboard);
  });
});
