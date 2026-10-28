import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import App from './App.svelte';
import { FakeEventSource } from './lib/eventsource.fake';
import { t } from './lib/i18n';
import { piMetrics } from './lib/metrics.fixtures';

beforeEach(() => {
  FakeEventSource.reset();
  vi.stubGlobal('EventSource', FakeEventSource);
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  location.hash = '';
});

async function stubApi() {
  const device = await import('./lib/device.fixtures');
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => new Response(JSON.stringify(url === '/api/v1/metrics' ? piMetrics : device.piZero2W))),
  );
}

describe('App', () => {
  it('opens on Home and replaces the old status view', async () => {
    await stubApi();
    render(App);
    expect(screen.getByRole('heading', { level: 1, name: t.homeTitle })).toBeTruthy();
    expect(await screen.findByTestId('hostname')).toBeTruthy();
    expect(screen.queryByText('Oarfish-Dienst')).toBeNull();
  });

  it('has a navigation with Home and the device page', async () => {
    await stubApi();
    render(App);

    const nav = screen.getByRole('navigation', { name: t.navLabel });
    expect(nav).toBeTruthy();
    expect(screen.getByRole('link', { name: t.navHome }).getAttribute('aria-current')).toBe('page');
    expect(screen.getByRole('link', { name: t.navDevice }).getAttribute('aria-current')).toBeNull();
    expect(FakeEventSource.instances.length).toBeLessThanOrEqual(1);

    await fireEvent.click(screen.getByRole('link', { name: t.navDevice }));
    // jsdom fires hashchange asynchronously
    expect(await screen.findByRole('heading', { level: 1, name: t.deviceTitle })).toBeTruthy();
    expect(await screen.findByText('Raspberry Pi Zero 2 W Rev 1.0')).toBeTruthy();
    expect(screen.getByRole('link', { name: t.navDevice }).getAttribute('aria-current')).toBe('page');
    // Leaving Home closes its stream: nobody watches, nothing is sampled.
    expect(FakeEventSource.instances.every((es) => es.closed)).toBe(true);

    await fireEvent.click(screen.getByRole('link', { name: t.navHome }));
    expect(await screen.findByRole('heading', { level: 1, name: t.homeTitle })).toBeTruthy();
    expect(FakeEventSource.last.closed).toBe(false);
  });

  it('opens the device page directly from the address', async () => {
    location.hash = '#/device';
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('offline')));
    render(App);
    expect(screen.getByRole('heading', { level: 1, name: t.deviceTitle })).toBeTruthy();
    expect(FakeEventSource.instances).toHaveLength(0); // the device page needs no stream
  });
});

describe('App shell', () => {
  it('has the landmarks banner, navigation and main, one level-1 heading, and the page title in the tab', async () => {
    await stubApi();
    render(App);
    await screen.findByTestId('hostname');
    expect(screen.getAllByRole('banner')).toHaveLength(1);
    expect(screen.getAllByRole('navigation')).toHaveLength(1);
    expect(screen.getAllByRole('main')).toHaveLength(1);
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1);
    expect(document.title).toBe(`${t.navHome} · ${t.brandName}`);
    // Headings never skip a level: every h3 sits under an h2, every h4 under an h3.
    const levels = Array.from(document.querySelectorAll('h1, h2, h3, h4')).map((h) => Number(h.tagName[1]));
    levels.reduce((previous, level) => {
      expect(level - previous).toBeLessThanOrEqual(1);
      return level;
    }, 0);
  });

  it('keeps exactly one live region in the whole app, the alert list, on Home', async () => {
    await stubApi();
    render(App);
    await screen.findByTestId('hostname');
    expect(document.querySelectorAll('[aria-live], [role="status"], [role="alert"], [role="log"]')).toHaveLength(1);
    expect(document.querySelector('[aria-live]')!.classList.contains('alert-list')).toBe(true);
  });

  it('is operated with native elements only, on both pages', async () => {
    await stubApi();
    render(App);
    await screen.findByTestId('hostname');
    const interactive = (root: ParentNode) => Array.from(root.querySelectorAll('a, button, summary, input, select, textarea, [tabindex], [role="button"], [role="link"]'));
    for (const el of interactive(document)) {
      expect(['A', 'BUTTON', 'SUMMARY', 'MAIN']).toContain(el.tagName);
      if (el.hasAttribute('tabindex')) expect(Number(el.getAttribute('tabindex'))).toBeLessThanOrEqual(0);
    }
    await fireEvent.click(screen.getByRole('link', { name: t.navDevice }));
    await screen.findByText('Raspberry Pi Zero 2 W Rev 1.0');
    for (const el of interactive(document)) expect(['A', 'BUTTON', 'SUMMARY', 'MAIN']).toContain(el.tagName);
    await waitFor(() => expect(document.title).toBe(`${t.navDevice} · ${t.brandName}`));
    expect(document.querySelectorAll('[aria-live]')).toHaveLength(0);
  });

  it('keeps the page when the skip link is used', async () => {
    location.hash = '#/device';
    await stubApi();
    render(App);
    await screen.findByText('Raspberry Pi Zero 2 W Rev 1.0');
    await fireEvent.click(screen.getByText(t.skipLink));
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('main')));
    expect(screen.getByRole('heading', { level: 1, name: t.deviceTitle })).toBeTruthy();
  });
});
