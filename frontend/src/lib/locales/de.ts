// German is the default UI language (ORB-GEN-006). English lives in en.ts with
// exactly the same keys; a test checks that. Keys with a dot are looked up by
// value that comes from the API (limitations, capabilities, support reasons).
export const de = {
  tagline: 'Verwaltung für ARM-Einzelplatinencomputer und Embedded Linux',
  statusLabel: 'Oarfish-Dienst',
  checking: 'Wird geprüft …',
  ok: 'Läuft',
  unreachable: 'Nicht erreichbar',
  recheck: 'Erneut prüfen',
  foundation:
    'Frühe Entwicklungsversion. Es sind nur Status und Geräteinformationen enthalten, noch keine Verwaltungsfunktionen.',

  navLabel: 'Hauptnavigation',
  navStatus: 'Status',
  navDevice: 'Gerät',

  deviceTitle: 'Gerät',
  deviceLoading: 'Geräteinformationen werden geladen …',
  deviceError: 'Die Geräteinformationen konnten nicht geladen werden.',
  deviceReload: 'Erneut laden',
  unknownValue: 'Unbekannt',
  unavailableValue: 'Nicht verfügbar',

  hardwareHeading: 'Hardware',
  fieldModel: 'Modell',
  fieldVendor: 'Hersteller',
  fieldSoc: 'SoC-Familie',
  fieldArch: 'Architektur',
  fieldRevision: 'Revisionscode',
  fieldMemory: 'Arbeitsspeicher',

  osHeading: 'Betriebssystem',
  fieldOs: 'System',
  fieldOsAdapter: 'Betriebssystem-Adapter',
  fieldKernel: 'Kernel',

  supportHeading: 'Supportstatus',
  fieldTier: 'Stufe',
  fieldProfile: 'Profil',
  fieldMatchedBy: 'Erkannt über',
  limitationsHeading: 'Bekannte Einschränkungen',

  genericNoticeTitle: 'Generischer Modus',
  unsupportedNoticeTitle: 'Nicht unterstützt',

  sensorsHeading: 'Sensoren',
  fieldCpuTemp: 'CPU-Temperatur',
  fieldBootMedium: 'Bootmedium',

  capabilitiesHeading: 'Fähigkeiten',
  capabilitiesIntro: 'Oarfish bietet Funktionen nur an, wenn die nötige Fähigkeit auf diesem Gerät gefunden wurde.',
  capAvailable: 'Verfügbar',
  capUnavailable: 'Nicht verfügbar',
  capDetails: 'Technische Einzelheiten',
  capSource: 'Geprüft',
  capReason: 'Grund',

  'tier.verified': 'Verifiziert',
  'tier.compatible': 'Kompatibel',
  'tier.community': 'Community',
  'tier.experimental': 'Experimentell',
  'tier.generic': 'Generisch',
  'tier.unsupported': 'Nicht unterstützt',

  'support.unsupported.not_linux':
    'Oarfish läuft hier nicht unter Linux. Unterstützt wird nur Linux auf 64-Bit-ARM-Geräten.',
  'support.unsupported.not_arm64':
    'Dies ist kein 64-Bit-ARM-System (ARM64). Oarfish unterstützt nur ARM64 und bietet hier keine Verwaltungsfunktionen an. Installieren Sie ein 64-Bit-ARM-Betriebssystem.',
  'support.unsupported.missing_minimum_capabilities':
    'Mindestvoraussetzungen fehlen: Oarfish braucht procfs, sysfs und systemd. Welche fehlen, steht unten bei den Einschränkungen und Fähigkeiten. Oarfish bietet hier bewusst keinen Teilbetrieb an.',
  'support.generic.no_board_profile':
    'Für dieses Board gibt es kein Profil. Oarfish läuft im Generischen Modus: Standard-Linux-Informationen werden angezeigt, board-spezifische Aktionen nicht.',
  'support.generic.os_not_covered_by_profile':
    'Das Board ist bekannt, sein Profil gilt aber nicht für dieses Betriebssystem. Oarfish läuft im Generischen Modus ohne board-spezifische Aktionen.',
  'support.generic.kernel_too_old':
    'Der Kernel ist älter, als das Profil dieses Boards verlangt. Oarfish läuft im Generischen Modus ohne board-spezifische Aktionen.',
  'support.compatible.profile_not_verified':
    'Board und Betriebssystem werden erkannt und unterstützt. Diese Kombination ist aber noch nicht vollständig auf echter Hardware geprüft. Es gelten nur Fähigkeiten, die hier nachweislich erkannt wurden.',
  'support.compatible.verified_combination_not_matched':
    'Das Profil dieses Boards ist verifiziert, aber genau diese Kombination aus Betriebssystem und Version wurde nicht getestet. Sie gilt daher nur als kompatibel.',
  'support.community.profile_community':
    'Das Profil wird von der Community gepflegt, nicht vom Oarfish-Team.',
  'support.experimental.profile_experimental':
    'Das Profil ist unvollständig. Funktionen können fehlen oder sich ändern.',
  'support.verified.verified_combination':
    'Diese Kombination aus Board und Betriebssystem wurde auf echter Hardware vollständig getestet.',

  'limitation.generic_mode': 'Generischer Modus: keine board-spezifischen Aktionen',
  'limitation.raspberry_pi_no_board_profile':
    'Ein Raspberry Pi, für dessen Modell es noch kein Boardprofil gibt',
  'limitation.os_adapter_not_qualified':
    'Der Adapter für dieses Betriebssystem ist noch nicht qualifiziert',
  'limitation.os_unrecognised': 'Das Betriebssystem wurde nicht erkannt',
  'limitation.expected_capability_missing':
    'Eine vom Profil erwartete Fähigkeit fehlt (siehe Fähigkeiten)',
  'limitation.capability_missing_procfs': 'procfs fehlt',
  'limitation.capability_missing_sysfs': 'sysfs fehlt',
  'limitation.capability_missing_systemd': 'systemd läuft nicht als Init-System',
  'limitation.memory_512mb': 'Nur 512 MB Arbeitsspeicher',
  'limitation.evidence_install_only':
    'Auf echter Hardware wurde bisher nur die Installation getestet',

  'capability.procfs': 'Prozessinformationen (procfs)',
  'capability.sysfs': 'Geräteinformationen (sysfs)',
  'capability.systemd': 'Dienstverwaltung (systemd)',
  'capability.journald': 'Systemprotokoll (journald)',
  'capability.cgroup_v2': 'Ressourcengruppen (cgroup v2)',
  'capability.cpu_temperature': 'CPU-Temperatursensor',
  'capability.boot_medium': 'Bootmedium erkannt',

  'capabilityReason.not_found': 'Nicht gefunden',
  'capabilityReason.implausible_value': 'Unplausibler Wert',
  'capabilityReason.unrecognised_device': 'Gerät nicht erkannt',
  'capabilityReason.no_root_mount': 'Root-Dateisystem nicht ermittelbar',
  'capabilityReason.no_sensor': 'Kein Sensor gefunden',
  'capabilityReason.sensor_unreadable': 'Sensor nicht lesbar',

  'matchedBy.compatible': 'Gerätekennung (Device Tree)',
  'matchedBy.model': 'Modellbezeichnung',
  'matchedBy.generic_fallback': 'Kein passendes Boardprofil',

  'adapter.raspberry_pi_os': 'Raspberry Pi OS',
  'adapter.ubuntu': 'Ubuntu',
  'adapter.debian_family': 'Debian-Familie',
  'adapter.generic_linux': 'Linux (nicht erkannt)',
  'adapterStatus.initial-target': 'Erstes Zielsystem, noch nicht verifiziert',
  'adapterStatus.not-yet-qualified': 'Noch nicht qualifiziert',
  'adapterStatus.generic': 'Allgemeine Unterstützung',
  'adapterStatus.unrecognised': 'Nicht erkannt',

  'bootMedium.mmc': 'SD-/MMC-Karte',
  'bootMedium.nvme': 'NVMe',
  'bootMedium.usb': 'USB-Datenträger',
  'bootMedium.unknown': 'Unbekannt',
} as const;

export type Messages = typeof de;
export type MessageKey = keyof Messages;
