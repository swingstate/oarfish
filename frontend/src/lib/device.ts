export interface DeviceCapability {
  id: string;
  available: boolean;
  source: string;
  reason?: string;
}

/** Response of GET /api/v1/device (schema_version 1). Unknown values are null. */
export interface Device {
  schema_version: number;
  hardware: {
    vendor: string | null;
    model: string | null;
    soc_family: string | null;
    arch: string;
    revision_code: string | null;
    memory_total_bytes: number | null;
  };
  os: {
    id: string | null;
    id_like: string[];
    version_id: string | null;
    pretty_name: string | null;
    adapter: string;
    adapter_status: string;
  };
  kernel: { release: string | null };
  profile: { id: string; version: string; matched_by: string };
  support: {
    tier: string;
    reason: string;
    explanation_key: string;
    limitations: string[];
    board_actions_allowed: boolean;
  };
  capabilities: DeviceCapability[];
  sensors: { cpu_temperature_celsius: number | null };
  boot_medium: { type: string };
}

export type DeviceResult = { ok: true; device: Device } | { ok: false };

/** Reads the device information from the Oarfish core. Never throws. */
export async function fetchDevice(
  fetchFn: typeof fetch = fetch,
  timeoutMs = 5000,
): Promise<DeviceResult> {
  try {
    const res = await fetchFn('/api/v1/device', {
      cache: 'no-store',
      signal: AbortSignal.timeout(timeoutMs),
    });
    if (!res.ok) return { ok: false };
    const body: unknown = await res.json();
    if (!isDevice(body)) return { ok: false };
    return { ok: true, device: body };
  } catch {
    return { ok: false };
  }
}

// Checks the parts the page relies on; the server owns the full schema.
function isDevice(v: unknown): v is Device {
  const d = v as Partial<Device> | null;
  return (
    typeof d === 'object' &&
    d !== null &&
    d.schema_version === 1 &&
    typeof d.hardware === 'object' &&
    d.hardware !== null &&
    typeof d.os === 'object' &&
    d.os !== null &&
    typeof d.support === 'object' &&
    d.support !== null &&
    Array.isArray(d.support.limitations) &&
    Array.isArray(d.capabilities) &&
    typeof d.sensors === 'object' &&
    typeof d.boot_medium === 'object' &&
    typeof d.profile === 'object'
  );
}

/** Memory in MiB, rounded; null stays null. */
export function formatMemory(bytes: number | null): string | null {
  return bytes === null ? null : `${Math.round(bytes / 1048576)} MiB`;
}

export function formatCelsius(value: number | null): string | null {
  return value === null ? null : `${new Intl.NumberFormat('de-DE', { minimumFractionDigits: 1, maximumFractionDigits: 1 }).format(value)} °C`;
}
