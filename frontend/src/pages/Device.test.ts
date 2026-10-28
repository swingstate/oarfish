import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/svelte';
import Device from './Device.svelte';
import { genericArm64, piZero2W, unsupportedX86 } from '../lib/device.fixtures';
import { msg, t } from '../lib/i18n';

afterEach(() => vi.unstubAllGlobals());

function stubDevice(body: unknown, status = 200) {
  const fetchFn = vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status }));
  vi.stubGlobal('fetch', fetchFn);
  return fetchFn;
}

describe('Gerät page', () => {
  it('shows model, SoC, OS, kernel, sensors, boot medium and support for a Compatible Raspberry Pi', async () => {
    stubDevice(piZero2W);
    render(Device);

    expect(await screen.findByText('Raspberry Pi Zero 2 W Rev 1.0')).toBeTruthy();
    expect(screen.getByRole('heading', { level: 1, name: t.deviceTitle })).toBeTruthy();
    expect(screen.getByText('BCM2837')).toBeTruthy();
    expect(screen.getByText('Debian GNU/Linux 13 (trixie)')).toBeTruthy();
    expect(screen.getByText('6.18.50+rpt-rpi-v8')).toBeTruthy();
    expect(screen.getByText('406 MiB')).toBeTruthy();
    expect(screen.getByTestId('cpu-temperature').textContent?.trim()).toBe('43,3 °C');
    expect(screen.getByText(t['bootMedium.mmc'])).toBeTruthy();
    expect(screen.getByText(msg('adapter.raspberry_pi_os'))).toBeTruthy();

    // Supportstatus: tier and its explanation, with the known limitations.
    expect(screen.getByTestId('tier').textContent).toBe(t['tier.compatible']);
    expect(screen.getByText(t['support.compatible.profile_not_verified'])).toBeTruthy();
    expect(screen.getByText(t['limitation.memory_512mb'])).toBeTruthy();
    expect(screen.getByText(t['limitation.evidence_install_only'])).toBeTruthy();
    // It is never presented as verified, and it is not generic mode.
    expect(screen.queryByText(t['tier.verified'])).toBeNull();
    expect(screen.queryByText(new RegExp(t.genericNoticeTitle))).toBeNull();
  });

  it('lists every capability with its state, and keeps the technical source behind a disclosure', async () => {
    stubDevice(genericArm64);
    render(Device);
    await screen.findByText('Synthetic Board A1');

    const items = document.querySelectorAll('.capabilities li');
    expect(items.length).toBe(genericArm64.capabilities.length);
    const journald = Array.from(items).find((li) => li.textContent?.includes(t['capability.journald']))!;
    expect(journald.getAttribute('data-available')).toBe('false');
    expect(within(journald as HTMLElement).getByText(t.capUnavailable)).toBeTruthy();
    expect(within(journald as HTMLElement).getByText(t['capabilityReason.not_found'], { exact: false })).toBeTruthy();
    // The source is inside <details>, closed by default (progressive disclosure).
    const details = journald.querySelector('details')!;
    expect(details.open).toBe(false);
    expect(details.textContent).toContain('/run/systemd/journal/socket');
  });

  it('shows Generic Mode with a clear notice, unknown values, and no board-specific action', async () => {
    stubDevice(genericArm64);
    render(Device);
    await screen.findByText('Synthetic Board A1');

    expect(screen.getByTestId('tier').textContent).toBe(t['tier.generic']);
    const notice = screen.getByText(t['support.generic.no_board_profile'], { exact: false });
    expect(notice.textContent).toContain(t.genericNoticeTitle);
    expect(screen.getByText(t['limitation.generic_mode'])).toBeTruthy();
    // Unknown values are labelled, not blank.
    expect(screen.getAllByText(t.unknownValue).length).toBeGreaterThan(0);
    expect(screen.getByTestId('cpu-temperature').textContent?.trim()).toBe(t.unavailableValue);
    expect(t['bootMedium.unknown']).toBe(t.unknownValue); // boot medium "unknown" is one of the labelled unknowns
    // The only control is "reload"; nothing can change the board.
    const buttons = screen.getAllByRole('button');
    expect(buttons.map((b) => b.textContent?.trim())).toEqual([t.deviceReload]);
    expect(document.querySelector('[data-board-action]')).toBeNull();
  });

  it('shows Unsupported with a diagnostic hint', async () => {
    stubDevice(unsupportedX86);
    render(Device);
    await screen.findByText(t['tier.unsupported']);

    const notice = screen.getByText(t['support.unsupported.not_arm64'], { exact: false });
    expect(notice.textContent).toContain(t.unsupportedNoticeTitle);
    expect(screen.getByText('amd64')).toBeTruthy();
    expect(screen.getAllByRole('button')).toHaveLength(1);
  });

  it('shows an error and can retry when the API is not reachable', async () => {
    const fetchFn = vi.fn().mockRejectedValueOnce(new TypeError('offline')).mockResolvedValue(
      new Response(JSON.stringify(piZero2W)),
    );
    vi.stubGlobal('fetch', fetchFn);
    render(Device);

    expect((await screen.findByRole('alert')).textContent).toBe(t.deviceError);
    await fireEvent.click(screen.getByRole('button', { name: t.deviceReload }));
    expect(await screen.findByText('Raspberry Pi Zero 2 W Rev 1.0')).toBeTruthy();
    expect(fetchFn).toHaveBeenCalledTimes(2);
  });

  it('shows a value the translations do not know as its key, not blank', async () => {
    stubDevice({ ...piZero2W, support: { ...piZero2W.support, limitations: ['brand_new_limitation'] } });
    render(Device);
    expect(await screen.findByText('limitation.brand_new_limitation')).toBeTruthy();
  });
});
