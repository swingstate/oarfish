// The page Speicher (task ST1, ORB-STO-001 to -003, the low-capacity part of
// -004; docs/UI.md, "Storage page"): file systems with device, type, size and
// use, the boot medium, the marks, the other mounts by kind, the warning,
// both languages, the empty and failed states. Read-only.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen, within } from '@testing-library/svelte';
import Storage from './Storage.svelte';
import { FakeEventSource } from '../lib/eventsource.fake';
import { installFakes, metricsWith, snapshotAt, textOf } from '../lib/harness.fake';
import { de, en, t } from '../lib/i18n';
import { setLang } from '../lib/lang.svelte';
import { piZero2W } from '../lib/device.fixtures';
import { nearlyFullStorage, otherMounts, piMetrics, piSnapshot, usbStick } from '../lib/metrics.fixtures';
import { capacityLevel, storageCriticalPercent, storageRelevantPercent } from '../lib/storage';
import type { Device } from '../lib/device';
import type { MetricsResponse } from '../lib/metrics';

beforeEach(installFakes);
afterEach(() => {
  cleanup();
  setLang('de');
  vi.unstubAllGlobals();
});

function serve(body: MetricsResponse | null, device: Device | null = piZero2W): void {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      if (url === '/api/v1/device') return device ? new Response(JSON.stringify(device)) : new Response('{}', { status: 503 });
      if (body === null) throw new TypeError('offline');
      return new Response(JSON.stringify(body));
    }),
  );
}

async function open(body: MetricsResponse = piMetrics, device: Device | null = piZero2W): Promise<FakeEventSource> {
  serve(body, device);
  render(Storage);
  await screen.findByTestId('filesystems');
  FakeEventSource.last.open();
  return FakeEventSource.last;
}

describe('storage page', () => {
  it('the reference layout: root and boot on the card with device, type, size and use; the boot medium; /tmp as memory', async () => {
    await open();
    expect(screen.getByRole('heading', { level: 1, name: de.storagePageTitle })).toBeTruthy();
    await vi.waitFor(() => expect(textOf('boot-kind')).toBe(`${de['bootMedium.mmc']}`));
    expect(textOf('root-device')).toBe(`mmcblk0p2`);
    const root = screen.getByTestId('fs-/');
    expect(within(root).getByRole('heading', { level: 3 }).textContent).toBe('/');
    expect(textOf('device-/')).toBe(`mmcblk0p2`);
    expect(within(root).getByText('ext4')).toBeTruthy();
    expect(textOf('mark-/')).toBe(t.stoMarkBoot);
    expect(textOf('mark-/boot/firmware')).toBe(t.stoMarkBoot);
    expect(within(root).getByRole('progressbar')).toBeTruthy();
    expect(screen.queryByTestId('warning-/')).toBeNull();
    expect(textOf('kind-/tmp')).toBe(`${de['stoKind.memory']}`);
    expect(textOf('kind-note-/tmp')).toBe(de['stoKindNote.memory']);
  });

  it('the same in English', async () => {
    setLang('en');
    await open();
    expect(screen.getByRole('heading', { level: 1, name: en.storagePageTitle })).toBeTruthy();
    expect(textOf('mark-/')).toBe(en.stoMarkBoot);
    expect(screen.getByRole('heading', { level: 2, name: en.stoFsHeading })).toBeTruthy();
  });

  it('a USB stick is marked removable; the boot medium never is', async () => {
    await open(metricsWith((m) => (m.storage = [...structuredClone(piSnapshot.storage), structuredClone(usbStick)])));
    expect(textOf('mark-/media/DEMO-STICK')).toBe(t.stoMarkRemovable);
    expect(textOf('mark-/')).toBe(t.stoMarkBoot);
    // Even if the kernel flags the card as removable, it is the boot medium.
    cleanup();
    await open(metricsWith((m) => m.storage.forEach((d) => (d.removable = true))));
    expect(screen.getAllByTestId('mark-/').map((e) => e.textContent)).toEqual([t.stoMarkBoot]);
    expect(screen.getByTestId('storage-page').textContent).not.toContain(t.stoMarkRemovable);
  });

  it('a file system with no known disk has no mark and an unknown device', async () => {
    await open(metricsWith((m) => m.storage.forEach((d) => ((d.device = ''), (d.disk = ''), (d.removable = null)))));
    expect(screen.queryByTestId('mark-/')).toBeNull();
    expect(textOf('device-/')).toBe(`${t.unknownValue}`);
    expect(textOf('root-device')).toBe(`${t.unavailableValue}`);
  });

  it('other mounts are shown by kind; a network share has no size', async () => {
    await open(metricsWith((m) => (m.mounts = structuredClone(otherMounts))));
    expect(textOf('kind-/mnt/share')).toBe(`${de['stoKind.network']}`);
    expect(textOf('kind-note-/mnt/share')).toBe(de['stoKindNote.network']);
    expect(within(screen.getByTestId('mount-/mnt/share')).getAllByText(t.unavailableValue).length).toBeGreaterThan(0);
    expect(textOf('kind-note-/srv/overlay')).toBe(de['stoKindNote.overlay']);
    // A kind this version does not know is said neutrally.
    cleanup();
    await open(metricsWith((m) => (m.mounts = [{ ...otherMounts[0]!, kind: 'later-kind' }])));
    expect(textOf('kind-/mnt/share')).toBe(`${de['stoKind.other']}`);
  });

  it('a nearly full file system shows the low-capacity warning with the thresholds of the alert', async () => {
    await open(metricsWith((m) => (m.storage = nearlyFullStorage())));
    expect(textOf('warning-/')).toContain('zu 96 % belegt. Über 95 % ist es kritisch.');
    expect(textOf('warning-/boot/firmware')).toContain('zu 88 % belegt. Über 85 % wird es gemeldet.');
    expect([85, 85.1, 95, 95.1].map(capacityLevel)).toEqual(['ok', 'relevant', 'relevant', 'critical']);
    expect([storageRelevantPercent, storageCriticalPercent]).toEqual([85, 95]);
  });

  it('no file system: the empty state; no other mount: no card', async () => {
    await open(metricsWith((m) => ((m.storage = []), (m.mounts = []))));
    expect(textOf('no-filesystems')).toBe(t.stoNone);
    expect(screen.queryByTestId('mounts')).toBeNull();
  });

  it('an answer before ST1 (no device fields, no mounts) still shows the file systems', async () => {
    await open(
      metricsWith((m) => {
        delete m.mounts;
        for (const d of m.storage) {
          delete d.device;
          delete d.disk;
          delete d.removable;
          delete d.usb;
        }
      }),
    );
    expect(textOf('device-/')).toBe(`${t.unknownValue}`);
    expect(screen.queryByTestId('mounts')).toBeNull();
  });

  it('the device summary is unreadable: the boot medium says Nicht verfügbar', async () => {
    await open(piMetrics, null);
    expect(textOf('boot-kind')).toBe(`${t.unavailableValue}`);
  });

  it('loading, and a failed first read with the connection down', async () => {
    serve(null);
    render(Storage);
    expect(screen.getByRole('status').textContent).toContain(t.stoPageLoading);
    await vi.waitFor(() => expect(FakeEventSource.instances.length).toBeGreaterThan(0));
    FakeEventSource.last.fail();
    expect((await screen.findByRole('alert')).textContent).toContain(t.stoPageLoadError);
  });

  it('follows the stream', async () => {
    const es = await open();
    es.send('metrics', snapshotAt(2, (s) => (s.storage = nearlyFullStorage())));
    await vi.waitFor(() => expect(screen.getByTestId('warning-/')).toBeTruthy());
  });

  it('offers no action: no button besides none at all', async () => {
    await open(metricsWith((m) => (m.mounts = structuredClone(otherMounts))));
    expect(within(screen.getByTestId('storage-page')).queryAllByRole('button')).toHaveLength(0);
  });
});
