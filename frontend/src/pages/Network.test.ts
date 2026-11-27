// The page Netzwerk (task N1, ORB-NET-001, ORB-NET-002 in part; docs/UI.md,
// "Network page"): interfaces with addresses and prefixes, the Wi-Fi signal,
// the default routes and the name servers, both languages, the empty and
// failed states, live updates, and no SSID and no hardware address.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen, within } from '@testing-library/svelte';
import Network from './Network.svelte';
import { FakeEventSource } from '../lib/eventsource.fake';
import { installFakes, metricsWith, snapshotAt, stubMetrics, textOf } from '../lib/harness.fake';
import { de, en, t } from '../lib/i18n';
import { setLang } from '../lib/lang.svelte';
import { behindStubRouting, ethernetOnly, piMetrics, wifiNoSignal } from '../lib/metrics.fixtures';
import { signalWord } from '../lib/network';
import type { MetricsResponse } from '../lib/metrics';

beforeEach(installFakes);
afterEach(() => {
  cleanup();
  setLang('de');
  vi.unstubAllGlobals();
});

const items = (testid: string) => within(screen.getByTestId(testid)).getAllByRole('listitem').map((li) => li.textContent?.trim()).join(' ');

async function open(body: MetricsResponse = piMetrics): Promise<FakeEventSource> {
  stubMetrics(body);
  render(Network);
  await screen.findByTestId('interfaces');
  FakeEventSource.last.open();
  return FakeEventSource.last;
}

describe('network page', () => {
  it('shows an interface with kind, link, addresses with prefix, signal and counters, in German', async () => {
    await open();
    expect(screen.getByRole('heading', { level: 1, name: de.networkTitle })).toBeTruthy();
    const nic = screen.getByTestId('iface-wlan0');
    expect(within(nic).getByRole('heading', { level: 3 }).textContent?.trim()).toBe('wlan0');
    expect(items('addresses-wlan0')).toBe('192.0.2.15/24 2001:db8::15/64');
    expect(within(nic).getByText('WLAN')).toBeTruthy();
    expect(textOf('signal-wlan0')).toBe('-52 dBm, Linkqualität 58 (gut)');
    expect(within(nic).getByText(de.netTotals)).toBeTruthy();
    expect(textOf('route-ipv4')).toBe('192.0.2.1 über wlan0');
    expect(textOf('dns-servers')).toBe('192.0.2.53, 2001:db8::53');
  });

  it('shows the same in English', async () => {
    setLang('en');
    await open();
    expect(screen.getByRole('heading', { level: 1, name: en.networkTitle })).toBeTruthy();
    expect(textOf('signal-wlan0')).toBe('-52 dBm, link quality 58 (good)');
    expect(screen.getByRole('heading', { level: 2, name: en.netPageInterfaces })).toBeTruthy();
  });

  it('Ethernet only: no signal row, the link speed, both families with prefix', async () => {
    await open(metricsWith((m) => (m.network = structuredClone(ethernetOnly))));
    expect(screen.queryByTestId('signal-eth0')).toBeNull();
    expect(items('addresses-eth0')).toBe('198.51.100.40/24 2001:db8:0:1::40/64');
    expect(within(screen.getByTestId('iface-eth0')).getByText('Ethernet')).toBeTruthy();
  });

  it('Wi-Fi without a signal line says Nicht verfügbar; an interface without addresses says so', async () => {
    await open(metricsWith((m) => (m.network = structuredClone(wifiNoSignal))));
    expect(textOf('signal-wlan0')).toContain(t.unavailableValue);
    expect(items('addresses-wlan1')).toBe(t.netNoAddress);
  });

  it('an answer from a service before N1 (no prefixes, no signal) shows the plain addresses', async () => {
    await open(
      metricsWith((m) => {
        for (const n of m.network) {
          delete n.ipv4_cidr;
          delete n.ipv6_cidr;
          delete n.signal;
        }
      }),
    );
    expect(items('addresses-wlan0')).toBe('192.0.2.15 2001:db8::15');
    expect(textOf('signal-wlan0')).toContain(t.unavailableValue);
  });

  it('the servers behind a local resolver carry the sentence that they are used through it', async () => {
    await open(metricsWith((m) => (m.routing = structuredClone(behindStubRouting))));
    expect(textOf('dns-servers')).toBe('198.51.100.53, 2001:db8:2::53 (genutzt über den Resolver auf dem Gerät)');
    setLang('en');
    await Promise.resolve();
    expect(textOf('dns-servers')).toContain('used through the resolver on the device');
  });

  it('no network interface: the empty state', async () => {
    await open(metricsWith((m) => (m.network = [])));
    expect(textOf('no-network')).toBe(t.netNone);
  });

  it('loading, and a failed first read with the connection down: the states', async () => {
    stubMetrics(null);
    render(Network);
    expect(screen.getByRole('status').textContent).toContain(t.netPageLoading);
    await vi.waitFor(() => expect(FakeEventSource.instances.length).toBeGreaterThan(0));
    FakeEventSource.last.fail();
    expect((await screen.findByRole('alert')).textContent).toContain(t.netPageLoadError);
  });

  it('follows the stream: a new signal replaces the old one', async () => {
    const es = await open();
    es.send('metrics', snapshotAt(2, (s) => (s.network[0]!.signal = { level_dbm: -75, link_quality: 30 })));
    await vi.waitFor(() => expect(textOf('signal-wlan0')).toBe('-75 dBm, Linkqualität 30 (schwach)'));
  });

  it('shows no hardware address and no SSID', async () => {
    await open();
    const page = screen.getByTestId('network-page').textContent ?? '';
    expect(page).not.toMatch(/[0-9a-f]{2}(:[0-9a-f]{2}){5}/i);
    expect(page).not.toMatch(/SSID/i);
  });
});

describe('signal word (proposal: good from -60 dBm, fair from -70 dBm, weak below)', () => {
  it('maps the thresholds', () => {
    expect([-30, -60, -61, -70, -71, -100].map(signalWord)).toEqual(['good', 'good', 'fair', 'fair', 'weak', 'weak']);
    expect(signalWord(null)).toBeNull();
  });
});
