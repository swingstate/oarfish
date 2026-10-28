// D-C43: the UI is data-driven. Names, kinds and lists come from the API, never from the code.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/svelte';
import Home from './Home.svelte';
import { installFakes, metricsWith, openHome, snapshotAt, textOf } from '../lib/harness.fake';
import { iconPaths, kindIcon } from '../lib/icons';
import { t } from '../lib/i18n';
import { piMetrics } from '../lib/metrics.fixtures';
import type { NetworkInterface } from '../lib/metrics';

beforeEach(installFakes);
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const open = (body = piMetrics) => openHome(() => render(Home), body);

// Names that exist nowhere in the code base.
const kinds = ['wifi', 'ethernet', 'other', 'quantum-link'];
const nic = (i: number): NetworkInterface => ({
  name: i === 0 ? 'zz-synthetic-interface-with-a-name-that-is-much-too-long-for-one-line' : `qq${i}-synth`,
  kind: kinds[i % kinds.length]!,
  state: i % 2 === 0 ? 'up' : 'down',
  link_up: i % 2 === 0,
  speed_mbps: null,
  rx_bytes: 1000 * i,
  tx_bytes: 500 * i,
  ipv4: [`192.0.2.${10 + i}`],
  ipv6: [`2001:db8::${i + 1}`],
});

describe('Home is data-driven (D-C43)', () => {
  it('renders the largest allowed lists cleanly: 8 interfaces, 8 mounts, 16 sensors', async () => {
    const m = metricsWith((x) => {
      x.network = Array.from({ length: 8 }, (_, i) => nic(i));
      x.storage = Array.from({ length: 8 }, (_, i) => ({
        mount_point: `/mnt/synthetic-volume-${i}`,
        fs_type: 'ext4',
        total_bytes: 1e9,
        used_bytes: 1e8 * (i + 1),
        available_bytes: 1e9 - 1e8 * (i + 1),
        used_percent: 10 * (i + 1),
      }));
      x.temperatures = Array.from({ length: 16 }, (_, i) => ({ id: `probe${i}`, name: `Synthetic probe ${i}`, source: 'hwmon', cpu: i === 0, celsius: 30 + i }));
    });
    await open(m);
    expect(document.querySelectorAll('[data-testid^="network-"]')).toHaveLength(8);
    expect(document.querySelectorAll('[data-testid^="storage-"]')).toHaveLength(8);
    expect(document.querySelectorAll('[data-testid^="temperature-"]')).toHaveLength(16);
    expect(document.querySelectorAll('section.card progress')).toHaveLength(10); // cpu, memory and one per mount
    for (const p of document.querySelectorAll('progress')) expect(p.getAttribute('aria-label')).toBeTruthy();
  });

  it('shows a long name cut to one line with the full text as title, for interface, mount and sensor', async () => {
    const longMount = '/mnt/' + 'v'.repeat(80);
    const longSensor = 'Synthetic sensor ' + 's'.repeat(80);
    const m = metricsWith((x) => {
      x.network = [nic(0)];
      x.storage = [{ mount_point: longMount, fs_type: 'ext4', total_bytes: 10, used_bytes: 5, available_bytes: 5, used_percent: 50 }];
      x.temperatures = [{ id: 'long1', name: longSensor, source: 'hwmon', cpu: false, celsius: 41 }];
    });
    await open(m);
    for (const full of [nic(0).name, longMount, longSensor]) {
      const el = screen.getAllByTitle(full)[0]!;
      expect(el.textContent).toBe(full);
      expect(el.classList.contains('truncate')).toBe(true);
    }
  });

  it('chooses the icon and the label by the API field kind; an unknown kind gets the neutral generic icon', async () => {
    const m = metricsWith((x) => (x.network = Array.from({ length: 4 }, (_, i) => nic(i))));
    await open(m);
    const firstPath = (testId: string) => screen.getByTestId(testId).querySelector('svg path')!.getAttribute('d');
    expect(firstPath(`network-${nic(0).name}`)).toBe(iconPaths.wifi[0]);
    expect(firstPath('network-qq1-synth')).toBe(iconPaths.ethernet[0]);
    expect(firstPath('network-qq2-synth')).toBe(iconPaths.network[0]);
    expect(firstPath('network-qq3-synth')).toBe(iconPaths.network[0]); // "quantum-link" is not known
    expect(kindIcon('anything else')).toBe('network');
    // The kind label is in the technical details; an unknown kind reads "Sonstige", never a raw key.
    const details = screen.getByTestId('network-qq3-synth').querySelector('details')!;
    expect(details.textContent).toContain(t['netKind.other']);
    expect(details.textContent).not.toContain('netKind.');
  });

  it('shows a missing value as a clear neutral placeholder', async () => {
    const m = metricsWith((x) => {
      x.network = [{ ...nic(1), rx_bytes: null, tx_bytes: null, speed_mbps: null, ipv4: [], ipv6: [] }];
    });
    await open(m);
    const li = screen.getByTestId('network-qq1-synth');
    expect(li.textContent).toContain(t.netNoAddress);
    expect(li.querySelector('details')!.textContent).toContain(t.unavailableValue);
  });

  it('follows a changing list without keeping a vanished entry', async () => {
    const es = await open();
    es.send('metrics', snapshotAt(2, (s) => (s.network = [nic(1), nic(2)])));
    await waitFor(() => expect(screen.getByTestId('network-qq2-synth')).toBeTruthy());
    expect(screen.queryByTestId('network-wlan0')).toBeNull();
    es.send('metrics', snapshotAt(4, (s) => (s.network = [nic(2)])));
    await waitFor(() => expect(screen.queryByTestId('network-qq1-synth')).toBeNull());
    expect(textOf('network-qq2-synth')).toContain('192.0.2.12');
  });
});

// The production code names no interface, port, hardware address or board (D-C43).
function productionFiles(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) {
      if (name !== 'locales') productionFiles(path, out); // the locale files hold the API's own vocabulary
    } else if (/\.(svelte|ts|css)$/.test(name) && !/\.(test|fixtures|fake)\.ts$/.test(name)) out.push(path);
  }
  return out;
}

describe('production code is free of device names (D-C43)', () => {
  const forbidden: [string, RegExp][] = [
    ['interface name', /\b(wlan|eth|enp|eno|wlp|usb|veth|docker|tailscale|br-)\w*\d/i],
    ['port name', /\b(gpio|uart|i2c|spi)\d*\b/i],
    ['hardware address', /\b[0-9a-f]{2}(:[0-9a-f]{2}){5}\b/i],
    ['board name', /raspberry|\bbcm\d{3,}|orange ?pi|banana ?pi|rock ?pi|jetson|odroid|\bpi zero/i],
    ['user or path', /\/home\/pi|\bpi@|raspi-config|vcgencmd|\/boot\/config/i],
  ];
  const srcDir = join(import.meta.dirname, '..');

  it('has no interface, port, hardware address or board name', () => {
    const hits: string[] = [];
    for (const file of productionFiles(srcDir)) {
      const text = readFileSync(file, 'utf8');
      for (const [what, re] of forbidden) {
        const m = re.exec(text);
        if (m) hits.push(`${file.replace(srcDir, 'src')}: ${what} "${m[0]}"`);
      }
    }
    expect(hits).toEqual([]);
  });
});
