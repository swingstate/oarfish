// docs/UI.md is what the owner carries into a design tool. It must not fall behind the code.
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { deUi } from './lib/locales/de';
import { enUi } from './lib/locales/en';

const root = join(import.meta.dirname, '..', '..');
const doc = readFileSync(join(root, 'docs', 'UI.md'), 'utf8');
const tokens = readFileSync(join(import.meta.dirname, 'styles', 'tokens.css'), 'utf8');

describe('docs/UI.md matches the code', () => {
  it('lists every design token by name', () => {
    const names = [...tokens.replace(/\/\*[\s\S]*?\*\//g, '').matchAll(/^\s*(--[\w-]+)\s*:/gm)].map((m) => m[1]!);
    expect(names.length).toBeGreaterThan(60);
    for (const name of new Set(names)) expect(doc, name).toContain(`\`${name}\``);
  });

  it('lists every component', () => {
    for (const file of readdirSync(join(import.meta.dirname, 'components')).filter((f) => f.endsWith('.svelte'))) {
      expect(doc, file).toContain(`\`${file.replace('.svelte', '')}\``);
    }
  });

  it('lists every state and label text with its German and English wording', () => {
    for (const key of Object.keys(deUi) as (keyof typeof deUi)[]) {
      expect(doc, key).toContain(`| \`${key}\` | ${deUi[key].replace(/\|/g, '\\|')} | ${enUi[key].replace(/\|/g, '\\|')} |`);
    }
  });

  it('names the two documents it must stay consistent with', () => {
    expect(doc).toContain('DASHBOARD.md');
    expect(doc).toContain('tokens.css');
  });
});
