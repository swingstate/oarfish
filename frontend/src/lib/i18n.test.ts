import { describe, expect, it } from 'vitest';
import { de, deApi, deUi, en, enApi, enUi, fmt, has, msg, t } from './i18n';

describe('translations', () => {
  it('English has exactly the keys of German', () => {
    expect(Object.keys(en).sort()).toEqual(Object.keys(de).sort());
  });

  it('has no empty text', () => {
    for (const dict of [de, en]) {
      for (const [key, value] of Object.entries(dict)) {
        expect(value.trim(), key).not.toBe('');
      }
    }
  });

  it('uses German by default', () => {
    expect(t).toBe(de);
  });

  it('looks up dynamic keys and shows an unknown key as itself', () => {
    expect(msg('tier.compatible')).toBe('Kompatibel');
    expect(msg('tier.compatible', en)).toBe('Compatible');
    expect(msg('limitation.not_in_the_dictionary')).toBe('limitation.not_in_the_dictionary');
  });

  it('fills in parameters, formats numbers the German way, and keeps a missing parameter visible', () => {
    expect(fmt('alert.cpu_temperature.relevant.cause', { celsius: 72.34, threshold: 70 })).toBe(
      'Die CPU-Temperatur beträgt 72,3 °C. Ab 70 °C wird sie als erhöht gemeldet.',
    );
    expect(fmt('alert.storage_usage.relevant.cause', { mount: '/data', percent: 88.4, threshold: 85 })).toContain('/data');
    expect(fmt('alert.cpu_temperature.relevant.cause', { celsius: 72 })).toContain('{threshold}');
    expect(fmt('alert.cpu_temperature.relevant.cause', { celsius: 72, threshold: 70 }, en)).toContain('72 °C');
    // A value that contains a placeholder is not interpreted again.
    expect(fmt('alert.storage_usage.relevant.cause', { mount: '{percent}', percent: 1, threshold: 2 })).toContain('Dateisystem {percent} ist');
  });

  it('has the same placeholders in German and English', () => {
    const names = (s: string) => (s.match(/\{\w+\}/g) ?? []).sort();
    for (const key of Object.keys(de) as (keyof typeof de)[]) {
      expect(names(en[key]), key).toEqual(names(de[key]));
    }
  });

  it('has the same two groups in both languages, with no key in both groups', () => {
    expect(Object.keys(enUi).sort()).toEqual(Object.keys(deUi).sort());
    expect(Object.keys(enApi).sort()).toEqual(Object.keys(deApi).sort());
    for (const key of Object.keys(deUi)) expect(key in deApi, key).toBe(false);
    expect(Object.keys(de).length).toBe(Object.keys(deUi).length + Object.keys(deApi).length);
    expect(has('tier.generic')).toBe(true);
    expect(has('tier.not_a_tier')).toBe(false);
    expect(has('constructor')).toBe(false);
  });
});

// Text rules for the state and label texts (docs/UI.md, "Text rules").
describe('UI texts follow the text rules', () => {
  const sentences = (s: string) => s.split(/(?<=[.:])\s+/).filter(Boolean);

  for (const [lang, dict] of [['de', deUi], ['en', enUi]] as const) {
    it(`${lang}: no exclamation mark, no emoji, no marketing filler, no ASCII ellipsis, no double space`, () => {
      for (const [key, value] of Object.entries(dict)) {
        expect(value, key).not.toMatch(/!/);
        expect(value, key).not.toMatch(/\p{Extended_Pictographic}/u);
        expect(value, key).not.toMatch(/\.\.\./);
        expect(value, key).not.toMatch(/\s{2,}|^\s|\s$/);
        expect(value, key).not.toMatch(/\b(bitte|leider|einfach|super|toll|willkommen|please|sorry|simply|welcome|awesome)\b/i);
      }
    });

    it(`${lang}: short sentences, at most 20 words each`, () => {
      for (const [key, value] of Object.entries(dict)) {
        for (const sentence of sentences(value)) {
          expect(sentence.split(/\s+/).length, `${key}: ${sentence}`).toBeLessThanOrEqual(20);
        }
      }
    });
  }

  it('de: one word for one thing', () => {
    const all = Object.values(deUi).join('\n');
    expect(all).not.toMatch(/\bRAM\b|Hauptspeicher/); // Arbeitsspeicher
    expect(all).not.toMatch(/Festplatte|Datenträger|Storage/); // Speicherplatz
    expect(all).not.toMatch(/Warnung|Alarm|Alert|Meldung/); // Hinweis
    expect(all).not.toMatch(/Netzwerkkarte|Adapter ?karte/); // Netzwerk
  });

  it('de: loading, error and empty states are full, plain sentences or phrases', () => {
    for (const key of ['homeLoading', 'homeLoadError', 'homeLoadErrorHint', 'connLost', 'connStale', 'alertsNone', 'tempNone', 'storageNone', 'netNone', 'deviceError'] as const) {
      expect(deUi[key].length, key).toBeGreaterThan(10);
      expect(deUi[key], key).toMatch(/[.…]$/);
    }
  });
});
