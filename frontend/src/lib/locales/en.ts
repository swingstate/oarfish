// English is prepared, not yet selectable in the UI. It must have exactly the
// keys of de.ts (checked by i18n.test.ts).
import type { Messages } from './de';

export const en: { [K in keyof Messages]: string } = {
  tagline: 'Management for ARM single-board computers and embedded Linux',
  statusLabel: 'Oarfish service',
  checking: 'Checking …',
  ok: 'Running',
  unreachable: 'Unreachable',
  recheck: 'Check again',
  foundation:
    'Early development version. It only shows status and device information and has no management functions yet.',

  navLabel: 'Main navigation',
  navStatus: 'Status',
  navDevice: 'Device',

  deviceTitle: 'Device',
  deviceLoading: 'Loading device information …',
  deviceError: 'The device information could not be loaded.',
  deviceReload: 'Reload',
  unknownValue: 'Unknown',
  unavailableValue: 'Not available',

  hardwareHeading: 'Hardware',
  fieldModel: 'Model',
  fieldVendor: 'Vendor',
  fieldSoc: 'SoC family',
  fieldArch: 'Architecture',
  fieldRevision: 'Revision code',
  fieldMemory: 'Memory',

  osHeading: 'Operating system',
  fieldOs: 'System',
  fieldOsAdapter: 'Operating system adapter',
  fieldKernel: 'Kernel',

  supportHeading: 'Support status',
  fieldTier: 'Level',
  fieldProfile: 'Profile',
  fieldMatchedBy: 'Recognised by',
  limitationsHeading: 'Known limitations',

  genericNoticeTitle: 'Generic mode',
  unsupportedNoticeTitle: 'Not supported',

  sensorsHeading: 'Sensors',
  fieldCpuTemp: 'CPU temperature',
  fieldBootMedium: 'Boot medium',

  capabilitiesHeading: 'Capabilities',
  capabilitiesIntro: 'Oarfish only offers a function when the capability it needs was found on this device.',
  capAvailable: 'Available',
  capUnavailable: 'Not available',
  capDetails: 'Technical details',
  capSource: 'Probed',
  capReason: 'Reason',

  'tier.verified': 'Verified',
  'tier.compatible': 'Compatible',
  'tier.community': 'Community',
  'tier.experimental': 'Experimental',
  'tier.generic': 'Generic',
  'tier.unsupported': 'Unsupported',

  'support.unsupported.not_linux':
    'Oarfish is not running on Linux here. Only Linux on 64-bit ARM devices is supported.',
  'support.unsupported.not_arm64':
    'This is not a 64-bit ARM (ARM64) system. Oarfish only supports ARM64 and offers no management functions here. Install a 64-bit ARM operating system.',
  'support.unsupported.missing_minimum_capabilities':
    'Minimum requirements are missing: Oarfish needs procfs, sysfs and systemd. The limitations and capabilities below show which are missing. Oarfish deliberately offers no partial operation here.',
  'support.generic.no_board_profile':
    'There is no profile for this board. Oarfish runs in generic mode: standard Linux information is shown, board-specific actions are not.',
  'support.generic.os_not_covered_by_profile':
    'The board is known, but its profile does not apply to this operating system. Oarfish runs in generic mode without board-specific actions.',
  'support.generic.kernel_too_old':
    'The kernel is older than the profile of this board requires. Oarfish runs in generic mode without board-specific actions.',
  'support.compatible.profile_not_verified':
    'The board and operating system are recognised and supported. This combination has not yet been fully tested on real hardware, so only capabilities that were demonstrably detected here apply.',
  'support.compatible.verified_combination_not_matched':
    'The profile of this board is verified, but exactly this combination of operating system and version was not tested. It is therefore only compatible.',
  'support.community.profile_community':
    'The profile is maintained by the community, not by the Oarfish team.',
  'support.experimental.profile_experimental':
    'The profile is incomplete. Functions may be missing or change.',
  'support.verified.verified_combination':
    'This combination of board and operating system has been fully tested on real hardware.',

  'limitation.generic_mode': 'Generic mode: no board-specific actions',
  'limitation.raspberry_pi_no_board_profile':
    'A Raspberry Pi whose model has no board profile yet',
  'limitation.os_adapter_not_qualified':
    'The adapter for this operating system is not qualified yet',
  'limitation.os_unrecognised': 'The operating system was not recognised',
  'limitation.expected_capability_missing':
    'A capability the profile expects is missing (see capabilities)',
  'limitation.capability_missing_procfs': 'procfs is missing',
  'limitation.capability_missing_sysfs': 'sysfs is missing',
  'limitation.capability_missing_systemd': 'systemd is not the running init system',
  'limitation.memory_512mb': 'Only 512 MB of memory',
  'limitation.evidence_install_only':
    'So far only the installation has been tested on real hardware',

  'capability.procfs': 'Process information (procfs)',
  'capability.sysfs': 'Device information (sysfs)',
  'capability.systemd': 'Service management (systemd)',
  'capability.journald': 'System log (journald)',
  'capability.cgroup_v2': 'Resource groups (cgroup v2)',
  'capability.cpu_temperature': 'CPU temperature sensor',
  'capability.boot_medium': 'Boot medium detected',

  'capabilityReason.not_found': 'Not found',
  'capabilityReason.implausible_value': 'Implausible value',
  'capabilityReason.unrecognised_device': 'Device not recognised',
  'capabilityReason.no_root_mount': 'Root file system could not be determined',
  'capabilityReason.no_sensor': 'No sensor found',
  'capabilityReason.sensor_unreadable': 'Sensor not readable',

  'matchedBy.compatible': 'Device identifier (device tree)',
  'matchedBy.model': 'Model name',
  'matchedBy.generic_fallback': 'No matching board profile',

  'adapter.raspberry_pi_os': 'Raspberry Pi OS',
  'adapter.ubuntu': 'Ubuntu',
  'adapter.debian_family': 'Debian family',
  'adapter.generic_linux': 'Linux (not recognised)',
  'adapterStatus.initial-target': 'First target system, not yet verified',
  'adapterStatus.not-yet-qualified': 'Not yet qualified',
  'adapterStatus.generic': 'General support',
  'adapterStatus.unrecognised': 'Not recognised',

  'bootMedium.mmc': 'SD / MMC card',
  'bootMedium.nvme': 'NVMe',
  'bootMedium.usb': 'USB drive',
  'bootMedium.unknown': 'Unknown',
};
