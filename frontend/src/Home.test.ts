import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/svelte';
import Home from './Home.svelte';
import { FakeEventSource } from './lib/eventsource.fake';
import { fmt, msg, t } from './lib/i18n';
import {
  complianceAlert,
  criticalTemperatureAlert,
  genericMetrics,
  piMetrics,
  piSnapshot,
  relevantStorageAlert,
} from './lib/metrics.fixtures';
import type { MetricsResponse, Snapshot } from './lib/metrics';

beforeEach(() => {
  FakeEventSource.reset();
  vi.stubGlobal('EventSource', FakeEventSource);
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  Object.defineProperty(document, 'hidden', { configurable: true, value: false });
});

function stubMetrics(body: MetricsResponse | null, status = 200) {
  const fetchFn =
    body === null
      ? vi.fn().mockRejectedValue(new TypeError('offline'))
      : vi.fn().mockImplementation(async () => new Response(JSON.stringify(body), { status }));
  vi.stubGlobal('fetch', fetchFn);
  return fetchFn;
}

/** Renders Home on a device that answers the one-shot request, and opens the stream. */
async function renderLive(body: MetricsResponse = piMetrics) {
  stubMetrics(body);
  render(Home);
  await screen.findByTestId('home');
  FakeEventSource.last.open();
  return FakeEventSource.last;
}

const at = (seconds: number): string => new Date(Date.parse(piSnapshot.collected_at) + seconds * 1000).toISOString();
const snap = (seconds: number, change: (s: Snapshot) => void = () => {}): Snapshot => {
  const s = structuredClone(piSnapshot);
  s.collected_at = at(seconds);
  change(s);
  return s;
};
const text = (testId: string) => screen.getByTestId(testId).textContent?.replace(/\s+/g, ' ').trim();

describe('Home', () => {
  it('shows a loading state until the first values arrive', () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})));
    render(Home);
    expect(screen.getByRole('heading', { level: 1, name: t.homeTitle })).toBeTruthy();
    expect(screen.getByTestId('home-loading').textContent).toBe(t.homeLoading);
    expect(screen.queryByTestId('home')).toBeNull();
  });

  // ORB-DASH-001, AT-008: values come from the device and change without a page reload.
  it('shows host name, board, SoC, OS, uptime, CPU, memory, storage and network, and updates them live', async () => {
    const es = await renderLive();

    expect(text('hostname')).toBe('oarfish-test');
    expect(screen.getByText('Raspberry Pi Zero 2 W Rev 1.0')).toBeTruthy();
    expect(screen.getByText('(BCM2837)')).toBeTruthy();
    expect(screen.getByText('Debian GNU/Linux 13 (trixie)')).toBeTruthy();
    expect(text('uptime')).toBe('1 T 1 Std.');
    expect(text('tier')).toBe(t['tier.compatible']);
    expect(text('cpu-usage')).toBe('12 %');
    expect(text('cpu-frequency')).toBe('600 MHz von 1.000 MHz');
    expect(text('memory-used')).toBe('149,7 MiB von 405,7 MiB');
    expect(text('swap')).toBe('0 B von 512,0 MiB');
    expect(text('temperature-thermal_zone0')).toBe('43,3 °C');
    expect(text('storage-/')).toContain('32 %');
    expect(text('storage-/')).toContain('1,1 GiB von 3,8 GiB belegt');
    expect(text('storage-/boot/firmware')).toContain('10 %');
    expect(text('network-wlan0')).toContain('192.0.2.15');
    expect(text('network-wlan0')).toContain('2001:db8::15');
    expect(text('network-wlan0')).toContain(t['linkState.up']);

    // A value event changes the page in place.
    es.send('metrics', snap(2, (s) => {
      s.cpu.usage_percent = 55.2;
      s.uptime_seconds = 90063;
      s.temperatures[0]!.celsius = 51.25;
      s.cpu.frequency.current_mhz = 1000;
      s.memory.used_bytes = 300 * 1048576;
    }));
    await waitFor(() => expect(text('cpu-usage')).toBe('55 %'));
    expect(text('temperature-thermal_zone0')).toBe('51,3 °C');
    expect(text('cpu-frequency')).toBe('1.000 MHz von 1.000 MHz');
    expect(text('memory-used')).toContain('300,0 MiB');
    expect(location.reload).toBeUndefined; // no navigation took place
  });

  it('computes the transfer rates in the browser from the counters', async () => {
    const es = await renderLive();
    // The one-shot answer is the first sample; no rate can be known yet.
    expect(text('rx-wlan0')).toBe(t.netRateWaiting);

    es.send('metrics', snap(2, (s) => {
      s.network[0]!.rx_bytes = 123456789 + 4096;
      s.network[0]!.tx_bytes = 23456789 + 1024;
    }));
    await waitFor(() => expect(text('rx-wlan0')).toBe('2,0 KiB/s'));
    expect(text('tx-wlan0')).toBe('512 B/s');

    // A counter that goes backwards (interface reset) shows no rate instead of a negative one.
    es.send('metrics', snap(4, (s) => {
      s.network[0]!.rx_bytes = 10;
      s.network[0]!.tx_bytes = 23456789 + 2048;
    }));
    await waitFor(() => expect(text('rx-wlan0')).toBe(t.netRateWaiting));
    expect(text('tx-wlan0')).toBe('512 B/s');
  });

  it('keeps the technical details collapsed (progressive disclosure, ORB-GEN-003)', async () => {
    await renderLive();
    const details = Array.from(document.querySelectorAll('details'));
    expect(details.length).toBeGreaterThanOrEqual(4);
    for (const d of details) expect(d.open).toBe(false);
    // The governor, interface type and file system type are in there, not in the open page.
    expect(document.querySelector('details')!.textContent).toContain('ondemand');
    expect(screen.getByText(t.cpuGovernor, { selector: 'dt' }).closest('details')).not.toBeNull();
    expect(screen.getByText(new RegExp(`${t.storageFsType}: ext4`)).closest('details')).not.toBeNull();
    expect(screen.getByText(msg('netKind.wifi')).closest('details')).not.toBeNull();
  });

  // ORB-NFR-014: the page copes with an unstable connection.
  it('shows a lost connection, marks the values stale, and recovers when fresh data arrives', async () => {
    const es = await renderLive();
    expect(screen.queryByTestId('connection-notice')).toBeNull();
    expect(screen.getByTestId('home').getAttribute('data-stale')).toBe('false');

    es.dropAndRetry();
    await waitFor(() => expect(screen.getByTestId('connection-notice')).toBeTruthy());
    expect(text('connection-notice')).toContain(t.connLost);
    expect(text('connection-notice')).toContain('Letzte Aktualisierung:');
    expect(screen.getByTestId('home').getAttribute('data-stale')).toBe('true');
    expect(screen.getAllByText(t.staleBadge).length).toBeGreaterThanOrEqual(5); // every card says so
    // The old values stay visible, marked as old.
    expect(text('hostname')).toBe('oarfish-test');

    es.open();
    es.send('metrics', snap(2));
    await waitFor(() => expect(screen.queryByTestId('connection-notice')).toBeNull());
    expect(screen.getByTestId('home').getAttribute('data-stale')).toBe('false');
    expect(screen.queryByText(t.staleBadge)).toBeNull();
  });

  it('marks the values stale when the stream is open but silent', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    try {
      stubMetrics(piMetrics);
      render(Home);
      await vi.waitFor(() => expect(screen.queryByTestId('home')).not.toBeNull());
      const es = FakeEventSource.last;
      es.open();
      es.send('metrics', snap(2));
      await vi.advanceTimersByTimeAsync(7000);
      expect(text('connection-notice')).toContain(t.connStale);
      es.send('metrics', snap(4));
      await vi.advanceTimersByTimeAsync(0);
      expect(screen.queryByTestId('connection-notice')).toBeNull();
    } finally {
      vi.useRealTimers();
    }
  });

  it('reconnects by hand after a refused connection', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    try {
      stubMetrics(piMetrics);
      render(Home);
      await vi.waitFor(() => expect(screen.queryByTestId('home')).not.toBeNull());
      FakeEventSource.last.fail(); // for example 503: too many windows
      await vi.advanceTimersByTimeAsync(3000);
      expect(FakeEventSource.instances).toHaveLength(2);
      FakeEventSource.last.open();
      FakeEventSource.last.send('metrics', snap(2));
      await vi.advanceTimersByTimeAsync(0);
      expect(screen.queryByTestId('connection-notice')).toBeNull();
    } finally {
      vi.useRealTimers();
    }
  });

  it('shows an error with a retry button when nothing can be loaded, and recovers', async () => {
    stubMetrics(null);
    render(Home);
    await waitFor(() => expect(FakeEventSource.instances).toHaveLength(1));
    FakeEventSource.last.fail();
    expect(await screen.findByText(t.homeLoadError)).toBeTruthy();

    const fetchFn = stubMetrics(piMetrics);
    await fireEvent.click(screen.getByRole('button', { name: t.homeRetry }));
    expect(await screen.findByTestId('home')).toBeTruthy();
    expect(fetchFn).toHaveBeenCalledTimes(1);
  });

  it('shows what the stream delivers even if the one-shot request failed, with unknown device fields', async () => {
    stubMetrics(null);
    render(Home);
    await waitFor(() => expect(FakeEventSource.instances).toHaveLength(1));
    FakeEventSource.last.open();
    FakeEventSource.last.send('metrics', snap(2));
    expect(await screen.findByTestId('home')).toBeTruthy();
    expect(text('hostname')).toBe('oarfish-test');
    expect(screen.getAllByText(t.unknownValue).length).toBeGreaterThan(0); // board, OS, support
    expect(screen.queryByText(t.homeLoadError)).toBeNull();
  });

  it('never lets an older reading replace a newer one', async () => {
    let resolve!: (r: Response) => void;
    vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>((r) => (resolve = r))));
    render(Home);
    await waitFor(() => expect(FakeEventSource.instances).toHaveLength(1));
    FakeEventSource.last.open();
    FakeEventSource.last.send('metrics', snap(10, (s) => (s.cpu.usage_percent = 77)));
    await screen.findByTestId('home');
    expect(text('cpu-usage')).toBe('77 %');

    resolve(new Response(JSON.stringify(piMetrics))); // taken earlier: 12 %
    await waitFor(() => expect(screen.getByText('Raspberry Pi Zero 2 W Rev 1.0')).toBeTruthy());
    expect(text('cpu-usage')).toBe('77 %');
  });

  // ORB-NFR-009 / ORB-DASH-006: nobody watching, nobody sampling.
  it('closes the stream when the tab is hidden and opens it when the tab is shown', async () => {
    const es = await renderLive();
    Object.defineProperty(document, 'hidden', { configurable: true, value: true });
    document.dispatchEvent(new Event('visibilitychange'));
    expect(es.closed).toBe(true);
    expect(FakeEventSource.instances).toHaveLength(1);

    Object.defineProperty(document, 'hidden', { configurable: true, value: false });
    document.dispatchEvent(new Event('visibilitychange'));
    expect(FakeEventSource.instances).toHaveLength(2);
    expect(FakeEventSource.last.closed).toBe(false);
  });

  it('closes the stream when the page is left', async () => {
    const es = await renderLive();
    cleanup();
    expect(es.closed).toBe(true);
  });

  it('ignores malformed events without crashing and keeps the last good values', async () => {
    const es = await renderLive();
    es.send('alerts', { schema_version: 1, alerts: [criticalTemperatureAlert] });
    await waitFor(() => expect(document.querySelector('.alert-group[data-level="critical"]')).not.toBeNull());

    for (const bad of ['{"broken"', '', 'null', '[]', '{"schema_version":1}', JSON.stringify({ ...piSnapshot, memory: 5 })]) {
      expect(() => es.send('metrics', bad)).not.toThrow();
    }
    es.send('alerts', 'garbage');
    es.send('history', '{"x":');
    expect(text('hostname')).toBe('oarfish-test');
    expect(text('cpu-usage')).toBe('12 %');
    expect(document.querySelector('.alert-group[data-level="critical"]')).not.toBeNull(); // the alerts survived too
    expect(screen.queryByTestId('connection-notice')).toBeNull();

    es.send('metrics', snap(2, (s) => (s.cpu.usage_percent = 40)));
    await waitFor(() => expect(text('cpu-usage')).toBe('40 %'));
  });
});

describe('Home alerts', () => {
  // ORB-DASH-005
  it('shows alerts first, grouped by level, each with cause, impact and next step', async () => {
    const es = await renderLive();
    es.send('alerts', { schema_version: 1, alerts: [criticalTemperatureAlert, relevantStorageAlert, complianceAlert] });

    const group = (level: string) => document.querySelector(`.alert-group[data-level="${level}"]`) as HTMLElement;
    await waitFor(() => expect(group('critical')).not.toBeNull());
    const headings = Array.from(document.querySelectorAll('.alert-list h3')).map((h) => h.textContent?.replace(/\s+/g, ' ').trim());
    expect(headings).toEqual([`${t['level.critical']} (1)`, `${t['level.relevant']} (1)`, `${t['level.informational']} (1)`]);

    const critical = within(group('critical'));
    expect(critical.getByText(fmt('alert.cpu_temperature.critical.cause', { celsius: 83, threshold: 80 }))).toBeTruthy();
    expect(critical.getByText(t['alert.cpu_temperature.critical.impact'])).toBeTruthy();
    expect(critical.getByText(t['alert.cpu_temperature.critical.next'])).toBeTruthy();
    for (const label of [t.alertCause, t.alertImpact, t.alertNext]) expect(critical.getByText(label)).toBeTruthy();

    const relevant = within(group('relevant'));
    expect(relevant.getByText('Das Dateisystem / ist zu 88,4 % belegt. Über 85 % wird es gemeldet.')).toBeTruthy();
    expect(within(group('informational')).getByText(t['alert.support_compatible.cause'])).toBeTruthy();

    // The alerts come before every other part of the page.
    const sections = Array.from(document.querySelectorAll('.home section')).map((s) => s.getAttribute('aria-labelledby'));
    expect(sections[0]).toBe('alerts-heading');
  });

  it('says so when there is nothing to report, and when the list changes', async () => {
    const es = await renderLive({ ...piMetrics, alerts: [] });
    expect(screen.getByText(t.alertsNone)).toBeTruthy();
    es.send('alerts', { schema_version: 1, alerts: [relevantStorageAlert] });
    await waitFor(() => expect(screen.queryByText(t.alertsNone)).toBeNull());
    es.send('alerts', { schema_version: 1, alerts: [] });
    await waitFor(() => expect(screen.getByText(t.alertsNone)).toBeTruthy());
  });

  // ORB-NFR-011: only the alert list is a live region; the two-second value updates are silent.
  it('announces only the alert list, not the metric updates', async () => {
    const es = await renderLive();
    const regions = document.querySelectorAll('[aria-live], [role="status"], [role="alert"], [role="log"], [role="marquee"], [role="timer"]');
    expect(regions).toHaveLength(1);
    const region = regions[0] as HTMLElement;
    expect(region.classList.contains('alert-list')).toBe(true);
    expect(region.getAttribute('aria-live')).toBe('polite');
    expect(region.contains(screen.getByTestId('cpu-usage'))).toBe(false);
    expect(region.contains(screen.getByTestId('hostname'))).toBe(false);

    es.send('metrics', snap(2, (s) => (s.cpu.usage_percent = 60)));
    await waitFor(() => expect(text('cpu-usage')).toBe('60 %'));
    expect(document.querySelectorAll('[aria-live], [role="status"], [role="alert"]')).toHaveLength(1);
  });

  it('uses only native interactive elements, so everything works from the keyboard (ORB-NFR-011)', async () => {
    await renderLive();
    for (const el of document.querySelectorAll('[tabindex]')) expect(Number(el.getAttribute('tabindex'))).toBeLessThanOrEqual(0);
    expect(document.querySelectorAll('[onclick], [role="button"]')).toHaveLength(0);
    // Summaries are focusable and toggle with the keyboard by the browser; the link to the Device page is a real link.
    expect(document.querySelectorAll('summary').length).toBeGreaterThan(0);
    expect(screen.getByRole('link', { name: t.sumDeviceLink }).getAttribute('href')).toBe('#/device');
    // Every progress bar has a name; every sparkline is a described image.
    for (const p of document.querySelectorAll('progress')) expect(p.getAttribute('aria-label')).toBeTruthy();
    for (const s of document.querySelectorAll('svg')) {
      expect(s.getAttribute('role')).toBe('img');
      expect(s.getAttribute('aria-label')).toBeTruthy();
    }
  });
});

describe('Home on devices with less information', () => {
  // ORB-HW-007, ORB-NFR-015: Generic Mode is clearly marked and the core dashboard still works.
  it('shows Generic Mode, unknown values, and no board-specific claims', async () => {
    await renderLive(genericMetrics);
    expect(text('tier')).toBe(t['tier.generic']);
    expect(screen.getByText('Synthetic Board A1')).toBeTruthy();
    expect(screen.getByText(t['alert.support_generic.cause'])).toBeTruthy();
    expect(screen.getByText(t['alert.support_generic.impact'])).toBeTruthy();
    expect(screen.getByTestId('cpu-usage')).toBeTruthy();
    expect(text('cpu-frequency')).toBe(t.unavailableValue);
    expect(text('swap')).toBe(t.memSwapNone);
    expect(screen.queryByText(t['tier.verified'])).toBeNull();
  });

  it('shows empty states for missing sensors, storage and interfaces', async () => {
    await renderLive(genericMetrics);
    expect(text('no-sensors')).toBe(t.tempNone);
    expect(text('no-storage')).toBe(t.storageNone);
    expect(text('no-network')).toBe(t.netNone);
    expect(screen.getAllByText(t.sparkNoData).length).toBeGreaterThanOrEqual(2); // no history yet
  });

  it('shows empty states when sensors disappear from a running device', async () => {
    const es = await renderLive();
    expect(text('temperature-thermal_zone0')).toBe('43,3 °C');
    es.send('metrics', snap(2, (s) => (s.temperatures = [])));
    await waitFor(() => expect(text('no-sensors')).toBe(t.tempNone));
  });

  it('labels every temperature source and shows an unreadable one as unavailable', async () => {
    const es = await renderLive();
    es.send('metrics', snap(2, (s) => {
      s.temperatures = [
        { id: 'hwmon0_temp1', name: 'Composite', source: 'hwmon', cpu: false, celsius: 38.9 },
        { id: 'hwmon1_temp1', name: 'cpu', source: 'hwmon', cpu: true, celsius: null },
      ];
    }));
    await waitFor(() => expect(text('temperature-hwmon0_temp1')).toBe('38,9 °C'));
    expect(text('temperature-hwmon1_temp1')).toBe(t.unavailableValue);
    expect(screen.getByText('Composite', { selector: 'span' })).toBeTruthy();
    expect(screen.getByText(t.tempSoc)).toBeTruthy();
  });

  it('colours storage usage by threshold', async () => {
    const es = await renderLive();
    es.send('metrics', snap(2, (s) => {
      s.storage = [
        { mount_point: '/', fs_type: 'ext4', total_bytes: 100, used_bytes: 50, available_bytes: 50, used_percent: 50 },
        { mount_point: '/a', fs_type: 'ext4', total_bytes: 100, used_bytes: 90, available_bytes: 10, used_percent: 90 },
        { mount_point: '/b', fs_type: 'ext4', total_bytes: 100, used_bytes: 97, available_bytes: 3, used_percent: 97 },
      ];
    }));
    await waitFor(() => expect(screen.getByTestId('storage-/b')).toBeTruthy());
    const level = (m: string) => screen.getByTestId(`storage-${m}`).querySelector('progress')!.getAttribute('data-level');
    expect([level('/'), level('/a'), level('/b')]).toEqual(['ok', 'relevant', 'critical']);
  });
});
