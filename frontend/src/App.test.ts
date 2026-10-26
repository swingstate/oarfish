import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import App from './App.svelte';
import { t } from './lib/i18n';

afterEach(() => {
  vi.unstubAllGlobals();
  location.hash = '';
});

describe('App', () => {
  it('shows the running state when /healthz answers ok', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(new Response(JSON.stringify({ status: 'ok' }))),
    );
    render(App);

    expect(screen.getByRole('heading', { level: 1, name: 'Oarfish' })).toBeTruthy();
    expect(await screen.findByText(t.ok)).toBe(screen.getByRole('status'));
  });

  it('shows unreachable when /healthz fails', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('offline')));
    render(App);

    expect(await screen.findByText(t.unreachable)).toBeTruthy();
  });

  it('has a navigation with the status view and the device page', async () => {
    const device = await import('./lib/device.fixtures');
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) =>
        new Response(JSON.stringify(url === '/healthz' ? { status: 'ok' } : device.piZero2W)),
      ),
    );
    render(App);

    const nav = screen.getByRole('navigation', { name: t.navLabel });
    expect(nav).toBeTruthy();
    expect(screen.getByRole('link', { name: t.navStatus }).getAttribute('aria-current')).toBe('page');
    expect(screen.getByRole('link', { name: t.navDevice }).getAttribute('aria-current')).toBeNull();

    await fireEvent.click(screen.getByRole('link', { name: t.navDevice }));
    // jsdom fires hashchange asynchronously
    expect(await screen.findByRole('heading', { level: 1, name: t.deviceTitle })).toBeTruthy();
    expect(await screen.findByText('Raspberry Pi Zero 2 W Rev 1.0')).toBeTruthy();
    expect(screen.getByRole('link', { name: t.navDevice }).getAttribute('aria-current')).toBe('page');
  });

  it('opens the device page directly from the address', async () => {
    location.hash = '#/device';
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('offline')));
    render(App);
    expect(screen.getByRole('heading', { level: 1, name: t.deviceTitle })).toBeTruthy();
  });
});
