// German is the default UI language (ORB-GEN-006). English lives in en.ts with
// exactly the same keys; a test checks that.
//
// The file has two groups:
//
//   deUi  - labels and states: everything the interface says about itself
//           (navigation, headings, field names, loading, empty, error and
//           connection texts, status words). Plain German, short sentences,
//           one word for one thing. The owner lifts this group in one piece
//           (docs/UI.md, "State and label texts").
//   deApi - sentences the API selects by key (alerts, support explanations,
//           limitations, capabilities). Keys with a dot are looked up by
//           value that comes from the API.
//
// The two groups are merged into `de`, so `t.homeTitle` and
// `msg('alert.network_no_link.cause')` work the same for both.

// ======================================================================
// GROUP 1: UI TEXTS (labels and states)
// ======================================================================
export const deUi = {
  // ---- Shell and navigation ----
  brandName: 'Oarfish',
  skipLink: 'Zum Inhalt springen',
  navLabel: 'Hauptnavigation',
  navHome: 'Home',
  navDevice: 'Gerät',
  homeTitle: 'Home',
  deviceTitle: 'Gerät',

  // ---- Connection: indicator at the top right ----
  connIndicatorLive: 'Live',
  connIndicatorConnecting: 'Verbindet …',
  connIndicatorReconnecting: 'Verbindet neu …',
  connIndicatorStale: 'Keine neuen Werte',
  connIndicatorPaused: 'Pausiert',

  // ---- States of the Home page ----
  homeLoading: 'Werte werden geladen …',
  homeLoadError: 'Die Werte konnten nicht geladen werden.',
  homeLoadErrorHint: 'Prüfen Sie, ob Oarfish auf dem Gerät läuft, und versuchen Sie es noch einmal.',
  homeRetry: 'Erneut versuchen',
  connConnecting: 'Verbindung wird aufgebaut …',
  connLost: 'Die Verbindung ist unterbrochen. Die Werte sind veraltet. Oarfish verbindet sich automatisch neu.',
  connStale: 'Seit einigen Sekunden kommen keine neuen Werte an. Die Werte sind vielleicht veraltet.',
  connLastUpdate: 'Letzte Aktualisierung: {time}',
  staleBadge: 'Veraltet',
  unknownValue: 'Unbekannt',
  unavailableValue: 'Nicht verfügbar',
  technicalDetails: 'Technische Einzelheiten',

  // ---- Summary strip ----
  summaryHeading: 'Überblick über das Gerät',
  sumHost: 'Hostname',
  sumBoard: 'Board',
  sumSoc: 'SoC',
  sumOs: 'Betriebssystem',
  sumUptime: 'Laufzeit',
  sumSupport: 'Supportstatus',
  sumDeviceLink: 'Details auf der Seite Gerät',
  uptimeDaysHours: '{days} T {hours} Std.',
  uptimeHoursMinutes: '{hours} Std. {minutes} Min.',
  uptimeMinutes: '{minutes} Min.',

  // ---- Alerts ----
  alertsHeading: 'Hinweise',
  alertsNone: 'Keine Hinweise. Alles in Ordnung.',
  alertsNoneStale: 'Beim letzten Abruf gab es keine Hinweise.',
  alertCause: 'Ursache',
  alertImpact: 'Auswirkung',
  alertNext: 'Nächster Schritt',
  'level.critical': 'Kritisch',
  'level.relevant': 'Wichtig',
  'level.informational': 'Information',

  // ---- CPU ----
  cpuHeading: 'CPU',
  cpuUsage: 'Auslastung',
  cpuLoad: 'Last (1 Min.)',
  cpuFreq: 'Takt',
  cpuFreqOfMax: '{current} von {max}',
  cpuGovernor: 'Governor',
  cpuCores: 'Kerne',
  cpuLoadLong: 'Last (5 / 15 Min.)',
  cpuNoFreq: 'Der Kernel meldet keinen Takt.',

  // ---- Memory ----
  memHeading: 'Arbeitsspeicher',
  memUsed: 'Belegt',
  memAvailable: 'Verfügbar',
  memTotal: 'Gesamt',
  memSwap: 'Auslagerungsspeicher (Swap)',
  memSwapNone: 'Kein Swap eingerichtet',
  memUsedOf: '{used} von {total}',

  // ---- Temperature ----
  tempHeading: 'Temperatur',
  tempNone: 'Keine Temperatursensoren gefunden.',
  tempSoc: 'SoC / CPU',
  tempSource: 'Quelle',
  tempSensorId: 'Sensor',
  'tempSource.thermal_zone': 'Thermal Zone',
  'tempSource.hwmon': 'Hardware-Monitor',

  // ---- Storage ----
  storageHeading: 'Speicherplatz',
  storageNone: 'Kein Speicherplatz gefunden.',
  storageUsedOf: '{used} von {total} belegt',
  storageFree: 'Frei',
  storageFsType: 'Dateisystem',

  // ---- Network ----
  netHeading: 'Netzwerk',
  netNone: 'Keine Netzwerkschnittstellen gefunden.',
  netAddresses: 'Adressen',
  netNoAddress: 'Keine Adresse',
  netRx: 'Empfang',
  netTx: 'Senden',
  netSpeed: 'Geschwindigkeit',
  netTotals: 'Übertragen seit dem Start',
  netRateWaiting: 'wird ermittelt …',
  netKind: 'Art',
  'linkState.up': 'Verbunden',
  'linkState.down': 'Kein Link',
  'linkState.unknown': 'Unbekannt',
  'linkState.dormant': 'Wartet',
  'linkState.lowerlayerdown': 'Kein Link',
  'linkState.notpresent': 'Nicht vorhanden',
  'linkState.testing': 'Wird geprüft',
  'netKind.ethernet': 'Ethernet',
  'netKind.wifi': 'WLAN',
  'netKind.other': 'Sonstige',

  // ---- Sparklines (text alternatives) ----
  sparkCpu: 'CPU-Auslastung der letzten Minuten, zuletzt {value}',
  sparkMemory: 'Belegter Arbeitsspeicher der letzten Minuten, zuletzt {value}',
  sparkTemperature: 'Temperatur der letzten Minuten, zuletzt {value}',
  sparkNoData: 'Noch kein Verlauf',

  // ---- Device page ----
  deviceLoading: 'Geräteinformationen werden geladen …',
  deviceError: 'Die Geräteinformationen konnten nicht geladen werden.',
  deviceErrorHint: 'Prüfen Sie, ob Oarfish auf dem Gerät läuft, und laden Sie die Seite noch einmal.',
  deviceReload: 'Erneut laden',

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
  capabilitiesIntro: 'Oarfish bietet eine Funktion nur an, wenn die nötige Fähigkeit auf diesem Gerät gefunden wurde.',
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

  // ---- Access: setup, sign-in, account (ADR 0009) ----
  navAccount: 'Konto',
  accountTitle: 'Konto',
  userMenuLabel: 'Benutzermenü',
  logout: 'Abmelden',
  sessionLoading: 'Sitzung wird geprüft …',
  sessionError: 'Oarfish ist nicht erreichbar.',
  setupTitle: 'Oarfish einrichten',
  setupIntro: 'Legen Sie das Administratorkonto an. Ohne dieses Konto zeigt Oarfish keine Daten des Geräts.',
  setupTokenLabel: 'Einrichtungscode',
  setupTokenHelp: 'Nur wer Zugang zum Gerät hat, kann ihn lesen. Zeigen Sie ihn dort mit diesem Befehl an:',
  setupTokenCommand: 'sudo cat /var/lib/oarfish/setup-token',
  setupBindingHeading: 'Erreichbarkeit',
  setupBinding: 'Oarfish ist nur auf diesem Gerät erreichbar (Loopback, 127.0.0.1). Von einem anderen Rechner aus nutzen Sie einen SSH-Tunnel.',
  usernameLabel: 'Benutzername',
  usernameHelp: '1 bis 32 Zeichen: Kleinbuchstaben, Ziffern, Punkt, Bindestrich und Unterstrich.',
  passwordLabel: 'Passwort',
  newPasswordHelp: 'Mindestens 12 Zeichen. Mehrere Wörter sind leicht zu merken.',
  passwordRepeatLabel: 'Passwort wiederholen',
  setupSubmit: 'Konto anlegen',
  setupWorking: 'Konto wird angelegt …',
  loginTitle: 'Anmelden',
  loginIntro: 'Melden Sie sich mit dem Administratorkonto an.',
  loginSubmit: 'Anmelden',
  loginWorking: 'Anmeldung läuft …',
  loginExpired: 'Die Sitzung ist beendet. Melden Sie sich erneut an.',
  loginLoggedOut: 'Sie sind abgemeldet.',
  accountSignedInAs: 'Angemeldet als {user}.',
  accountSessionNote: 'Eine Sitzung endet nach einer Stunde ohne Aktivität, spätestens nach zwölf Stunden.',
  accountPasswordHeading: 'Passwort ändern',
  currentPasswordLabel: 'Aktuelles Passwort',
  newPasswordLabel: 'Neues Passwort',
  newPasswordRepeatLabel: 'Neues Passwort wiederholen',
  accountSubmit: 'Passwort ändern',
  accountWorking: 'Passwort wird geändert …',
  accountChanged: 'Das Passwort ist geändert. Alle anderen Sitzungen sind beendet.',
  errorCredentials: 'Benutzername oder Passwort ist falsch.',
  errorRateLimitedOneSecond: 'Zu viele Versuche. Warten Sie eine Sekunde und versuchen Sie es dann erneut.',
  errorRateLimitedSeconds: 'Zu viele Versuche. Warten Sie {seconds} Sekunden und versuchen Sie es dann erneut.',
  errorRateLimitedMinutes: 'Zu viele Versuche. Warten Sie {minutes} Minuten und versuchen Sie es dann erneut.',
  errorSetupToken: 'Der Einrichtungscode stimmt nicht. Lesen Sie ihn auf dem Gerät erneut aus.',
  errorSetupDone: 'Das Administratorkonto ist schon eingerichtet. Melden Sie sich an.',
  errorUsername: 'Dieser Benutzername ist nicht erlaubt. Erlaubt sind 1 bis 32 Kleinbuchstaben, Ziffern, Punkt, Bindestrich und Unterstrich.',
  errorPasswordShort: 'Das Passwort ist zu kurz. Es braucht mindestens 12 Zeichen.',
  errorPasswordLong: 'Das Passwort ist zu lang. Es darf höchstens 128 Zeichen haben.',
  errorPasswordCommon: 'Dieses Passwort ist zu verbreitet. Wählen Sie ein anderes.',
  errorPasswordInvalid: 'Das Passwort enthält ein Zeichen, das nicht erlaubt ist.',
  errorPasswordMismatch: 'Die beiden Passwörter sind nicht gleich.',
  errorCurrentPassword: 'Das aktuelle Passwort ist falsch.',
  errorRequired: 'Dieses Feld ist leer.',
  errorNetwork: 'Oarfish ist nicht erreichbar. Prüfen Sie die Verbindung und versuchen Sie es erneut.',
  errorGeneric: 'Das hat nicht funktioniert. Versuchen Sie es erneut.',
} as const;

// ======================================================================
// GROUP 2: API TEXTS (sentences chosen by a key from the API)
// ======================================================================
export const deApi = {
  'alertTitle.cpu_temperature': 'Hohe CPU-Temperatur',
  'alertTitle.memory_available': 'Wenig Arbeitsspeicher frei',
  'alertTitle.swap_in_use': 'Auslagerungsspeicher belegt',
  'alertTitle.storage_usage': 'Speicherplatz wird knapp',
  'alertTitle.network_no_link': 'Keine Netzwerkverbindung',
  'alertTitle.cpu_load': 'Hohe CPU-Last',
  'alertTitle.support_generic': 'Generischer Modus',

  'alert.cpu_temperature.relevant.cause':
    'Die CPU-Temperatur beträgt {celsius} °C. Ab {threshold} °C wird sie als erhöht gemeldet.',
  'alert.cpu_temperature.relevant.impact':
    'Steigt die Temperatur weiter, drosselt der Prozessor seinen Takt und das Gerät wird langsamer.',
  'alert.cpu_temperature.relevant.next':
    'Für bessere Belüftung sorgen oder einen Kühlkörper verwenden und die Last prüfen.',
  'alert.cpu_temperature.critical.cause':
    'Die CPU-Temperatur beträgt {celsius} °C. Ab {threshold} °C ist sie kritisch.',
  'alert.cpu_temperature.critical.impact':
    'Der Prozessor drosselt stark, das Gerät kann sich abschalten oder Fehler zeigen.',
  'alert.cpu_temperature.critical.next':
    'Last verringern und Kühlung verbessern. Sinkt die Temperatur nicht, das Gerät ausschalten.',
  'alert.memory_available.relevant.cause':
    'Nur noch {percent} % des Arbeitsspeichers sind verfügbar. Unter {threshold} % wird es gemeldet.',
  'alert.memory_available.relevant.impact':
    'Das System beginnt, Daten auszulagern, und Anwendungen werden langsamer.',
  'alert.memory_available.relevant.next':
    'Nicht benötigte Anwendungen beenden oder weniger Anwendungen gleichzeitig laufen lassen.',
  'alert.memory_available.critical.cause':
    'Nur noch {percent} % des Arbeitsspeichers sind verfügbar. Unter {threshold} % ist es kritisch.',
  'alert.memory_available.critical.impact':
    'Der Kernel kann Programme zwangsweise beenden, wenn der Speicher ausgeht.',
  'alert.memory_available.critical.next':
    'Sofort Anwendungen beenden. Auf Dauer weniger Dienste einplanen oder ein Gerät mit mehr Speicher nutzen.',
  'alert.swap_in_use.cause': 'Der Auslagerungsspeicher (Swap) ist zu {percent} % belegt ({used_mib} MiB).',
  'alert.swap_in_use.impact':
    'Belegter Swap heißt nicht, dass gerade ausgelagert wird. Wächst die Belegung weiter, kann das System langsamer werden und die SD-Karte schneller verschleißen.',
  'alert.swap_in_use.next':
    'Den Arbeitsspeicher beobachten. Wird auch der verfügbare Speicher knapp, Anwendungen beenden, die nicht gebraucht werden.',
  'alert.storage_usage.relevant.cause':
    'Das Dateisystem {mount} ist zu {percent} % belegt. Über {threshold} % wird es gemeldet.',
  'alert.storage_usage.relevant.impact':
    'Wird der Platz knapp, können Dienste und Updates fehlschlagen.',
  'alert.storage_usage.relevant.next':
    'Nicht benötigte Daten löschen oder einen größeren Datenträger verwenden.',
  'alert.storage_usage.critical.cause':
    'Das Dateisystem {mount} ist zu {percent} % belegt. Über {threshold} % ist es kritisch.',
  'alert.storage_usage.critical.impact':
    'Das Dateisystem ist fast voll. Schreibvorgänge, Updates und Dienste können jetzt fehlschlagen.',
  'alert.storage_usage.critical.next': 'Sofort Platz schaffen, indem nicht benötigte Daten gelöscht werden.',
  'alert.network_no_link.cause': 'Keine Netzwerkschnittstelle hat eine aktive Verbindung.',
  'alert.network_no_link.impact':
    'Das Gerät ist über das Netzwerk nicht erreichbar. Fernzugriff und Updates sind nicht möglich.',
  'alert.network_no_link.next': 'Kabel, WLAN-Zugangsdaten und Router prüfen.',
  'alert.cpu_load.cause':
    'Die Last der letzten Minute ({load}) liegt über der Zahl der CPU-Kerne ({cores}).',
  'alert.cpu_load.impact': 'Das Gerät ist überlastet und Anwendungen reagieren langsamer.',
  'alert.cpu_load.next': 'Laufende Anwendungen prüfen und rechenintensive Aufgaben beenden.',
  'alert.support_generic.cause': 'Dieses Gerät läuft im Generischen Modus.',
  'alert.support_generic.impact':
    'Oarfish zeigt allgemeine Linux-Informationen. Board-spezifische Funktionen gibt es nicht.',
  'alert.support_generic.next':
    'Auf der Seite Gerät steht der Grund. Ein passendes Boardprofil würde mehr Funktionen freischalten.',
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

export const de = { ...deUi, ...deApi } as const;

export type Messages = typeof de;
export type MessageKey = keyof Messages;
