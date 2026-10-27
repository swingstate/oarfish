// English is prepared, not yet selectable in the UI. It must have exactly the
// keys of de.ts (checked by i18n.test.ts).
import type { Messages } from './de';

export const en: { [K in keyof Messages]: string } = {
  navHome: 'Home',

  homeTitle: 'Home',
  homeLoading: 'Loading values …',
  homeLoadError: 'The measurements could not be loaded.',
  homeRetry: 'Try again',
  technicalDetails: 'Technical details',

  connConnecting: 'Connecting …',
  connLost: 'Connection lost. The values shown are out of date. Oarfish reconnects automatically.',
  connStale: 'No new values have arrived for a few seconds. The values shown may be out of date.',
  connLastUpdate: 'Last update: {time}',
  staleBadge: 'Out of date',

  summaryHeading: 'Device',
  sumHost: 'Host name',
  sumBoard: 'Board',
  sumSoc: 'SoC',
  sumOs: 'Operating system',
  sumUptime: 'Uptime',
  sumSupport: 'Support status',
  sumDeviceLink: 'Details on the Device page',

  uptimeDaysHours: '{days} d {hours} h',
  uptimeHoursMinutes: '{hours} h {minutes} min',
  uptimeMinutes: '{minutes} min',

  alertsHeading: 'Alerts',
  alertsNone: 'No alerts. Everything is in order.',
  alertCause: 'Cause',
  alertImpact: 'Impact',
  alertNext: 'Next step',
  'level.critical': 'Critical',
  'level.relevant': 'Relevant',
  'level.informational': 'Information',

  cpuHeading: 'CPU',
  cpuUsage: 'Usage',
  cpuLoad: 'Load (1 min)',
  cpuFreq: 'Clock',
  cpuFreqOfMax: '{current} of {max}',
  cpuGovernor: 'Governor',
  cpuCores: 'Cores',
  cpuLoadLong: 'Load (5 / 15 min)',
  cpuNoFreq: 'The kernel reports no clock frequency.',

  memHeading: 'Memory',
  memUsed: 'In use',
  memAvailable: 'Available',
  memTotal: 'Total',
  memSwap: 'Swap',
  memSwapNone: 'No swap configured',
  memUsedOf: '{used} of {total}',

  tempHeading: 'Temperature',
  tempNone: 'No temperature sensors found.',
  tempSoc: 'SoC / CPU',
  tempSource: 'Source',
  tempSensorId: 'Sensor',
  'tempSource.thermal_zone': 'Thermal zone',
  'tempSource.hwmon': 'Hardware monitor',

  storageHeading: 'Storage',
  storageNone: 'No storage devices found.',
  storageUsedOf: '{used} of {total} used',
  storageFree: 'Free',
  storageFsType: 'File system',

  netHeading: 'Network',
  netNone: 'No network interfaces found.',
  netAddresses: 'Addresses',
  netNoAddress: 'No address',
  netRx: 'Receive',
  netTx: 'Send',
  netSpeed: 'Link speed',
  netTotals: 'Transferred since boot',
  netRateWaiting: 'measuring …',
  netKind: 'Type',
  'linkState.up': 'Connected',
  'linkState.down': 'Disconnected',
  'linkState.unknown': 'Unknown',
  'linkState.dormant': 'Dormant',
  'linkState.lowerlayerdown': 'Lower layer down',
  'linkState.notpresent': 'Not present',
  'linkState.testing': 'Testing',
  'netKind.ethernet': 'Ethernet',
  'netKind.wifi': 'Wi-Fi',
  'netKind.other': 'Other',

  sparkCpu: 'CPU usage over the last minutes, now {value}',
  sparkMemory: 'Memory use over the last minutes, now {value}',
  sparkTemperature: 'Temperature over the last minutes, now {value}',
  sparkNoData: 'No history yet',

  'alert.cpu_temperature.relevant.cause':
    'The CPU temperature is {celsius} °C. It is reported as elevated from {threshold} °C.',
  'alert.cpu_temperature.relevant.impact':
    'If it keeps rising, the processor lowers its clock speed and the device gets slower.',
  'alert.cpu_temperature.relevant.next': 'Improve ventilation or add a heat sink, and check the load.',
  'alert.cpu_temperature.critical.cause':
    'The CPU temperature is {celsius} °C. From {threshold} °C it is critical.',
  'alert.cpu_temperature.critical.impact':
    'The processor throttles heavily; the device may shut down or show errors.',
  'alert.cpu_temperature.critical.next':
    'Reduce the load and improve cooling. If the temperature does not drop, switch the device off.',
  'alert.memory_available.relevant.cause':
    'Only {percent} % of the memory is available. It is reported below {threshold} %.',
  'alert.memory_available.relevant.impact':
    'The system starts to swap data out and applications get slower.',
  'alert.memory_available.relevant.next': 'Stop applications you do not need, or run fewer at the same time.',
  'alert.memory_available.critical.cause':
    'Only {percent} % of the memory is available. Below {threshold} % it is critical.',
  'alert.memory_available.critical.impact': 'The kernel may kill programs when the memory runs out.',
  'alert.memory_available.critical.next':
    'Stop applications now. In the long run, plan fewer services or use a device with more memory.',
  'alert.swap_in_use.cause': 'Swap is in use with {used_mib} MiB.',
  'alert.swap_in_use.impact': 'Swapping slows the system down and wears out the SD card faster.',
  'alert.swap_in_use.next': 'Relieve the memory: stop applications you do not need.',
  'alert.storage_usage.relevant.cause':
    'The file system {mount} is {percent} % full. It is reported above {threshold} %.',
  'alert.storage_usage.relevant.impact': 'When space runs short, services and updates can fail.',
  'alert.storage_usage.relevant.next': 'Delete data you do not need or use a larger storage device.',
  'alert.storage_usage.critical.cause':
    'The file system {mount} is {percent} % full. Above {threshold} % it is critical.',
  'alert.storage_usage.critical.impact':
    'The file system is almost full. Writes, updates and services can fail now.',
  'alert.storage_usage.critical.next': 'Free up space right away by deleting data you do not need.',
  'alert.network_no_link.cause': 'No network interface has an active connection.',
  'alert.network_no_link.impact':
    'The device cannot be reached over the network. Remote access and updates are not possible.',
  'alert.network_no_link.next': 'Check the cable, the Wi-Fi credentials and the router.',
  'alert.cpu_load.cause':
    'The load over the last minute ({load}) is above the number of CPU cores ({cores}).',
  'alert.cpu_load.impact': 'The device is overloaded and applications respond more slowly.',
  'alert.cpu_load.next': 'Check the running applications and stop compute-heavy tasks.',
  'alert.support_generic.cause': 'This device runs in Generic Mode.',
  'alert.support_generic.impact':
    'Oarfish shows general Linux information. There are no board-specific functions.',
  'alert.support_generic.next':
    'The Device page gives the reason. A matching board profile would unlock more functions.',
  'alert.support_compatible.cause': 'This device is rated compatible but not yet verified.',
  'alert.support_compatible.impact': 'Not every function has been fully tested on real hardware.',
  'alert.support_compatible.next': 'The known limitations are listed on the Device page.',

  navLabel: 'Main navigation',
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
