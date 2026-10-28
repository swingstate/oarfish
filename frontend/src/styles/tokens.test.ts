import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { contrast, over, parseHex } from '../lib/contrast';
import { pairs, stalePairs } from './contrast-pairs';

const here = import.meta.dirname;
const src = join(here, '..');
const read = (path: string) => readFileSync(path, 'utf8');
const css = read(join(here, 'tokens.css')).replace(/\/\*[\s\S]*?\*\//g, '');

function declarations(block: string): Map<string, string> {
  const map = new Map<string, string>();
  for (const m of block.matchAll(/(--[\w-]+)\s*:\s*([^;]+);/g)) map.set(m[1]!, m[2]!.trim());
  return map;
}

const light = declarations(/:root\s*\{([^}]*)\}/.exec(css)![1]!);
const darkOnly = declarations(/@media \(prefers-color-scheme: dark\)\s*\{\s*:root\s*\{([^}]*)\}/.exec(css)![1]!);
const dark = new Map([...light, ...darkOnly]);
const themes = { light, dark } as const;

const isColour = (value: string) => /^#[0-9a-f]{6}$/i.test(value);
const colourNames = [...light].filter(([, v]) => isColour(v)).map(([k]) => k);
const colour = (theme: Map<string, string>, name: string) => parseHex(theme.get(`--color-${name}`) ?? '#ff00ff');

function sourceFiles(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) sourceFiles(path, out);
    else if (/\.(svelte|css|ts)$/.test(name) && !/\.(test|fixtures|fake)\.ts$/.test(name)) out.push(path);
  }
  return out;
}

describe('design tokens (ORB-NFR-011, WCAG 2.2 AA)', () => {
  it('has a complete dark set for every colour of the light set', () => {
    expect(colourNames.length).toBeGreaterThan(25);
    for (const name of colourNames) {
      expect(darkOnly.has(name), `${name} has no dark value`).toBe(true);
      expect(isColour(darkOnly.get(name)!), `${name} dark is not a hex colour`).toBe(true);
    }
    // Only colours and shadows differ between the themes.
    for (const name of darkOnly.keys()) expect(name.startsWith('--color-') || name.startsWith('--shadow-'), name).toBe(true);
  });

  for (const [themeName, theme] of Object.entries(themes)) {
    it(`meets the contrast minimum for every text and graphic pair in the ${themeName} theme`, () => {
      const failures: string[] = [];
      for (const p of pairs) {
        for (const name of [p.fg, p.bg]) if (!theme.has(`--color-${name}`)) failures.push(`unknown token ${name}`);
        const ratio = contrast(colour(theme, p.fg), colour(theme, p.bg));
        if (ratio < p.min) failures.push(`${p.fg} on ${p.bg}: ${ratio.toFixed(2)} < ${p.min} (${p.use})`);
      }
      expect(failures).toEqual([]);
    });

    it(`keeps stale (dimmed) values above the minimum in the ${themeName} theme`, () => {
      const alpha = Number(theme.get('--opacity-stale'));
      expect(alpha).toBeGreaterThan(0.5);
      expect(alpha).toBeLessThan(1);
      for (const p of stalePairs) {
        const bg = colour(theme, p.bg);
        const ratio = contrast(over(colour(theme, p.fg), bg, alpha), bg);
        expect(ratio, `${p.fg} dimmed on ${p.bg} (${p.use})`).toBeGreaterThanOrEqual(p.min);
      }
    });
  }

  it('is the only place with colour values and defines every token the UI uses', () => {
    const defined = new Set(light.keys());
    const used = new Set<string>();
    for (const file of sourceFiles(src)) {
      if (file.endsWith('tokens.css') || file.endsWith('contrast-pairs.ts')) continue;
      const text = read(file);
      for (const m of text.matchAll(/var\((--[\w-]+)/g)) used.add(m[1]!);
      if (/\.(svelte|css)$/.test(file)) {
        const noComments = text.replace(/\/\*[\s\S]*?\*\//g, '').replace(/<!--[\s\S]*?-->/g, '');
        expect(noComments.match(/#[0-9a-fA-F]{3,8}\b/g) ?? [], `literal colour in ${file}`).toEqual([]);
        expect(noComments.match(/\brgba?\(/g) ?? [], `literal colour in ${file}`).toEqual([]);
      }
    }
    const local = (name: string) => name.startsWith('--tone-') || name === '--meter-fill';
    for (const name of used) if (!local(name)) expect(defined.has(name), `${name} is used but not defined`).toBe(true);
  });

  it('declares both colour schemes, forced-colours support and reduced motion', () => {
    expect(light.get('color-scheme')).toBeUndefined(); // it is a property, not a custom property
    expect(read(join(here, 'tokens.css'))).toMatch(/color-scheme:\s*light dark;/);
    expect(read(join(here, 'base.css'))).toMatch(/@media \(forced-colors: active\)/);
    expect(read(join(here, 'base.css'))).toMatch(/@media \(prefers-reduced-motion: reduce\)/);
    expect(read(join(here, 'tokens.css'))).toMatch(/@media \(prefers-reduced-motion: reduce\)/);
  });

  it('uses system fonts only and makes no external request', () => {
    expect(light.get('--font-sans')).toMatch(/^system-ui/);
    expect(light.get('--font-mono')).toMatch(/^ui-monospace/);
    const html = read(join(src, '..', 'index.html'));
    for (const file of [...sourceFiles(src), join(src, '..', 'index.html')]) {
      const text = file.endsWith('index.html') ? html : read(file);
      expect(text, file).not.toMatch(/@import|@font-face|url\(|https?:\/\/|<link|\.woff/);
    }
  });

  it('keeps the interactive target size at 24 CSS pixels or more', () => {
    expect(light.get('--target-min')).toBe('1.5rem');
  });
});
