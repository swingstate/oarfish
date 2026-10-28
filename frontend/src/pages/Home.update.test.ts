// ORB-NFR-006 / ORB-NFR-011: a new sample changes values in place. It does not rebuild the
// page and nothing animates on each update.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/svelte';
import Home from './Home.svelte';
import { installFakes, openHome, snapshotAt, textOf } from '../lib/harness.fake';
import { criticalTemperatureAlert } from '../lib/metrics.fixtures';

beforeEach(installFakes);
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('Home updates', () => {
  it('updates a sample in place: no element is added or removed, and the cards stay the same nodes', async () => {
    const es = await openHome(() => render(Home));
    const cards = Array.from(document.querySelectorAll('section.card'));
    const storage = screen.getByTestId('storage-/');
    const seen: MutationRecord[] = [];
    const observer = new MutationObserver((records) => seen.push(...records));
    observer.observe(document.body, { subtree: true, childList: true, characterData: true, attributes: true });

    es.send('metrics', snapshotAt(2, (s) => {
      s.cpu.usage_percent = 33;
      s.uptime_seconds = 90063;
    }));
    await waitFor(() => expect(textOf('cpu-usage')).toBe('33 %'));
    await Promise.resolve();
    observer.disconnect();

    expect(seen.filter((r) => r.type === 'childList' && (r.addedNodes.length > 0 || r.removedNodes.length > 0)).filter((r) => [...r.addedNodes, ...r.removedNodes].some((n) => n.nodeType === 1))).toEqual([]);
    expect(Array.from(document.querySelectorAll('section.card'))).toEqual(cards);
    expect(screen.getByTestId('storage-/')).toBe(storage);
    // Only a handful of text and attribute changes, not a rebuild of the page.
    expect(seen.length).toBeLessThan(40);
  });

  it('leaves the sparklines alone when only the alerts change', async () => {
    const es = await openHome(() => render(Home));
    const paths = Array.from(document.querySelectorAll('svg[role="img"] path'));
    const before = paths.map((p) => p.getAttribute('d'));
    expect(before.length).toBeGreaterThan(0);
    const seen: MutationRecord[] = [];
    const observer = new MutationObserver((records) => seen.push(...records));
    for (const p of paths) observer.observe(p, { attributes: true });
    es.send('alerts', { schema_version: 1, alerts: [criticalTemperatureAlert] });
    await waitFor(() => expect(document.querySelector('.alert-group')).not.toBeNull());
    await Promise.resolve();
    observer.disconnect();
    expect(seen).toEqual([]);
    expect(paths.map((p) => p.getAttribute('d'))).toEqual(before);
  });

  it('redraws a sparkline only when a new sample arrives', async () => {
    const es = await openHome(() => render(Home));
    const d = () => document.querySelector('svg[role="img"] path')!.getAttribute('d');
    const before = d();
    es.send('metrics', snapshotAt(2, (s) => (s.cpu.usage_percent = 90)));
    await waitFor(() => expect(d()).not.toBe(before));
  });
});

describe('Motion', () => {
  function cssFiles(dir: string, out: string[] = []): string[] {
    for (const name of readdirSync(dir)) {
      const path = join(dir, name);
      if (statSync(path).isDirectory()) cssFiles(path, out);
      else if (/\.(svelte|css)$/.test(name)) out.push(path);
    }
    return out;
  }
  const src = join(import.meta.dirname, '..');

  it('has no keyframe animation and no transition on a value', () => {
    for (const file of cssFiles(src)) {
      const text = readFileSync(file, 'utf8');
      expect(text, file).not.toMatch(/@keyframes|animation\s*:(?!\s*none)|animation-name/);
      for (const m of text.matchAll(/transition\s*:([^;]+);/g)) {
        // Only hover and open/close effects may transition, and only with a token duration.
        expect(m[1], file).toMatch(/var\(--duration-/);
        expect(m[1], file).toMatch(/background-color|transform/);
      }
      expect(text, file).not.toMatch(/transition[^;]*\b(width|height|opacity|stroke)/);
    }
  });
});
