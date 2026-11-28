// Synthetic metrics for tests, shaped like GET /api/v1/metrics and the
// event stream. Not captures of a device. Host name and addresses come from
// the synthetic set: "oarfish-test" and the documentation ranges.
import type { Alert, History, MetricsResponse, NetworkInterface, OtherMount, Routing, Snapshot, Storage } from './metrics';

// Task N0: the routing part. The stub address 127.0.0.53 and the link-local
// next hop have no documentation range; they are the well-known stub address
// and a made-up suffix.
export const piRouting: Routing = {
  ipv4: { status: 'found', interface: 'wlan0', gateway: '192.0.2.1', gateway_hidden: false },
  ipv6: { status: 'found', interface: 'wlan0', gateway: 'fe80::abcd%wlan0', gateway_hidden: false },
  dns: { status: 'found', servers: [{ address: '192.0.2.53', local: false }, { address: '2001:db8::53', local: false }], hidden: 0 },
};

/** Read, but no default route of either family. */
export const noRouteRouting: Routing = {
  ipv4: { status: 'none', interface: null, gateway: null, gateway_hidden: false },
  ipv6: { status: 'none', interface: null, gateway: null, gateway_hidden: false },
  dns: { status: 'none', servers: [], hidden: 0 },
};

/** A resolver stub on the device: resolv.conf names only 127.0.0.53. */
export const stubRouting: Routing = {
  ...piRouting,
  dns: { status: 'found', servers: [{ address: '127.0.0.53', local: true }], hidden: 0 },
};

/** Task N1: the stub, and the two servers behind it read from /run/systemd/resolve/resolv.conf (the Ubuntu shape). */
export const behindStubRouting: Routing = {
  ...piRouting,
  dns: { status: 'found', servers: [{ address: '198.51.100.53', local: false }, { address: '2001:db8:2::53', local: false }], hidden: 0, via_local_resolver: true },
};

/** A router whose addresses contain its hardware address, and a tunnel without a gateway. */
export const hiddenRouting: Routing = {
  ipv4: { status: 'found', interface: 'wg0', gateway: null, gateway_hidden: false },
  ipv6: { status: 'found', interface: 'wlan0', gateway: null, gateway_hidden: true },
  dns: { status: 'found', servers: [{ address: '2001:db8::53', local: false }], hidden: 1 },
};

/** None of the three files could be read. */
export const unreadableRouting: Routing = {
  ipv4: { status: 'unreadable', interface: null, gateway: null, gateway_hidden: false },
  ipv6: { status: 'unreadable', interface: null, gateway: null, gateway_hidden: false },
  dns: { status: 'unreadable', servers: [], hidden: 0 },
};

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
    {
      mount_point: '/',
      fs_type: 'ext4',
      total_bytes: 4096000000,
      used_bytes: 1228800000,
      available_bytes: 2662400000,
      used_percent: 31.6,
      device: 'mmcblk0p2',
      disk: 'mmcblk0',
      removable: false,
      usb: false,
    },
    {
      mount_point: '/boot/firmware',
      fs_type: 'vfat',
      total_bytes: 536870912,
      used_bytes: 53687091,
      available_bytes: 483183821,
      used_percent: 10,
      device: 'mmcblk0p1',
      disk: 'mmcblk0',
      removable: false,
      usb: false,
    },
  ],
  mounts: [{ mount_point: '/tmp', fs_type: 'tmpfs', kind: 'memory', total_bytes: 212000000, used_bytes: 1048576, available_bytes: 210951424, used_percent: 0.5 }],
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
      ipv4_cidr: ['192.0.2.15/24'],
      ipv6_cidr: ['2001:db8::15/64'],
      signal: { level_dbm: -52, link_quality: 58 },
    },
  ],
  routing: piRouting,
};

/** Task N1: one wired interface with a link speed and two families, no Wi-Fi. */
export const ethernetOnly: NetworkInterface[] = [
  {
    name: 'eth0',
    kind: 'ethernet',
    state: 'up',
    link_up: true,
    speed_mbps: 1000,
    rx_bytes: 9000000000,
    tx_bytes: 4000000000,
    ipv4: ['198.51.100.40'],
    ipv6: ['2001:db8:0:1::40'],
    ipv4_cidr: ['198.51.100.40/24'],
    ipv6_cidr: ['2001:db8:0:1::40/64'],
    signal: null,
  },
];

/** Task N1: the default route of the wired fixture. */
export const ethernetRouting: Routing = { ...piRouting, ipv4: { ...piRouting.ipv4, interface: 'eth0' } };

/** Task SF1: Wi-Fi with two IPv6 addresses hidden (they contain a hardware address), and a wired interface with one hidden and no other address. */
export const hiddenAddresses: NetworkInterface[] = [
  { ...piSnapshot.network[0]!, ipv6_hidden: 2 },
  { ...ethernetOnly[0]!, ipv4: [], ipv6: [], ipv4_cidr: [], ipv6_cidr: [], ipv6_hidden: 1 },
];

/** Task N1: a Wi-Fi interface whose driver reports no signal line, and one that is down. */
export const wifiNoSignal: NetworkInterface[] = [
  { ...piSnapshot.network[0]!, signal: null },
  { ...piSnapshot.network[0]!, name: 'wlan1', state: 'down', link_up: false, ipv4: [], ipv6: [], ipv4_cidr: [], ipv6_cidr: [], signal: null, rx_bytes: 0, tx_bytes: 0 },
];

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

// ---- Task ST1: the page Speicher ----

/** A USB stick (removable, on a USB bus) beside the card. The label in the mount point is made up. */
export const usbStick: Storage = {
  mount_point: '/media/DEMO-STICK',
  fs_type: 'exfat',
  total_bytes: 32000000000,
  used_bytes: 8000000000,
  available_bytes: 24000000000,
  used_percent: 25,
  device: 'sda1',
  disk: 'sda',
  removable: true,
  usb: true,
};

/** A network share, an overlay and a memory mount: shown by kind; the share has no size. */
export const otherMounts: OtherMount[] = [
  { mount_point: '/mnt/share', fs_type: 'nfs4', kind: 'network', total_bytes: null, used_bytes: null, available_bytes: null, used_percent: null },
  { mount_point: '/srv/overlay', fs_type: 'overlay', kind: 'overlay', total_bytes: 1000000, used_bytes: 500000, available_bytes: 500000, used_percent: 50 },
  ...(piSnapshot.mounts ?? []),
];

/** The root file system nearly full (above the critical threshold), the boot partition above the relevant one. */
export function nearlyFullStorage(): Storage[] {
  const [root, boot] = structuredClone(piSnapshot.storage);
  return [
    { ...root!, used_bytes: 3940000000, available_bytes: 156000000, used_percent: 96.2 },
    { ...boot!, used_bytes: 470000000, available_bytes: 66870912, used_percent: 87.5 },
  ];
}
