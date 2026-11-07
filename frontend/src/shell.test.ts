// The shell of U1: grouped navigation (specification 7.2), the language switch,
// the version line, and what the frontend must not call yet.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import { createRawSnippet } from 'svelte';
import App from './App.svelte';
import AppShell from './components/AppShell.svelte';
import { FakeEventSource } from './lib/eventsource.fake';
import { t } from './lib/i18n';
import { initLang, LANG_KEY } from './lib/lang.svelte';
import { piMetrics } from './lib/metrics.fixtures';
import { signedIn } from './lib/session.fixtures';
import type { NavItem } from './lib/ui';
import { fetchVersion } from './lib/version';
import { routes } from './routes';

const page = createRawSnippet(() => ({ render: () => '<p>Seite</p>' }));

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  Object.defineProperty(navigator, 'languages', { value: ['de-DE', 'de'], configurable: true });
  location.hash = '';
});

/** The lists of the navigation: the group name (or null at the top) and the link texts. */
function lists(nav: HTMLElement) {
  return Array.from(nav.querySelectorAll('ul')).map((ul) => ({
    label: ul.getAttribute('aria-labelledby') ? document.getElementById(ul.getAttribute('aria-labelledby')!)!.textContent : null,
    links: Array.from(ul.querySelectorAll('a')).map((a) => a.textContent?.trim()),
  }));
}

describe('navigation groups (specification 7.2)', () => {
  const items: NavItem[] = [
    { id: 'dashboard', href: '#/', label: 'Dashboard', icon: 'home' },
    { id: 'device', href: '#/device', label: 'Gerät', icon: 'device' },
    { id: 'apps', href: '#/apps', label: 'Apps', icon: 'chip', group: 'workload' },
    { id: 'update', href: '#/update', label: 'Update', icon: 'chip', group: 'system' },
  ];

  it('shows the top entries first, then Workload, then System, each group named', () => {
    render(AppShell, { items, current: 'dashboard', children: page });
    expect(lists(screen.getByRole('navigation', { name: t.navLabel }))).toEqual([
      { label: null, links: ['Dashboard', 'Gerät'] },
      { label: t.navGroupWorkload, links: ['Apps'] },
      { label: t.navGroupSystem, links: ['Update'] },
    ]);
  });

  it('shows no group name for a group without an entry', () => {
    render(AppShell, { items: items.slice(0, 2), current: 'dashboard', children: page });
    expect(screen.queryByText(t.navGroupWorkload)).toBeNull();
    expect(screen.queryByText(t.navGroupSystem)).toBeNull();
    expect(lists(screen.getByRole('navigation', { name: t.navLabel }))).toHaveLength(1);
  });

  it('has an entry only for a page that exists: Dashboard and Gerät in no group, then Update as the first entry of System', () => {
    const nav = routes.filter((r) => r.nav !== false);
    expect(nav.map((r) => [r.id, r.path, r.group])).toEqual([
      ['dashboard', '#/', undefined],
      ['device', '#/device', undefined],
      ['update', '#/update', 'system'],
    ]);
  });
});

function stubApi(healthz: () => Response | Promise<Response>) {
  return import('./lib/device.fixtures').then((device) => {
    vi.stubGlobal('EventSource', FakeEventSource);
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) => {
        if (url === '/healthz') return healthz();
        return new Response(JSON.stringify(url === '/api/v1/session' ? signedIn : url === '/api/v1/metrics' ? piMetrics : device.piZero2W));
      }),
    );
  });
}

describe('language switch in the shell', () => {
  beforeEach(() => FakeEventSource.reset());

  it('switches the whole interface and html lang without a reload, and remembers only the code', async () => {
    await stubApi(() => new Response(JSON.stringify({ status: 'ok', version: 'v1.2.3', pid: 7 })));
    render(App);
    await screen.findByRole('navigation', { name: t.navLabel });
    expect(document.documentElement.lang).toBe('de');

    const group = screen.getByRole('group', { name: 'Sprache' });
    const german = screen.getByRole('button', { name: 'Deutsch' });
    const english = screen.getByRole('button', { name: 'English' });
    expect(group.contains(german) && group.contains(english)).toBe(true);
    expect(german.getAttribute('aria-pressed')).toBe('true');
    expect(english.getAttribute('aria-pressed')).toBe('false');

    await fireEvent.click(english);
    expect(document.documentElement.lang).toBe('en');
    expect(screen.getByRole('navigation', { name: 'Main navigation' })).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Device' })).toBeTruthy();
    expect(screen.getByRole('group', { name: 'Language' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'English' }).getAttribute('aria-pressed')).toBe('true');
    expect(document.title).toBe('Dashboard · Oarfish');
    expect((await screen.findByTestId('version')).textContent).toBe('Version v1.2.3');
    expect(localStorage.getItem(LANG_KEY)).toBe('en');
    expect(localStorage.length).toBe(1);

    await fireEvent.click(screen.getByRole('button', { name: 'Deutsch' }));
    expect(document.documentElement.lang).toBe('de');
    expect(screen.getByRole('link', { name: 'Gerät' })).toBeTruthy();
  });

  it('is reachable with the keyboard: native buttons in tab order, after the navigation', async () => {
    await stubApi(() => new Response('{}', { status: 500 }));
    render(App);
    await screen.findByRole('navigation', { name: t.navLabel });
    const focusable = Array.from(document.querySelectorAll<HTMLElement>('a[href], button:not(:disabled), summary'));
    const names = focusable.map((e) => e.textContent?.trim());
    expect(names.indexOf('Gerät')).toBeLessThan(names.indexOf('Deutsch'));
    expect(names.indexOf('Deutsch') + 1).toBe(names.indexOf('English'));
    for (const name of ['Deutsch', 'English']) {
      const button = screen.getByRole('button', { name });
      expect(button.tagName).toBe('BUTTON');
      expect(button.getAttribute('tabindex')).toBeNull();
    }
  });

  it('starts in English for an English browser and in German after the switch and a reload', async () => {
    Object.defineProperty(navigator, 'languages', { value: ['en-GB'], configurable: true });
    initLang();
    await stubApi(() => new Response('{}', { status: 500 }));
    render(App);
    expect(await screen.findByRole('navigation', { name: 'Main navigation' })).toBeTruthy();
    expect(document.documentElement.lang).toBe('en');

    await fireEvent.click(screen.getByRole('button', { name: 'Deutsch' }));
    cleanup();
    initLang(); // a reload: the stored choice wins over the browser
    render(App);
    expect(await screen.findByRole('navigation', { name: 'Hauptnavigation' })).toBeTruthy();
    expect(document.documentElement.lang).toBe('de');
  });
});

describe('version line', () => {
  beforeEach(() => FakeEventSource.reset());

  it('shows the version from /healthz in the sidebar footer', async () => {
    await stubApi(() => new Response(JSON.stringify({ status: 'ok', version: 'v0.1.0-alpha.7', pid: 1 })));
    render(App);
    const line = await screen.findByTestId('version');
    expect(line.textContent).toBe('Version v0.1.0-alpha.7');
    expect(line.closest('header')).toBeTruthy();
  });

  it('shows nothing, and no error, when the request fails or the answer is not a version', async () => {
    for (const answer of [
      () => new Response('{}', { status: 503 }),
      () => Promise.reject(new TypeError('offline')) as unknown as Response,
      () => new Response('<html>', { status: 200 }),
      () => new Response(JSON.stringify({ version: '<img src=x onerror=1>' })),
      () => new Response(JSON.stringify({ version: 12 })),
    ]) {
      cleanup();
      FakeEventSource.reset();
      await stubApi(answer);
      render(App);
      await screen.findByRole('navigation', { name: t.navLabel });
      await waitFor(() => expect((fetch as ReturnType<typeof vi.fn>).mock.calls.some((c) => c[0] === '/healthz')).toBe(true));
      await new Promise((r) => setTimeout(r, 0));
      expect(screen.queryByTestId('version')).toBeNull();
      expect(screen.queryByRole('alert')).toBeNull();
    }
  });

  it('fetchVersion accepts a plausible version only', async () => {
    const ok = (version: unknown) => (async () => new Response(JSON.stringify({ version }))) as unknown as typeof fetch;
    expect(await fetchVersion(ok('v1.2.3-rc.1+build'))).toBe('v1.2.3-rc.1+build');
    expect(await fetchVersion(ok('a b'))).toBeNull();
    expect(await fetchVersion(ok(''))).toBeNull();
    expect(await fetchVersion(ok('x'.repeat(65)))).toBeNull();
  });
});

describe('what the frontend must not call yet (ADR 0010, A11.1, D-C68; G6.8)', () => {
  function sources(dir: string, out: string[] = []): string[] {
    for (const name of readdirSync(dir)) {
      const path = join(dir, name);
      if (statSync(path).isDirectory()) sources(path, out);
      else if (!/\.test\.ts$/.test(name)) out.push(path);
    }
    return out;
  }

  // Since G6.8 the update page uses the four update routes. The notice route
  // stays unused until a user-visible off switch exists.
  it('never names the update notice route in the frontend sources, not even in parts', () => {
    const root = join(import.meta.dirname, '..');
    const files = [...sources(import.meta.dirname), join(root, 'index.html')];
    let updateClients = 0;
    for (const file of files) {
      const text = readFileSync(file, 'utf8');
      expect(text, file).not.toMatch(/update\/notice|updateNotice|\/notice\b/);
      if (text.includes('/api/v1/update')) {
        updateClients++;
        expect(text, file).not.toMatch(/['"`]notice['"`]/);
      }
    }
    // The client and the development scenario mode name the update routes.
    expect(updateClients).toBeGreaterThan(0);
  });
});
