import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import App from './App.svelte';
import { FakeEventSource } from './lib/eventsource.fake';
import { t } from './lib/i18n';
import { piMetrics } from './lib/metrics.fixtures';

beforeEach(() => {
  FakeEventSource.reset();
  vi.stubGlobal('EventSource', FakeEventSource);
});
afterEach(() => {
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
