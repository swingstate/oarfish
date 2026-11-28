// Task N0 (ORB-NET-001): the default routes and the DNS servers on the network card.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen, within } from '@testing-library/svelte';
import Dashboard from './Dashboard.svelte';
import Device from './Device.svelte';
import { installMetricsMock, metricsScenarioNames } from '../dev/metricsMock';
import { installFakes, metricsWith, openDashboard } from '../lib/harness.fake';
import { setLang } from '../lib/lang.svelte';
import { hiddenRouting, noRouteRouting, piRouting, stubRouting, unreadableRouting } from '../lib/metrics.fixtures';
import { isSnapshot, type Routing } from '../lib/metrics';
import { piSnapshot } from '../lib/metrics.fixtures';

beforeEach(installFakes);
afterEach(() => {
  cleanup();
  setLang('de');
  vi.unstubAllGlobals();
});

async function openWith(routing: Routing | undefined) {
  await openDashboard(
    () => render(Dashboard),
    metricsWith((m) => {
      if (routing === undefined) delete m.routing;
      else m.routing = structuredClone(routing);
    }),
  );
}

const row = (testid: string) => screen.getByTestId(testid).textContent?.replace(/\s+/g, ' ').trim();

describe('network card: default route and DNS (task N0)', () => {
  it('shows both default routes and the servers, in German', async () => {
    await openWith(piRouting);
    const section = screen.getByTestId('routing');
    expect(within(section).getByRole('heading', { level: 3 }).textContent).toBe('Standardroute und DNS');
    expect(within(section).getByText('Standardroute IPv4')).toBeTruthy();
    expect(row('route-ipv4')).toBe('192.0.2.1 über wlan0');
    expect(row('route-ipv6')).toBe('fe80::abcd%wlan0 über wlan0');
    expect(row('dns-servers')).toBe('192.0.2.53, 2001:db8::53');
  });

  it('shows the same in English', async () => {
    setLang('en');
    await openWith(piRouting);
    expect(within(screen.getByTestId('routing')).getByRole('heading', { level: 3 }).textContent).toBe('Default route and DNS');
    expect(row('route-ipv4')).toBe('192.0.2.1 via wlan0');
    expect(row('route-ipv6')).toBe('fe80::abcd%wlan0 via wlan0');
    expect(screen.getByText('DNS servers')).toBeTruthy();
  });

  it('shows Keine without a default route and Keiner eingetragen without a server', async () => {
    await openWith(noRouteRouting);
    expect(row('route-ipv4')).toBe('Keine');
    expect(row('route-ipv6')).toBe('Keine');
    expect(row('dns-servers')).toBe('Keiner eingetragen');
    setLang('en');
    await Promise.resolve();
    expect(row('route-ipv4')).toBe('None');
    expect(row('dns-servers')).toBe('None configured');
  });

  it('marks a local resolver stub and says that the servers behind it are not visible', async () => {
    await openWith(stubRouting);
    expect(row('dns-servers')).toBe('127.0.0.53 (lokal) (lokal heißt: ein Resolver auf dem Gerät, die Server dahinter sind hier nicht sichtbar)');
    cleanup();
    setLang('en');
    await openWith(stubRouting);
    expect(row('dns-servers')).toBe('127.0.0.53 (local) (local means a resolver on the device; the servers behind it are not visible here)');
  });

  it('never shows a hidden gateway, says why, and counts hidden servers; a tunnel has no gateway', async () => {
    await openWith(hiddenRouting);
    expect(row('route-ipv4')).toBe('direkt über wg0');
    expect(row('route-ipv6')).toBe('Router über wlan0 (Adresse verborgen, sie enthält eine Hardwareadresse)');
    expect(row('dns-servers')).toBe('2001:db8::53 (1 weitere verborgen, mit Hardwareadresse)');
    const allHidden = structuredClone(hiddenRouting);
    allHidden.dns = { status: 'found', servers: [], hidden: 2 };
    cleanup();
    await openWith(allHidden);
    expect(row('dns-servers')).toBe('Verborgen (2 weitere verborgen, mit Hardwareadresse)');
  });

  it('shows Nicht verfügbar for a part whose file cannot be read; the rest of the card stays', async () => {
    await openWith(unreadableRouting);
    for (const id of ['route-ipv4', 'route-ipv6', 'dns-servers']) expect(row(id)).toBe('Nicht verfügbar');
    expect(screen.getByTestId('network-wlan0')).toBeTruthy();
    // An unknown status is treated like an unreadable source, never as a value.
    const odd = structuredClone(piRouting);
    odd.ipv4.status = 'something-new';
    cleanup();
    await openWith(odd);
    expect(row('route-ipv4')).toBe('Nicht verfügbar');
  });

  it('shows no routing part for a service that sends none', async () => {
    await openWith(undefined);
    expect(screen.queryByTestId('routing')).toBeNull();
    expect(screen.getByTestId('network-wlan0')).toBeTruthy();
  });

  it('shows the routing part also when no interface is listed', async () => {
    await openDashboard(
      () => render(Dashboard),
      metricsWith((m) => {
        m.network = [];
      }),
    );
    expect(screen.getByTestId('no-network')).toBeTruthy();
    expect(row('route-ipv4')).toBe('192.0.2.1 über wlan0');
  });
});

describe('the routing part of a snapshot is checked', () => {
  it('accepts the fixtures and a snapshot without routing', () => {
    for (const r of [piRouting, noRouteRouting, stubRouting, hiddenRouting, unreadableRouting]) expect(isSnapshot({ ...piSnapshot, routing: r })).toBe(true);
    const { routing: _drop, ...old } = piSnapshot;
    expect(isSnapshot(old)).toBe(true);
  });

  it('rejects a routing part of the wrong shape', () => {
    const bad: unknown[] = [
      null,
      {},
      { ...piRouting, ipv4: { ...piRouting.ipv4, gateway: 5 } },
      { ...piRouting, ipv6: { ...piRouting.ipv6, gateway_hidden: 'no' } },
      { ...piRouting, dns: { ...piRouting.dns, servers: [{ address: 1, local: false }] } },
      { ...piRouting, dns: { ...piRouting.dns, servers: 'x' } },
      { ...piRouting, dns: { ...piRouting.dns, hidden: null } },
      { ...piRouting, dns: { status: 'found', servers: [{ address: '192.0.2.53' }], hidden: 0 } },
    ];
    for (const routing of bad) expect(isSnapshot({ ...piSnapshot, routing }), JSON.stringify(routing)).toBe(false);
  });
});

// ---- the development scenario mode (task N0, docs/DEVELOPMENT.md) ---------------------

describe('network scenario mode', () => {
  it('has the scenarios of docs/DEVELOPMENT.md, and refuses an unknown name', () => {
    expect(metricsScenarioNames()).toEqual([
      'network-route-dns',
      'network-no-route',
      'network-dns-stub',
      'network-hidden',
      'network-unreadable',
      'device-memory-unknown',
      'network-wifi-signal',
      'network-ethernet-only',
      'network-resolver-stub',
      'network-none',
      'storage-reference',
      'storage-removable',
      'storage-nearly-full',
    ]);
    expect(installMetricsMock('not-a-scenario')).toBe(false);
  });

  const expected: Record<string, string> = {
    'network-route-dns': '192.0.2.1 über wlan0',
    'network-no-route': 'Keine',
    'network-dns-stub': '192.0.2.1 über wlan0',
    'network-hidden': 'direkt über wg0',
    'network-unreadable': 'Nicht verfügbar',
    'device-memory-unknown': '192.0.2.1 über wlan0',
    'network-wifi-signal': '192.0.2.1 über wlan0',
    'network-ethernet-only': '192.0.2.1 über eth0',
    'network-resolver-stub': '192.0.2.1 über wlan0',
    'network-none': 'Keine',
    'storage-reference': '192.0.2.1 über wlan0',
    'storage-removable': '192.0.2.1 über wlan0',
    'storage-nearly-full': '192.0.2.1 über wlan0',
  };
  for (const name of metricsScenarioNames()) {
    it(`scenario ${name} renders the network card from the stream`, async () => {
      vi.stubGlobal('fetch', vi.fn(async () => new Response('{}', { status: 404 })));
      const realES = window.EventSource;
      try {
        expect(installMetricsMock(name)).toBe(true);
        render(Dashboard);
        await screen.findByTestId('routing', {}, { timeout: 2000 });
        expect(row('route-ipv4')).toBe(expected[name]);
        if (name === 'network-dns-stub') expect(row('dns-servers')).toContain('127.0.0.53 (lokal)');
        if (name === 'network-resolver-stub') expect(row('dns-servers')).toContain('genutzt über den Resolver auf dem Gerät');
      } finally {
        window.EventSource = realES;
      }
    });
  }

  it('scenario device-memory-unknown shows the Gerät page without an installed size', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('{}', { status: 404 })));
    expect(installMetricsMock('device-memory-unknown')).toBe(true);
    render(Device);
    await screen.findByText('Synthetic Board A1');
    expect(row('memory')).toBe('7970 MiB');
    cleanup();
    expect(installMetricsMock('network-route-dns')).toBe(true);
    render(Device);
    await screen.findByText('Raspberry Pi Zero 2 W Rev 1.0');
    expect(row('memory')).toBe('512 MiB (davon 406 MiB nutzbar)');
  });
});
