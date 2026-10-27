// Client for GET /api/v1/metrics and the shapes of the metrics event stream
// (docs/DASHBOARD.md). Everything that arrives is checked before it is used:
// a malformed event must never crash the page.

export interface Load {
  one: number;
  five: number;
  fifteen: number;
}

export interface Temperature {
  id: string;
  name: string;
  source: string;
  cpu: boolean;
  celsius: number | null;
}

export interface Storage {
  mount_point: string;
  fs_type: string;
  total_bytes: number;
  used_bytes: number;
  available_bytes: number;
  used_percent: number;
}

export interface NetworkInterface {
  name: string;
  kind: string;
  state: string;
  link_up: boolean;
  speed_mbps: number | null;
  rx_bytes: number | null;
  tx_bytes: number | null;
  ipv4: string[];
  ipv6: string[];
}

/** One reading of the device (schema_version 1). Unknown values are null. */
export interface Snapshot {
  schema_version: number;
  collected_at: string;
  hostname: string | null;
  uptime_seconds: number | null;
  cpu: {
    usage_percent: number | null;
    cores: number | null;
    load: Load | null;
    frequency: { current_mhz: number | null; max_mhz: number | null };
    governor: string | null;
  };
  memory: {
    total_bytes: number | null;
    available_bytes: number | null;
    used_bytes: number | null;
    swap_total_bytes: number | null;
    swap_used_bytes: number | null;
  };
  temperatures: Temperature[];
  storage: Storage[];
  network: NetworkInterface[];
}

export type AlertLevel = 'critical' | 'relevant' | 'informational';
export const alertLevels: readonly AlertLevel[] = ['critical', 'relevant', 'informational'];

export interface AlertMessage {
  key: string;
  params?: Record<string, string | number>;
}

export interface Alert {
  id: string;
  level: AlertLevel;
  cause: AlertMessage;
  impact: AlertMessage;
  next_step: AlertMessage;
}

export interface History {
  schema_version: number;
  interval_seconds: number;
  cpu_percent: (number | null)[];
  memory_used_percent: (number | null)[];
  temperature_celsius: (number | null)[];
}

export interface DeviceSummary {
  model: string | null;
  soc_family: string | null;
  arch: string | null;
  os_pretty_name: string | null;
  kernel_release: string | null;
  support_tier: string | null;
  support_reason: string | null;
  support_explanation_key: string | null;
  generic_mode: boolean;
}

/** Response of GET /api/v1/metrics: a snapshot plus device summary, alerts and history. */
export interface MetricsResponse extends Snapshot {
  device: DeviceSummary;
  alerts: Alert[];
  history: History;
}

// ---- validation ---------------------------------------------------------

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v);
const isNum = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v);
const isNumOrNull = (v: unknown): v is number | null => v === null || isNum(v);
const isStrOrNull = (v: unknown): v is string | null => v === null || typeof v === 'string';
const isStr = (v: unknown): v is string => typeof v === 'string';
const isStrList = (v: unknown): v is string[] => Array.isArray(v) && v.every(isStr);

function isLoad(v: unknown): v is Load {
  return isObject(v) && isNum(v.one) && isNum(v.five) && isNum(v.fifteen);
}

function isTemperature(v: unknown): v is Temperature {
  return isObject(v) && isStr(v.id) && isStr(v.name) && isStr(v.source) && typeof v.cpu === 'boolean' && isNumOrNull(v.celsius);
}

function isStorage(v: unknown): v is Storage {
  return (
    isObject(v) &&
    isStr(v.mount_point) &&
    isStr(v.fs_type) &&
    isNum(v.total_bytes) &&
    isNum(v.used_bytes) &&
    isNum(v.available_bytes) &&
    isNum(v.used_percent)
  );
}

function isInterface(v: unknown): v is NetworkInterface {
  return (
    isObject(v) &&
    isStr(v.name) &&
    isStr(v.kind) &&
    isStr(v.state) &&
    typeof v.link_up === 'boolean' &&
    isNumOrNull(v.speed_mbps) &&
    isNumOrNull(v.rx_bytes) &&
    isNumOrNull(v.tx_bytes) &&
    isStrList(v.ipv4) &&
    isStrList(v.ipv6)
  );
}

export function isSnapshot(v: unknown): v is Snapshot {
  if (!isObject(v) || v.schema_version !== 1 || !isStr(v.collected_at) || Number.isNaN(Date.parse(v.collected_at))) return false;
  if (!isStrOrNull(v.hostname) || !isNumOrNull(v.uptime_seconds)) return false;
  const cpu = v.cpu;
  if (
    !isObject(cpu) ||
    !isNumOrNull(cpu.usage_percent) ||
    !isNumOrNull(cpu.cores) ||
    !(cpu.load === null || isLoad(cpu.load)) ||
    !isStrOrNull(cpu.governor) ||
    !isObject(cpu.frequency) ||
    !isNumOrNull(cpu.frequency.current_mhz) ||
    !isNumOrNull(cpu.frequency.max_mhz)
  ) {
    return false;
  }
  const mem = v.memory;
  if (
    !isObject(mem) ||
    !isNumOrNull(mem.total_bytes) ||
    !isNumOrNull(mem.available_bytes) ||
    !isNumOrNull(mem.used_bytes) ||
    !isNumOrNull(mem.swap_total_bytes) ||
    !isNumOrNull(mem.swap_used_bytes)
  ) {
    return false;
  }
  return (
    Array.isArray(v.temperatures) &&
    v.temperatures.every(isTemperature) &&
    Array.isArray(v.storage) &&
    v.storage.every(isStorage) &&
    Array.isArray(v.network) &&
    v.network.every(isInterface)
  );
}

function isMessage(v: unknown): v is AlertMessage {
  if (!isObject(v) || !isStr(v.key)) return false;
  if (v.params === undefined) return true;
  return isObject(v.params) && Object.values(v.params).every((p) => isStr(p) || isNum(p));
}

function isAlert(v: unknown): v is Alert {
  return (
    isObject(v) &&
    isStr(v.id) &&
    isStr(v.level) &&
    (alertLevels as readonly string[]).includes(v.level) &&
    isMessage(v.cause) &&
    isMessage(v.impact) &&
    isMessage(v.next_step)
  );
}

export function isHistory(v: unknown): v is History {
  const series = (x: unknown) => Array.isArray(x) && x.every(isNumOrNull);
  return (
    isObject(v) &&
    v.schema_version === 1 &&
    isNum(v.interval_seconds) &&
    series(v.cpu_percent) &&
    series(v.memory_used_percent) &&
    series(v.temperature_celsius)
  );
}

function isDeviceSummary(v: unknown): v is DeviceSummary {
  return (
    isObject(v) &&
    isStrOrNull(v.model) &&
    isStrOrNull(v.soc_family) &&
    isStrOrNull(v.arch) &&
    isStrOrNull(v.os_pretty_name) &&
    isStrOrNull(v.kernel_release) &&
    isStrOrNull(v.support_tier) &&
    isStrOrNull(v.support_reason) &&
    isStrOrNull(v.support_explanation_key) &&
    typeof v.generic_mode === 'boolean'
  );
}

function parseJSON(text: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    return undefined;
  }
}

/** Parses the data of a `metrics` event. Returns null for anything malformed. */
export function parseSnapshot(text: string): Snapshot | null {
  const v = parseJSON(text);
  return isSnapshot(v) ? v : null;
}

/** Parses the data of an `alerts` event. Returns null for anything malformed; a single invalid alert is dropped. */
export function parseAlerts(text: string): Alert[] | null {
  const v = parseJSON(text);
  if (!isObject(v) || v.schema_version !== 1 || !Array.isArray(v.alerts)) return null;
  return v.alerts.filter(isAlert);
}

/** Parses the data of a `history` event. Returns null for anything malformed. */
export function parseHistory(text: string): History | null {
  const v = parseJSON(text);
  return isHistory(v) ? v : null;
}

export type MetricsResult = { ok: true; metrics: MetricsResponse } | { ok: false };

/** Reads one snapshot with device summary, alerts and history. Never throws. */
export async function fetchMetrics(fetchFn: typeof fetch = fetch, timeoutMs = 8000): Promise<MetricsResult> {
  try {
    const res = await fetchFn('/api/v1/metrics', { cache: 'no-store', signal: AbortSignal.timeout(timeoutMs) });
    if (!res.ok) return { ok: false };
    const body: unknown = await res.json();
    if (!isSnapshot(body)) return { ok: false };
    const extra = body as unknown as Record<string, unknown>;
    if (!isDeviceSummary(extra.device) || !Array.isArray(extra.alerts) || !isHistory(extra.history)) return { ok: false };
    return {
      ok: true,
      metrics: { ...body, device: extra.device, alerts: (extra.alerts as unknown[]).filter(isAlert), history: extra.history },
    };
  } catch {
    return { ok: false };
  }
}

// ---- derived values -----------------------------------------------------

export interface Rate {
  rx: number | null; // bytes per second
  tx: number | null;
}

/**
 * Transfer rates per interface, computed in the browser from two snapshots
 * (the server sends counters only). A value is null when there is no previous
 * snapshot, the counter went backwards (reset), or the samples are too far
 * apart or too close together to mean anything.
 */
export function computeRates(prev: Snapshot | null, cur: Snapshot): Record<string, Rate> {
  const out: Record<string, Rate> = {};
  const seconds = prev ? (Date.parse(cur.collected_at) - Date.parse(prev.collected_at)) / 1000 : 0;
  const usable = seconds >= 0.2 && seconds <= 60;
  for (const n of cur.network) {
    const before = prev?.network.find((p) => p.name === n.name);
    const rate = (a: number | null, b: number | null | undefined) =>
      usable && a !== null && b !== null && b !== undefined && a >= b ? (a - b) / seconds : null;
    out[n.name] = { rx: rate(n.rx_bytes, before?.rx_bytes), tx: rate(n.tx_bytes, before?.tx_bytes) };
  }
  return out;
}

/** The hottest temperature of the sensors that stand for the processor. */
export function cpuTemperature(s: Snapshot): number | null {
  let best: number | null = null;
  for (const t of s.temperatures) {
    if (t.cpu && t.celsius !== null && (best === null || t.celsius > best)) best = t.celsius;
  }
  return best;
}

/** Memory in use, as a share of the total. */
export function memoryUsedPercent(s: Snapshot): number | null {
  const { total_bytes, used_bytes } = s.memory;
  return total_bytes && used_bytes !== null ? Math.round((used_bytes / total_bytes) * 1000) / 10 : null;
}

/** Appends one point to a series, keeping at most `max` points. */
export function pushPoint(series: (number | null)[], value: number | null, max = 60): (number | null)[] {
  const next = [...series, value];
  return next.length > max ? next.slice(next.length - max) : next;
}
