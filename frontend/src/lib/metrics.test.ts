import { describe, expect, it, vi } from 'vitest';
import { formatBytes, formatCelsius, formatClock, formatLoad, formatMHz, formatPercent, formatRate, formatSpeed, formatUptime } from './format';
import { complianceAlert, genericMetrics, piHistory, piMetrics, piSnapshot } from './metrics.fixtures';
import {
  computeRates,
  cpuTemperature,
  fetchMetrics,
  memoryUsedPercent,
  parseAlerts,
  parseHistory,
  parseSnapshot,
  pushPoint,
  type Snapshot,
} from './metrics';

const json = (v: unknown) => JSON.stringify(v);
const clone = <T>(v: T): T => structuredClone(v);

describe('parseSnapshot', () => {
  it('accepts a full snapshot and one with every optional value unknown', () => {
    expect(parseSnapshot(json(piSnapshot))).toEqual(piSnapshot);
    const empty: Snapshot = {
      ...piSnapshot,
      hostname: null,
      uptime_seconds: null,
      cpu: { usage_percent: null, cores: null, load: null, frequency: { current_mhz: null, max_mhz: null }, governor: null },
      memory: { total_bytes: null, available_bytes: null, used_bytes: null, swap_total_bytes: null, swap_used_bytes: null },
      temperatures: [],
      storage: [],
      network: [],
    };
    expect(parseSnapshot(json(empty))).toEqual(empty);
  });

  it.each([
    ['empty text', ''],
    ['not JSON', '{"a"'],
    ['null', 'null'],
    ['an array', '[]'],
    ['a number', '5'],
    ['another schema version', json({ ...piSnapshot, schema_version: 2 })],
    ['no timestamp', json({ ...piSnapshot, collected_at: undefined })],
    ['a timestamp that is no date', json({ ...piSnapshot, collected_at: 'yesterday' })],
    ['hostname of the wrong type', json({ ...piSnapshot, hostname: 5 })],
    ['cpu missing', json({ ...piSnapshot, cpu: undefined })],
    ['load of the wrong shape', json({ ...piSnapshot, cpu: { ...piSnapshot.cpu, load: { one: 'x' } } })],
    ['a memory value that is a string', json({ ...piSnapshot, memory: { ...piSnapshot.memory, total_bytes: '5' } })],
    ['temperatures that are no list', json({ ...piSnapshot, temperatures: null })],
    ['a temperature without id', json({ ...piSnapshot, temperatures: [{ name: 'x', source: 'hwmon', cpu: false, celsius: 1 }] })],
    ['storage with a missing number', json({ ...piSnapshot, storage: [{ mount_point: '/', fs_type: 'ext4' }] })],
    ['a network entry with addresses that are no list', json({ ...piSnapshot, network: [{ ...piSnapshot.network[0], ipv4: 'x' }] })],
    ['a counter that is not a number', json({ ...piSnapshot, network: [{ ...piSnapshot.network[0], rx_bytes: 'many' }] })],
  ])('rejects %s', (_name, text) => {
    expect(parseSnapshot(text)).toBeNull();
  });

  it('rejects numbers that are not finite', () => {
    // JSON has no NaN; an oversized exponent parses to Infinity.
    expect(parseSnapshot(json(piSnapshot).replace('"usage_percent":12.4', '"usage_percent":1e999'))).toBeNull();
  });
});

describe('parseAlerts and parseHistory', () => {
  it('reads an alerts event and drops single invalid alerts', () => {
    const bad = { id: 'x', level: 'urgent', cause: { key: 'a' }, impact: { key: 'b' }, next_step: { key: 'c' } };
    const noKey = { id: 'y', level: 'critical', cause: {}, impact: { key: 'b' }, next_step: { key: 'c' } };
    const badParam = { ...complianceAlert, id: 'z', cause: { key: 'k', params: { a: { nested: 1 } } } };
    expect(parseAlerts(json({ schema_version: 1, alerts: [complianceAlert, bad, noKey, badParam, 5, null] }))).toEqual([complianceAlert]);
    expect(parseAlerts(json({ schema_version: 1, alerts: [] }))).toEqual([]);
  });

  it.each(['', 'x', 'null', '[]', json({ alerts: [] }), json({ schema_version: 2, alerts: [] }), json({ schema_version: 1, alerts: 'x' })])(
    'rejects the malformed alerts event %j',
    (text) => expect(parseAlerts(text)).toBeNull(),
  );

  it('reads a history event', () => {
    expect(parseHistory(json(piHistory))).toEqual(piHistory);
    expect(parseHistory(json({ ...piHistory, cpu_percent: [1, 'x'] }))).toBeNull();
    expect(parseHistory(json({ ...piHistory, interval_seconds: 'x' }))).toBeNull();
    expect(parseHistory('nope')).toBeNull();
  });
});

describe('fetchMetrics', () => {
  const answer = (body: unknown, status = 200) => vi.fn().mockResolvedValue(new Response(json(body), { status }));

  it('returns the snapshot with device summary, alerts and history', async () => {
    const fetchFn = answer(piMetrics);
    const r = await fetchMetrics(fetchFn);
    expect(r).toEqual({ ok: true, metrics: piMetrics });
    expect(fetchFn).toHaveBeenCalledWith('/api/v1/metrics', expect.objectContaining({ cache: 'no-store' }));
  });

  it('keeps only the valid alerts', async () => {
    const r = await fetchMetrics(answer({ ...piMetrics, alerts: [complianceAlert, { id: 'broken' }] }));
    expect(r.ok && r.metrics.alerts).toEqual([complianceAlert]);
  });

  it('fails without throwing on an error status, a wrong shape or a network error', async () => {
    expect(await fetchMetrics(answer(piMetrics, 503))).toEqual({ ok: false });
    expect(await fetchMetrics(answer({ schema_version: 1 }))).toEqual({ ok: false });
    expect(await fetchMetrics(answer({ ...piMetrics, device: null }))).toEqual({ ok: false });
    expect(await fetchMetrics(answer({ ...piMetrics, history: {} }))).toEqual({ ok: false });
    expect(await fetchMetrics(vi.fn().mockRejectedValue(new TypeError('offline')))).toEqual({ ok: false });
    expect(await fetchMetrics(vi.fn().mockResolvedValue(new Response('<html>', { status: 200 })))).toEqual({ ok: false });
  });
});

describe('computeRates', () => {
  const at = (iso: string, rx: number | null, tx: number | null, name = 'wlan0'): Snapshot => {
    const s = clone(piSnapshot);
    s.collected_at = iso;
    s.network = [{ ...s.network[0]!, name, rx_bytes: rx, tx_bytes: tx }];
    return s;
  };

  it('divides the counter difference by the time between the samples', () => {
    const r = computeRates(at('2026-10-27T12:00:00Z', 1000, 5000), at('2026-10-27T12:00:02Z', 5096, 5000));
    expect(r.wlan0).toEqual({ rx: 2048, tx: 0 });
  });

  it('has no rate without a previous sample, for a new interface, or for an unknown counter', () => {
    const cur = at('2026-10-27T12:00:02Z', 5096, 100);
    expect(computeRates(null, cur).wlan0).toEqual({ rx: null, tx: null });
    expect(computeRates(at('2026-10-27T12:00:00Z', 1000, 1000, 'eth0'), cur).wlan0).toEqual({ rx: null, tx: null });
    expect(computeRates(at('2026-10-27T12:00:00Z', null, null), cur).wlan0).toEqual({ rx: null, tx: null });
    expect(computeRates(at('2026-10-27T12:00:00Z', 1, 1), at('2026-10-27T12:00:02Z', null, null)).wlan0).toEqual({ rx: null, tx: null });
  });

  it('has no rate when a counter went backwards (reboot, wrap) or the samples are too close or too far apart', () => {
    expect(computeRates(at('2026-10-27T12:00:00Z', 9000, 9000), at('2026-10-27T12:00:02Z', 100, 9100)).wlan0).toEqual({ rx: null, tx: 50 });
    expect(computeRates(at('2026-10-27T12:00:00.000Z', 1, 1), at('2026-10-27T12:00:00.100Z', 100, 100)).wlan0).toEqual({ rx: null, tx: null });
    expect(computeRates(at('2026-10-27T12:00:00Z', 1, 1), at('2026-10-27T12:05:00Z', 100, 100)).wlan0).toEqual({ rx: null, tx: null });
    expect(computeRates(at('2026-10-27T12:00:05Z', 1, 1), at('2026-10-27T12:00:00Z', 100, 100)).wlan0).toEqual({ rx: null, tx: null });
  });
});

describe('derived values', () => {
  it('finds the hottest processor sensor and the memory share in use', () => {
    const s = clone(piSnapshot);
    s.temperatures = [
      { id: 'a', name: 'nvme', source: 'hwmon', cpu: false, celsius: 90 },
      { id: 'b', name: 'cpu', source: 'hwmon', cpu: true, celsius: 51 },
      { id: 'c', name: 'soc', source: 'thermal_zone', cpu: true, celsius: 60.5 },
      { id: 'd', name: 'x', source: 'hwmon', cpu: true, celsius: null },
    ];
    expect(cpuTemperature(s)).toBe(60.5);
    s.temperatures = [];
    expect(cpuTemperature(s)).toBeNull();
    expect(memoryUsedPercent(piSnapshot)).toBe(36.9);
    expect(memoryUsedPercent({ ...piSnapshot, memory: { ...piSnapshot.memory, total_bytes: null } })).toBeNull();
    expect(memoryUsedPercent(genericMetrics)).toBe(26.5);
  });

  it('keeps at most 60 points', () => {
    let s: (number | null)[] = [];
    for (let i = 0; i < 100; i++) s = pushPoint(s, i);
    expect(s).toHaveLength(60);
    expect(s[0]).toBe(40);
    expect(s.at(-1)).toBe(99);
    expect(pushPoint([1], null)).toEqual([1, null]);
  });
});

describe('formatting', () => {
  it('formats values for the German interface', () => {
    expect(formatBytes(null)).toBeNull();
    expect(formatBytes(0)).toBe('0 B');
    expect(formatBytes(1023)).toBe('1.023 B');
    expect(formatBytes(1536)).toBe('1,5 KiB');
    expect(formatBytes(425373696)).toBe('405,7 MiB');
    expect(formatBytes(3 * 1024 ** 3)).toBe('3,0 GiB');
    expect(formatBytes(5 * 1024 ** 5)).toBe('5.120,0 TiB');
    expect(formatRate(2048)).toBe('2,0 KiB/s');
    expect(formatRate(null)).toBeNull();
    expect(formatPercent(12.4)).toBe('12 %');
    expect(formatPercent(null)).toBeNull();
    expect(formatCelsius(43.34)).toBe('43,3 °C');
    expect(formatMHz(600)).toBe('600 MHz');
    expect(formatSpeed(1000)).toBe('1.000 Mbit/s');
    expect(formatSpeed(null)).toBeNull();
    expect(formatLoad(0.18)).toBe('0,18');
    expect(formatClock('not a date')).toBeNull();
    expect(formatClock('2026-10-27T12:00:00Z')).toMatch(/^\d\d:\d\d:\d\d$/);
  });

  it('formats the uptime in the two largest units', () => {
    expect(formatUptime(null)).toBeNull();
    expect(formatUptime(5)).toBe('0 Min.');
    expect(formatUptime(125)).toBe('2 Min.');
    expect(formatUptime(3 * 3600 + 7 * 60)).toBe('3 Std. 7 Min.');
    expect(formatUptime(90061)).toBe('1 T 1 Std.');
    expect(formatUptime(400 * 86400)).toBe('400 T 0 Std.');
  });
});
