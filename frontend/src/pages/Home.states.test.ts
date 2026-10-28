// The designed states of Home (docs/UI.md, "States"). Each state has a test.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen, waitFor, within } from '@testing-library/svelte';
import Home from './Home.svelte';
import { FakeEventSource } from '../lib/eventsource.fake';
import { installFakes, metricsWith, openHome, snapshotAt, stubMetrics, textOf } from '../lib/harness.fake';
import { t } from '../lib/i18n';
import {
  criticalTemperatureAlert,
  genericMetrics,
  genericModeAlert,
  piMetrics,
  relevantStorageAlert,
} from '../lib/metrics.fixtures';

beforeEach(installFakes);
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const open = (body = piMetrics) => openHome(() => render(Home), body);
const indicator = () => screen.getByTestId('connection-indicator');

describe('Home states', () => {
  it('state loading: shows the page header, a loading text and the connecting indicator before any value', () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})));
    render(Home);
    expect(screen.getByRole('heading', { level: 1, name: t.homeTitle })).toBeTruthy();
    expect(textOf('home-loading')).toBe(t.homeLoading);
    expect(indicator().getAttribute('data-state')).toBe('connecting');
    expect(indicator().textContent).toContain(t.connIndicatorConnecting);
    expect(screen.queryByTestId('home')).toBeNull();
    expect(screen.queryByText(t.staleBadge)).toBeNull();
  });

  it('state live: the indicator says Live, nothing is marked old, no notice', async () => {
    await open();
    expect(indicator().getAttribute('data-state')).toBe('live');
    expect(indicator().textContent).toContain(t.connIndicatorLive);
    expect(screen.queryByTestId('connection-notice')).toBeNull();
    expect(screen.queryByText(t.staleBadge)).toBeNull();
  });

  it('state connection lost: values stay, are dimmed and marked Veraltet, and a notice explains', async () => {
    const es = await open();
    es.dropAndRetry();
    await waitFor(() => expect(screen.getByTestId('connection-notice')).toBeTruthy());
    expect(textOf('connection-notice')).toContain(t.connLost);
    expect(screen.getByTestId('home').getAttribute('data-stale')).toBe('true');
    const cards = document.querySelectorAll('section.card');
    expect(cards.length).toBeGreaterThanOrEqual(6);
    for (const card of Array.from(cards).filter((c) => c.getAttribute('aria-labelledby') !== 'alerts-heading')) {
      expect(card.getAttribute('data-stale')).toBe('true');
      expect(within(card as HTMLElement).getByText(t.staleBadge)).toBeTruthy();
    }
    expect(textOf('hostname')).toBe('oarfish-test'); // the old values are still readable
  });

  it('state reconnecting: the indicator says that Oarfish is connecting again', async () => {
    const es = await open();
    es.dropAndRetry();
    await waitFor(() => expect(indicator().getAttribute('data-state')).toBe('reconnecting'));
    expect(indicator().textContent).toContain(t.connIndicatorReconnecting);
  });

  it('state reconnecting without data: shows the connecting text instead of an empty page', async () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})));
    render(Home);
    await waitFor(() => expect(FakeEventSource.instances).toHaveLength(1));
    FakeEventSource.last.dropAndRetry();
    await waitFor(() => expect(indicator().getAttribute('data-state')).toBe('reconnecting'));
    expect(textOf('home-loading')).toBe(t.connConnecting);
  });

  it('state open but silent: the indicator and the notice say that no new values arrive', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    try {
      stubMetrics(piMetrics);
      render(Home);
      await vi.waitFor(() => expect(screen.queryByTestId('home')).not.toBeNull());
      FakeEventSource.last.open();
      FakeEventSource.last.send('metrics', snapshotAt(2));
      await vi.advanceTimersByTimeAsync(7000);
      expect(indicator().textContent).toContain(t.connIndicatorStale);
      expect(textOf('connection-notice')).toContain(t.connStale);
    } finally {
      vi.useRealTimers();
    }
  });

  it('state paused: a hidden tab shows the paused indicator when it returns', async () => {
    await open();
    Object.defineProperty(document, 'hidden', { configurable: true, value: true });
    document.dispatchEvent(new Event('visibilitychange'));
    await waitFor(() => expect(indicator().getAttribute('data-state')).toBe('paused'));
    expect(indicator().textContent).toContain(t.connIndicatorPaused);
    Object.defineProperty(document, 'hidden', { configurable: true, value: false });
  });

  it('state load error: says so with a retry button and a hint', async () => {
    stubMetrics(null);
    render(Home);
    await waitFor(() => expect(FakeEventSource.instances).toHaveLength(1));
    FakeEventSource.last.fail();
    expect(await screen.findByText(t.homeLoadError)).toBeTruthy();
    expect(screen.getByText(t.homeLoadErrorHint)).toBeTruthy();
    expect(screen.getByRole('button', { name: t.homeRetry })).toBeTruthy();
  });

  it('state no alerts: a calm line inside the live region', async () => {
    await open();
    const none = screen.getByText(t.alertsNone);
    expect(none.closest('[aria-live="polite"]')).not.toBeNull();
    expect(document.querySelectorAll('.alert-group')).toHaveLength(0);
  });

  it('state one alert per level: every level has a group, a count, an icon and a short title', async () => {
    const es = await open();
    es.send('alerts', { schema_version: 1, alerts: [criticalTemperatureAlert, relevantStorageAlert, genericModeAlert] });
    await waitFor(() => expect(document.querySelectorAll('.alert-group')).toHaveLength(3));
    const items = Array.from(document.querySelectorAll('.alert-group li'));
    expect(items).toHaveLength(3);
    for (const li of items) {
      expect(li.querySelector('svg[aria-hidden="true"]')).not.toBeNull();
      expect(li.querySelector('h4')?.textContent).toBeTruthy();
      expect(li.querySelectorAll('dt')).toHaveLength(3);
    }
    expect(items.map((li) => li.querySelector('h4')!.textContent)).toEqual([
      t['alertTitle.cpu_temperature'],
      t['alertTitle.storage_usage'],
      t['alertTitle.support_generic'],
    ]);
  });

  it('state alert without a short title: the cause leads and nothing breaks', async () => {
    const es = await open();
    es.send('alerts', {
      schema_version: 1,
      alerts: [{ ...genericModeAlert, id: 'rule_from_a_newer_version' }],
    });
    await waitFor(() => expect(document.querySelectorAll('.alert-group li')).toHaveLength(1));
    expect(document.querySelector('.alert-group li h4')).toBeNull();
    expect(screen.getByText(t['alert.support_generic.cause'])).toBeTruthy();
  });

  it('state no sensors, no storage, no interfaces: each card says so in one plain line', async () => {
    await open(genericMetrics);
    expect(textOf('no-sensors')).toBe(t.tempNone);
    expect(textOf('no-storage')).toBe(t.storageNone);
    expect(textOf('no-network')).toBe(t.netNone);
  });

  it('state unknown values: every missing value is a neutral placeholder, never null, undefined or NaN', async () => {
    const m = metricsWith((x) => {
      x.hostname = null;
      x.uptime_seconds = null;
      x.cpu = { usage_percent: null, cores: null, load: null, frequency: { current_mhz: null, max_mhz: null }, governor: null };
      x.memory = { total_bytes: null, available_bytes: null, used_bytes: null, swap_total_bytes: null, swap_used_bytes: null };
      x.device = { ...x.device, model: null, soc_family: null, os_pretty_name: null, support_tier: null };
      x.temperatures = [{ id: 's1', name: 'x', source: 'hwmon', cpu: true, celsius: null }];
      x.history = { ...x.history, cpu_percent: [], memory_used_percent: [], temperature_celsius: [] };
    });
    await open(m);
    const page = screen.getByTestId('home').textContent ?? '';
    expect(page).not.toMatch(/\b(null|undefined|NaN)\b/);
    expect(textOf('hostname')).toBe(t.unknownValue);
    expect(textOf('uptime')).toBe(t.unknownValue);
    expect(textOf('cpu-usage')).toBe(t.unavailableValue);
    expect(textOf('memory-used')).toBe(t.unavailableValue);
    expect(textOf('temperature-s1')).toBe(t.unavailableValue);
    expect(screen.getAllByText(t.sparkNoData).length).toBe(3);
    // The usage bars exist and have a name even when the value is unknown.
    for (const p of document.querySelectorAll('progress')) expect(p.getAttribute('aria-label')).toBeTruthy();
  });

  it('state Generic Mode: a neutral status badge, the informational alert, no claim about a board', async () => {
    await open(genericMetrics);
    const badge = screen.getByTestId('tier');
    expect(badge.getAttribute('data-tone')).toBe('neutral');
    expect(badge.textContent).toBe(t['tier.generic']);
    expect(document.querySelector('.alert-group[data-level="informational"]')).not.toBeNull();
    expect(document.querySelector('.alert-group[data-level="critical"]')).toBeNull();
  });

  it('state very long host name: one line with an ellipsis, the full name as title, no layout break', async () => {
    const long = 'a-very-long-synthetic-host-name-' + 'x'.repeat(40);
    await open(metricsWith((m) => (m.hostname = long)));
    const host = screen.getByTestId('hostname');
    expect(host.textContent).toBe(long);
    expect(host.getAttribute('title')).toBe(long);
    expect(host.classList.contains('truncate')).toBe(true);
  });

  it('colours the support status by tier, and only a problem is warning or critical', async () => {
    const tone = async (tier: string) => {
      cleanup();
      await open(metricsWith((m) => (m.device.support_tier = tier)));
      return screen.getByTestId('tier').getAttribute('data-tone');
    };
    expect(await tone('verified')).toBe('success');
    expect(await tone('compatible')).toBe('information');
    expect(await tone('community')).toBe('neutral');
    expect(await tone('generic')).toBe('neutral');
    expect(await tone('a_tier_from_the_future')).toBe('neutral');
    expect(await tone('unsupported')).toBe('critical');
  });

  it('shows a link state honestly: up is success, no link and unknown are neutral grey, never a warning', async () => {
    const es = await open();
    es.send('metrics', snapshotAt(2, (s) => {
      const nic = s.network[0]!;
      s.network = [
        { ...nic, name: 'nic-a', state: 'up', link_up: true },
        { ...nic, name: 'nic-b', state: 'down', link_up: false },
        { ...nic, name: 'nic-c', state: 'unknown', link_up: false },
        { ...nic, name: 'nic-d', state: 'a_state_from_the_future', link_up: false },
      ];
    }));
    await waitFor(() => expect(screen.getByTestId('network-nic-d')).toBeTruthy());
    const badge = (name: string) => screen.getByTestId(`network-${name}`).querySelector('.badge')!;
    expect([badge('nic-a'), badge('nic-b'), badge('nic-c'), badge('nic-d')].map((b) => b.getAttribute('data-tone'))).toEqual([
      'success',
      'neutral',
      'neutral',
      'neutral',
    ]);
    expect(badge('nic-b').textContent).toBe('Kein Link');
    expect(badge('nic-c').textContent).toBe(t['linkState.unknown']);
    expect(badge('nic-d').textContent).toBe(t['linkState.unknown']); // not a raw key
  });
});

// A statement about the present is made only while the stream is live. Old values never say "all fine"
// or promise a measurement that cannot come.
describe('Home wording of stale and lost states', () => {
  const liveRegions = () => document.querySelectorAll('[aria-live], [role="status"], [role="alert"], [role="log"]');
  const withBytes = (rx: number, tx: number) => (s: ReturnType<typeof snapshotAt>) => {
    s.network[0]!.rx_bytes = 123456789 + rx;
    s.network[0]!.tx_bytes = 23456789 + tx;
  };

  it('empty alert list: the current text while live, a text about the last reading when lost or reconnecting', async () => {
    const es = await open();
    expect(screen.getByText(t.alertsNone)).toBeTruthy();
    expect(screen.queryByText(t.alertsNoneStale)).toBeNull();
    expect(liveRegions()).toHaveLength(1);

    es.dropAndRetry(); // connection lost, reconnecting
    await waitFor(() => expect(screen.getByText(t.alertsNoneStale)).toBeTruthy());
    expect(screen.queryByText(t.alertsNone)).toBeNull();
    expect(screen.getByText(t.alertsNoneStale).closest('[aria-live="polite"]')).not.toBeNull();
    expect(liveRegions()).toHaveLength(1);

    es.open();
    es.send('metrics', snapshotAt(2));
    await waitFor(() => expect(screen.getByText(t.alertsNone)).toBeTruthy());
    expect(screen.queryByText(t.alertsNoneStale)).toBeNull();
    expect(liveRegions()).toHaveLength(1);
  });

  it('empty alert list: an open but silent stream also says only what was true at the last reading', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    try {
      stubMetrics(piMetrics);
      render(Home);
      await vi.waitFor(() => expect(screen.queryByTestId('home')).not.toBeNull());
      FakeEventSource.last.open();
      FakeEventSource.last.send('metrics', snapshotAt(2));
      await vi.advanceTimersByTimeAsync(0);
      expect(screen.getByText(t.alertsNone)).toBeTruthy();
      await vi.advanceTimersByTimeAsync(7000);
      expect(screen.getByText(t.alertsNoneStale)).toBeTruthy();
      expect(screen.queryByText(t.alertsNone)).toBeNull();
      expect(liveRegions()).toHaveLength(1);
    } finally {
      vi.useRealTimers();
    }
  });

  it('keeps the wording free of an all-clear claim in every stale text', () => {
    for (const text of [t.alertsNoneStale, t.connLost, t.connStale]) expect(text).not.toMatch(/in Ordnung|alles gut|keine Probleme/i);
    expect(t.alertsNoneStale).toContain('letzten Abruf');
  });

  it('network rates: waiting only while live; the last rate when lost; "Nicht verfügbar" when there is none', async () => {
    const es = await open();
    expect(textOf('rx-wlan0')).toBe(t.netRateWaiting); // live, second reading still missing

    es.send('metrics', snapshotAt(2, withBytes(4096, 1024)));
    await waitFor(() => expect(textOf('rx-wlan0')).toBe('2,0 KiB/s')); // live
    expect(textOf('tx-wlan0')).toBe('512 B/s');

    es.dropAndRetry(); // lost: the last rate stays, inside the dimmed, marked card
    await waitFor(() => expect(screen.getByTestId('connection-notice')).toBeTruthy());
    expect(textOf('rx-wlan0')).toBe('2,0 KiB/s');
    expect(screen.getByTestId('network-wlan0').closest('section')!.getAttribute('data-stale')).toBe('true');
    expect(screen.getByTestId('home').textContent).not.toContain(t.netRateWaiting);
  });

  it('network rates: a lost connection before a rate exists shows "Nicht verfügbar", never a promise', async () => {
    const es = await open();
    expect(textOf('rx-wlan0')).toBe(t.netRateWaiting);
    es.dropAndRetry();
    await waitFor(() => expect(screen.getByTestId('connection-notice')).toBeTruthy());
    expect(textOf('rx-wlan0')).toBe(t.unavailableValue);
    expect(textOf('tx-wlan0')).toBe(t.unavailableValue);
    expect(screen.getByTestId('home').textContent).not.toContain(t.netRateWaiting);

    es.open(); // live again, still no second reading: waiting is true again
    await waitFor(() => expect(textOf('rx-wlan0')).toBe(t.netRateWaiting));
  });

  it('network rates: an open but silent stream says "Nicht verfügbar" as well', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    try {
      stubMetrics(piMetrics);
      render(Home);
      await vi.waitFor(() => expect(screen.queryByTestId('home')).not.toBeNull());
      FakeEventSource.last.open();
      await vi.advanceTimersByTimeAsync(7000);
      expect(textOf('rx-wlan0')).toBe(t.unavailableValue);
      expect(screen.getByTestId('home').textContent).not.toContain(t.netRateWaiting);
    } finally {
      vi.useRealTimers();
    }
  });
});
