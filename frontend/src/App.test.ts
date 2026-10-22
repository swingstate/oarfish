import { afterEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import App from './App.svelte';
import { t } from './lib/i18n';

afterEach(() => vi.unstubAllGlobals());

describe('App', () => {
  it('shows the running state when /healthz answers ok', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(new Response(JSON.stringify({ status: 'ok' }))),
    );
    render(App);

    expect(screen.getByRole('heading', { level: 1, name: 'Orbit' })).toBeTruthy();
    expect(await screen.findByText(t.ok)).toBe(screen.getByRole('status'));
  });

  it('shows unreachable when /healthz fails', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('offline')));
    render(App);

    expect(await screen.findByText(t.unreachable)).toBeTruthy();
  });
});
