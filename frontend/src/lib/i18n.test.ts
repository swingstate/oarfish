import { describe, expect, it } from 'vitest';
import { de, en, fmt, msg, t } from './i18n';

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
});
