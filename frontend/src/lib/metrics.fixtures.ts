// Synthetic metrics for tests, shaped like GET /api/v1/metrics and the
// event stream. Not captures of a device. Host name and addresses come from
// the synthetic set: "oarfish-test" and the documentation ranges.
import type { Alert, History, MetricsResponse, Snapshot } from './metrics';

export const piSnapshot: Snapshot = {
  schema_version: 1,
  collected_at: '2026-10-27T12:00:00.000Z',
  hostname: 'oarfish-test',
  uptime_seconds: 90061, // 1 day, 1 hour, 1 minute
  cpu: {
    usage_percent: 12.4,
    cores: 4,
    load: { one: 0.18, five: 0.12, fifteen: 0.09 },
    frequency: { current_mhz: 600, max_mhz: 1000 },
    governor: 'ondemand',
  },
  memory: {
    total_bytes: 415408 * 1024,
    available_bytes: 262144 * 1024,
    used_bytes: (415408 - 262144) * 1024,
    swap_total_bytes: 524272 * 1024,
    swap_used_bytes: 0,
  },
  temperatures: [{ id: 'thermal_zone0', name: 'cpu-thermal', source: 'thermal_zone', cpu: true, celsius: 43.3 }],
  storage: [
    { mount_point: '/', fs_type: 'ext4', total_bytes: 4096000000, used_bytes: 1228800000, available_bytes: 2662400000, used_percent: 31.6 },
    { mount_point: '/boot/firmware', fs_type: 'vfat', total_bytes: 536870912, used_bytes: 53687091, available_bytes: 483183821, used_percent: 10 },
  ],
  network: [
    {
      name: 'wlan0',
      kind: 'wifi',
      state: 'up',
      link_up: true,
      speed_mbps: null,
      rx_bytes: 123456789,
      tx_bytes: 23456789,
      ipv4: ['192.0.2.15'],
      ipv6: ['2001:db8::15'],
    },
  ],
};

export const genericModeAlert: Alert = {
  id: 'support_generic',
  level: 'informational',
  cause: { key: 'alert.support_generic.cause' },
  impact: { key: 'alert.support_generic.impact' },
  next_step: { key: 'alert.support_generic.next' },
};

export const relevantSwapAlert: Alert = {
  id: 'swap_in_use',
  level: 'relevant',
  cause: { key: 'alert.swap_in_use.cause', params: { percent: 31.2, used_mib: 160 } },
  impact: { key: 'alert.swap_in_use.impact', params: { percent: 31.2, used_mib: 160 } },
  next_step: { key: 'alert.swap_in_use.next', params: { percent: 31.2, used_mib: 160 } },
};

export const criticalTemperatureAlert: Alert = {
  id: 'cpu_temperature',
  level: 'critical',
  cause: { key: 'alert.cpu_temperature.critical.cause', params: { celsius: 83, threshold: 80 } },
  impact: { key: 'alert.cpu_temperature.critical.impact', params: { celsius: 83, threshold: 80 } },
  next_step: { key: 'alert.cpu_temperature.critical.next', params: { celsius: 83, threshold: 80 } },
};

export const relevantStorageAlert: Alert = {
  id: 'storage_usage:/',
  level: 'relevant',
  cause: { key: 'alert.storage_usage.relevant.cause', params: { mount: '/', percent: 88.4, threshold: 85 } },
  impact: { key: 'alert.storage_usage.relevant.impact', params: { mount: '/', percent: 88.4, threshold: 85 } },
  next_step: { key: 'alert.storage_usage.relevant.next', params: { mount: '/', percent: 88.4, threshold: 85 } },
};

export const piHistory: History = {
  schema_version: 1,
  interval_seconds: 2,
  cpu_percent: [10, 12, 15, null, 20],
  memory_used_percent: [36, 36.5, 37, 37, 37.2],
  temperature_celsius: [42, 42.5, 43, 43.3, 43.3],
};

export const piMetrics: MetricsResponse = {
  ...piSnapshot,
  device: {
    model: 'Raspberry Pi Zero 2 W Rev 1.0',
    soc_family: 'BCM2837',
    arch: 'arm64',
    os_pretty_name: 'Debian GNU/Linux 13 (trixie)',
    kernel_release: '6.18.50+rpt-rpi-v8',
    support_tier: 'compatible',
    support_reason: 'profile_not_verified',
    support_explanation_key: 'support.compatible.profile_not_verified',
    generic_mode: false,
  },
  alerts: [], // the Compatible tier is shown in the header, not as an alert
  history: piHistory,
};

/** A generic ARM64 device: no temperature sensor, no cpufreq, no swap, one wired interface. */
export const genericMetrics: MetricsResponse = {
  ...piMetrics,
  hostname: 'oarfish-test',
  cpu: { usage_percent: 3, cores: 8, load: { one: 0.5, five: 0.4, fifteen: 0.3 }, frequency: { current_mhz: null, max_mhz: null }, governor: null },
  memory: { total_bytes: 8160000 * 1024, available_bytes: 6000000 * 1024, used_bytes: 2160000 * 1024, swap_total_bytes: null, swap_used_bytes: null },
  temperatures: [],
  storage: [],
  network: [],
  device: {
    model: 'Synthetic Board A1',
    soc_family: null,
    arch: 'arm64',
    os_pretty_name: 'Debian GNU/Linux 12 (bookworm)',
    kernel_release: '6.1.0-synthetic',
    support_tier: 'generic',
    support_reason: 'no_board_profile',
    support_explanation_key: 'support.generic.no_board_profile',
    generic_mode: true,
  },
  alerts: [genericModeAlert],
  history: { schema_version: 1, interval_seconds: 2, cpu_percent: [], memory_used_percent: [], temperature_celsius: [] },
};
