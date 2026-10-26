import { describe, expect, it } from 'vitest';
import { de, en, msg, t } from './i18n';

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
});
