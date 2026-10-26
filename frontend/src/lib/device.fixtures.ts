// Sample /api/v1/device responses for tests. They mirror what the backend
// returns for its synthetic fixtures; they are not data from a real device.
import type { Device } from './device';

const allCapabilities = [
  'procfs',
  'sysfs',
  'systemd',
  'journald',
  'cgroup_v2',
  'cpu_temperature',
  'boot_medium',
].map((id) => ({ id, available: true, source: `/probe/${id}` }));

export const piZero2W: Device = {
  schema_version: 1,
  hardware: {
    vendor: 'Raspberry Pi',
    model: 'Raspberry Pi Zero 2 W Rev 1.0',
    soc_family: 'BCM2837',
    arch: 'arm64',
    revision_code: '902120',
    memory_total_bytes: 415404 * 1024,
  },
  os: {
    id: 'debian',
    id_like: [],
    version_id: '13',
    pretty_name: 'Debian GNU/Linux 13 (trixie)',
    adapter: 'raspberry_pi_os',
    adapter_status: 'initial-target',
  },
  kernel: { release: '6.18.50+rpt-rpi-v8' },
  profile: { id: 'raspberry-pi-zero-2-w', version: '1.0.0', matched_by: 'compatible' },
  support: {
    tier: 'compatible',
    reason: 'profile_not_verified',
    explanation_key: 'support.compatible.profile_not_verified',
    limitations: ['memory_512mb', 'evidence_install_only'],
    board_actions_allowed: true,
  },
  capabilities: allCapabilities,
  sensors: { cpu_temperature_celsius: 43.3 },
  boot_medium: { type: 'mmc' },
};

export const genericArm64: Device = {
  schema_version: 1,
  hardware: {
    vendor: null,
    model: 'Synthetic Board A1',
    soc_family: null,
    arch: 'arm64',
    revision_code: null,
    memory_total_bytes: 8161000 * 1024,
  },
  os: {
    id: 'debian',
    id_like: [],
    version_id: '12',
    pretty_name: 'Debian GNU/Linux 12 (bookworm)',
    adapter: 'debian_family',
    adapter_status: 'generic',
  },
  kernel: { release: '6.1.0-28-arm64' },
  profile: { id: 'generic-arm64', version: '1.0.0', matched_by: 'generic_fallback' },
  support: {
    tier: 'generic',
    reason: 'no_board_profile',
    explanation_key: 'support.generic.no_board_profile',
    limitations: ['generic_mode'],
    board_actions_allowed: false,
  },
  capabilities: [
    ...allCapabilities.filter((c) => c.id !== 'journald'),
    { id: 'journald', available: false, source: '/run/systemd/journal/socket', reason: 'not_found' },
  ],
  sensors: { cpu_temperature_celsius: null },
  boot_medium: { type: 'unknown' },
};

export const unsupportedX86: Device = {
  ...genericArm64,
  hardware: { ...genericArm64.hardware, arch: 'amd64', model: null },
  support: {
    tier: 'unsupported',
    reason: 'not_arm64',
    explanation_key: 'support.unsupported.not_arm64',
    limitations: [],
    board_actions_allowed: false,
  },
};
