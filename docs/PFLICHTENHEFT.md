# Oarfish Pflichtenheft

**GUI-first Management Platform for ARM SBCs and Embedded Linux**

- Dokumentversion: 0.3.3
- Status: Normative Baseline, Umsetzung läuft (Gates G0 bis G4 abgenommen)
- Stand: 27. Oktober 2026
- Zielrelease: Oarfish MVP 0.1
- Produktfokus: Single-Board-Computer, Edge-Geräte und kleine Linux-Appliances
- Initial verifizierte Plattform: Raspberry Pi Zero 2 W mit Raspberry Pi OS Lite 64-bit
- Verbindliches Folgeziel: Ubuntu Server LTS ARM64 auf Raspberry-Pi- und generischer ARM64-Hardware
- Entwicklung: Claude Code auf macOS
- CI und Distribution: GitHub Actions, GitHub Releases, pull-basierte Installation und GUI-Updates

> Dieses Pflichtenheft ist die normative Produkt- und Implementierungsgrundlage. Die Markdown-Fassung unter `docs/PFLICHTENHEFT.md` ist die Source of Truth. Die Umsetzung erfolgt in abgegrenzten Gates. Ein Gate wird erst begonnen, wenn sein Vorgänger abgenommen wurde.

## 0. Dokumentsteuerung

### 0.1 Management Summary

Oarfish ist eine leichte, vollständig GUI-gesteuerte Management-Plattform für ARM-basierte Single-Board-Computer und kleine Edge-Systeme. Der Produktfokus liegt auf Raspberry Pi, später weiteren ARM64-SBCs und Linux-Appliances. Oarfish ist kein generischer Enterprise-Server-Manager und keine eigene Linux-Distribution.

Die erste Entwicklungs-, Laufzeit- und Abnahmeplattform ist ein Raspberry Pi Zero 2 W mit Raspberry Pi OS Lite 64-bit. Ubuntu Server LTS ARM64 ist ein verbindliches Folgeziel. Hardwareerkennung und Betriebssystemerkennung werden deshalb von Beginn an getrennt. Funktionen werden anhand real erkannter Capabilities aktiviert, nicht anhand starrer Annahmen über Distribution, Benutzername oder Dateipfade.

Die Differenzierung entsteht aus fünf Elementen:

1. **Gerätebewusstsein:** Oarfish erkennt Board, SoC-Familie, Betriebssystem, Sensoren, Bootmedium und verfügbare Hardwarefähigkeiten.
2. **GUI-Vollständigkeit:** Jede als unterstützt ausgewiesene Aufgabe ist ohne Terminal, YAML oder manuelle Dateibearbeitung durchführbar.
3. **Plugin-first Produktmodell:** Der Core bleibt klein. Tailscale, Container Management und Oarfish Assist sind optionale First-Party-Plugins.
4. **Embedded-Tauglichkeit:** Ressourcenverbrauch, microSD-Schreiblast, thermisches Verhalten und Recovery sind verbindliche Anforderungen.
5. **Pull-basierte Distribution:** GitHub Actions baut und testet. GitHub Releases verteilt. Der SBC installiert und aktualisiert selbstständig über verifizierte Release-Artefakte.

Der Raspberry Pi Zero 2 W ist die bewusst knappe Ressourcenbaseline. Builds finden auf dem MacBook oder auf GitHub-hosted Runnern statt, niemals auf dem Zielgerät. Das Zielgerät benötigt im Betrieb weder Node.js noch Python, Datenbank, Git oder Build-Toolchain.

### 0.2 Bestätigte Leitentscheidungen

| ID | Entscheidung | Status |
|---|---|---|
| B-01 | Oarfish fokussiert ARM-SBCs, Edge-Geräte und Embedded Linux. | bestätigt |
| B-02 | Raspberry Pi Zero 2 W mit Raspberry Pi OS Lite 64-bit ist die einzige physische MVP-Referenzplattform. | bestätigt |
| B-03 | Ubuntu Server LTS ARM64 ist ein verbindliches Folgeziel und wird architektonisch von Beginn an berücksichtigt. | bestätigt |
| B-04 | Hardwareprofil und Betriebssystemadapter sind voneinander getrennt. | bestätigt |
| B-05 | Armbian und Rockchip sind wichtige Post-MVP-Ziele, aber keine Voraussetzung für die MVP-Abnahme. | bestätigt |
| B-06 | Tailscale ist ein optionales First-Party-Plugin und keine Core-, Installations- oder Update-Abhängigkeit. | bestätigt |
| B-07 | Container Management ist ein optionales First-Party-Plugin mit Docker- und Podman-Adaptern. | bestätigt |
| B-08 | Oarfish Assist ist ein optionales Plugin für Cloud- oder lokale LLM-Endpunkte. | bestätigt |
| B-09 | Entwicklung erfolgt mit Claude Code auf dem MacBook in kleinen Gates. | bestätigt |
| B-10 | GitHub Actions führt CI, Cross-Builds und Release-Erstellung aus, aber kein Deployment auf den Pi. | bestätigt |
| B-11 | Erstinstallation erfolgt über ein Installationsskript, spätere Updates über die Oarfish-GUI. | bestätigt |
| B-12 | Self-Update, Signaturprüfung, atomare Aktivierung und Rollback gehören zum Core. | bestätigt |

### 0.3 Freigabereihenfolge

1. Dieses Pflichtenheft als Version 0.3 prüfen und als Gate G0 freigeben.
2. Gate G1 mit dem bereitgestellten Claude-Code-Prompt ausführen.
3. Repository-Skeleton, CI und ARM64-Cross-Build abnehmen.
4. Gate G2 für die GitHub-Release-Pipeline separat beauftragen.
5. Gate G3 für das Installationsskript separat beauftragen.
6. Erst danach Produktfunktionen, Self-Update und Plugins implementieren.
7. Ubuntu Server LTS ARM64 nach stabiler Raspberry-Pi-OS-Basis qualifizieren.
8. Weitere SBC-Familien und Armbian nach dem MVP physisch testen.

### 0.4 Inhaltsübersicht

| Nr. | Abschnitt |
|---:|---|
| 1 | Produktauftrag und Positionierung |
| 2 | Stakeholder, Nutzer und Einsatzkontext |
| 3 | Zielhardware, Betriebssysteme und Supportmodell |
| 4 | Scope und Abgrenzung |
| 5 | Leitende Anwendungsfälle und Aktionsmodell |
| 6 | Funktionale Anforderungen |
| 7 | UX- und Interaktionsanforderungen |
| 8 | Technische Zielarchitektur |
| 9 | GitHub Actions, Releases, Installation und Self-Update |
| 10 | Security, Datenschutz und AI Governance |
| 11 | Nichtfunktionale Anforderungen |
| 12 | Test- und Abnahmekonzept |
| 13 | Betrieb, Recovery und Observability |
| 14 | Schrittweiser Realisierungsplan mit Claude Code |
| 15 | Risiken und Gegenmaßnahmen |
| 16 | Definition of Done |
| 17 | Verbleibende Entscheidungen und Defaultwerte |
| 18 | Glossar, Referenzen und Versionshistorie |

## 1. Produktauftrag und Positionierung

### 1.1 Produktvision

Oarfish macht ARM-SBCs und kleine Edge-Systeme so einfach administrierbar wie ein modernes Consumer-Gerät, ohne technische Kontrolle oder Transparenz zu verlieren. Der Benutzer soll ein Gerät einschalten, Oarfish im Browser öffnen und Zustand, Konnektivität, Anwendungen und Wartungsbedarf verstehen können.

Das Produktversprechen lautet:

> **Powerful, aber vollständig GUI-driven.** Unterstützte Aufgaben benötigen keine Shell, keine Konfigurationsdateien und kein Vorwissen über distributionsspezifische Befehle. Technische Details bleiben einsehbar, sind aber keine Voraussetzung.

### 1.2 Produktkategorie

Oarfish ist eine **lokale Management- und Erweiterungsplattform für SBCs und Embedded Linux**. Oarfish läuft auf einem bereits installierten Linux-System und ergänzt es um:

- verständliche Hardware- und Systemdiagnose,
- sichere, geführte GUI-Workflows,
- optionale Funktionen über Plugins,
- kontrollierte AI-Unterstützung,
- einen reproduzierbaren Installations- und Updatepfad.

Oarfish ist keine Linux-Distribution, kein Image-Builder, kein Kernel-Buildsystem und keine Fleet-Management-Plattform im MVP.

### 1.3 Abgrenzung zu Armbian

Armbian ist eine zentrale Produktanalogie für den SBC-Fokus. Armbian adressiert Betriebssystem-Images, Board-Support, Kernel, Bootloader und Boardkonfiguration. Oarfish setzt darüber als browserbasierte Laufzeitverwaltung an.

| Aspekt | Armbian | Oarfish |
|---|---|---|
| Primäre Aufgabe | OS-Images, Kernel, Bootloader und Board-Support | Laufzeitverwaltung, GUI-Workflows, Plugins und Diagnose |
| Zeitpunkt | Build, Installation und Systemkonfiguration | Betrieb eines bereits laufenden Geräts |
| Hardwaremodell | Board- und SoC-Familienkonfiguration | Runtime Board Profiles und Capability Detection |
| Bedienung | CLI, TUI und Skripting | Browser-GUI mit Progressive Disclosure |
| Ergebnis | Linux auf vielen SBCs nutzbar machen | SBCs sicher und verständlich betreiben |

### 1.4 Strategische Differenzierung

- **Board-aware statt nur Linux-aware:** Oarfish kennt Modell, SoC-Familie, Supportstatus und Hardwarefähigkeiten.
- **OS-offen innerhalb der Zielbasis:** Raspberry Pi OS zuerst, Ubuntu Server LTS ARM64 als verbindliches Folgeziel.
- **Embedded Health:** Temperatur, Frequenz, Throttling, Unterspannung, Bootmedium und Schreiblast sind zentrale Produktobjekte.
- **Plugin-first:** Tailscale, Container und AI sind optionale, klar abgegrenzte Module.
- **Task-oriented UX:** Der Benutzer wählt Ziele statt Dateien oder Kommandos.
- **Sichere Recovery:** Netzwerk-, Update- und Storage-Änderungen benötigen Vorschau, Rückfallweg und Verifikation.
- **Kleine Runtime:** Der Core muss auf einem Zero 2 W sinnvoll laufen.

### 1.5 Messbare Erfolgskriterien

| Dimension | Zielwert für MVP |
|---|---|
| Referenzplattform | Raspberry Pi Zero 2 W mit Raspberry Pi OS Lite 64-bit ist physisch verifiziert. |
| Ubuntu-Bereitschaft | Architektur, Dateipfade, Detection und Adapter benötigen für Ubuntu Server LTS ARM64 keine grundlegende Neustrukturierung. |
| GUI-Komplettheit | Alle freigegebenen Kernaufgaben funktionieren ohne Terminal oder Dateiedit. |
| Hardwareerkennung | Board, OS, Architektur, Kernel und verfügbare Sensoren werden nachvollziehbar erkannt. |
| Performance | Core Idle-RSS Ziel kleiner oder gleich 35 MB, tolerierter Sollwert kleiner oder gleich 45 MB. |
| Installation | Frische Raspberry-Pi-OS-Installation lässt sich über ein geprüftes Installationsskript einrichten. |
| Self-Update | Neue signierte Releases lassen sich über die GUI installieren und bei Fehler automatisch zurückrollen. |
| Erweiterbarkeit | Tailscale, Containers und Oarfish Assist sind vom Core sauber abgegrenzt. |
| Sicherheit | Kein generischer Root-Executor, kein Shell-Tool für das LLM, keine Secrets in UI oder Logs. |
| Nutzbarkeit | Zustand und nächste Handlung sind auf der Home-Seite innerhalb von 30 Sekunden erfassbar. |

## 2. Stakeholder, Nutzer und Einsatzkontext

### 2.1 Rollen

| Rolle | Ziel | Rechte im MVP |
|---|---|---|
| Product Owner | Scope, UX, Prioritäten und Baseline freigeben. | Keine besondere Laufzeitrolle. |
| Geräteadministrator | SBC verstehen, konfigurieren und Anwendungen betreiben. | Lokaler Admin mit bestätigungspflichtigen Aktionen. |
| Entwickler | Code mit Claude Code entwickeln und über GitHub versionieren. | Repository-Zugriff, kein automatischer Zugriff auf Zielgeräte. |
| GitHub Actions | Tests, Build, Paketierung und Release-Erstellung ausführen. | Nur Repository- und Release-Berechtigungen, kein Pi-Zugriff. |
| Installer | Erstinstallation eines verifizierten Release-Bundles. | Einmalig mit explizitem Root-Aufruf auf dem Zielgerät. |
| Update Helper | Geprüfte Oarfish-Releases atomar aktivieren oder zurückrollen. | Eng begrenzte Root-Rechte für Oarfish-Releasepfade und Dienstneustart. |
| Plugin | Optionale Funktion bereitstellen. | Nur deklarierte und genehmigte Capabilities. |
| Oarfish Assist | Diagnosen und Vorschläge liefern. | Nur explizit erlaubte, typisierte Read-Tools im MVP. |

### 2.2 Zielnutzer

- Technisch interessierte Betreiber von Home-Lab- und Smart-Home-Systemen.
- Entwickler, die ARM-SBCs für Prototypen und kleine Dienste einsetzen.
- Kleine Integratoren, die Linux-basierte Edge-Geräte ausliefern.
- Betreiber von Gateways, Sensor-Hubs, Medien- oder Netzwerk-Appliances.
- Nutzer, die Linux-Funktionalität benötigen, aber keine tägliche Shell-Administration wünschen.

### 2.3 Typische Einsatzszenarien

| Szenario | Beispiel |
|---|---|
| Home-Lab-Appliance | Raspberry Pi mit DNS, Reverse Proxy, Home Automation oder Monitoring. |
| Edge Gateway | Kleines ARM-Gerät mit VPN, Netzwerkdiensten und Container-Workloads. |
| Embedded Appliance | Hersteller liefert ein vorkonfiguriertes Gerät mit Oarfish als lokale Managementoberfläche. |
| Entwicklungsboard | Entwickler testet Services auf einem Pi und später weiteren ARM-SBCs. |
| Remote Device | Gerät wird über ein optionales Konnektivitäts-Plugin erreichbar gemacht. |

### 2.4 Systemkontext

| Komponente | Rolle |
|---|---|
| MacBook | Claude Code, lokale Entwicklung, Git-Operationen und optionale lokale Tests. |
| GitHub Repository | Source of Truth für Code, Dokumentation, Issues und Pull Requests. |
| GitHub-hosted Runner | Tests, Frontend-Build, Go-Cross-Build, Checksums und Releases. |
| GitHub Releases | Versionierte, unveränderliche Distributionsartefakte. |
| Raspberry Pi Zero 2 W | Primäre reale Laufzeit-, Integrations- und Abnahmeplattform. |
| Browser | Vollständige Bedienoberfläche von Oarfish. |
| Externer LLM-Endpoint | Optionales Frontier-Modell oder lokales Modell auf einem anderen Rechner. |

Es gibt keinen Self-hosted GitHub Runner auf MacBook oder Pi. GitHub Actions verbindet sich nicht per SSH oder Tailscale zum Zielgerät. Der Installations- und Updateprozess wird vom SBC als HTTPS-Pull gestartet.

## 3. Zielhardware, Betriebssysteme und Supportmodell

### 3.1 Architektur- und Gerätefokus

| Ebene | Status | Bemerkung |
|---|---|---|
| ARM64 / AArch64 | MUSS | Primäre Zielarchitektur. |
| ARMv7 / armhf | Post-MVP | Optionaler Buildpfad, keine MVP-Abnahme. |
| x86_64 | Entwicklung | Lokale Tests möglich, kein primärer Produktfokus. |
| RISC-V 64 | später | Architektur offen halten, keine MVP-Abnahme. |
| OpenWrt / BusyBox | nicht MVP | Eigenes Init-, Paket- und Sicherheitsmodell. |

### 3.2 Hardwarefamilien

| Familie | MVP-Rolle | Supportaussage |
|---|---|---|
| Raspberry Pi / Broadcom BCM27xx | Primär | Zero 2 W ist das einzige physisch verifizierte Referenzgerät. |
| Andere Raspberry-Pi-Modelle ARM64 | Generic, mit Profil Compatible | Compatible, sobald ein Board- oder Familienprofil existiert (Datenänderung). Bis dahin Generic mit den erkannten Pi-Fähigkeiten und ohne board-spezifische Aktionen (3.6). Verified erst nach physischer Verifikation. |
| Rockchip RK35xx und Derivate | Post-MVP | Generic ARM64 oder spätere Boardprofile, keine MVP-Voraussetzung. |
| Allwinner und Amlogic | Post-MVP | Generic ARM64, spätere Community- oder First-Party-Profile. |
| Generic ARM64 | MUSS | Sichere Basisansicht ohne board-spezifische Schreibaktionen. |

### 3.3 Betriebssystembaseline

Oarfish MUSS Raspberry Pi OS Lite 64-bit und später Ubuntu Server LTS ARM64 unterstützen. Die Implementierung und physische MVP-Abnahme beginnen auf Raspberry Pi OS Lite 64-bit mit einem Raspberry Pi Zero 2 W.

Ubuntu Server LTS ARM64 ist ein verbindliches Folgeziel. Architektur, Plattformdetektion, Dateipfade, Benutzerannahmen und Systemadapter müssen von Beginn an so gestaltet werden, dass Ubuntu-Unterstützung keine grundlegende Neustrukturierung erfordert.

| Betriebssystem | Zielstatus | Zeitpunkt |
|---|---|---|
| Raspberry Pi OS Lite 64-bit | Verified | MVP |
| Ubuntu Server LTS ARM64 | Mandatory Compatible, danach Verified | Nach stabiler Pi-OS-Basis |
| Armbian ARM64, Debian- oder Ubuntu-basiert | Compatible / Post-MVP | Nach Ubuntu-Qualifikation |
| Debian ARM64 mit systemd | Generic | Basisfunktionen ohne volle Supportzusage |
| Fedora, RHEL, openSUSE ARM | später | Adapterarchitektur darf Erweiterung nicht blockieren |
| Ubuntu Core | nicht MVP | Separates Immutable- und Snap-Modell erforderlich |
| Alpine / OpenRC | nicht MVP | Kein systemd, separates Architekturmodell erforderlich |

### 3.4 Trennung von Hardware und Betriebssystem

Oarfish modelliert Hardware und Betriebssystem unabhängig:

```text
Hardware Profile
  Raspberry Pi Zero 2 W
  Generic ARM64
  später Rockchip Board
        +
Operating System Adapter
  Raspberry Pi OS
  Ubuntu Server
  später Armbian
        =
Runtime Capability Set
```

Die Hardwareerkennung darf nicht von Raspberry Pi OS abhängen. Ein Raspberry Pi mit Ubuntu Server muss weiterhin als Raspberry Pi erkannt werden. Umgekehrt darf ein Ubuntu-System ohne Pi-spezifische Hardware keine Pi-Aktionen erhalten.

### 3.5 Detection und Capability-Modell

Primäre Quellen:

- `/etc/os-release` für Betriebssystemidentität,
- `/proc`, `/sys`, Device Tree und DMI, soweit vorhanden, für Hardware,
- systemd und D-Bus für Dienste,
- procfs, sysfs und hwmon für Metriken,
- installierte Binaries und Sockets für optionale Fähigkeiten.

Oarfish darf keine festen Annahmen über folgende Werte treffen:

- Benutzername `pi`,
- Home-Verzeichnis `/home/pi`,
- Bootdatei `/boot/config.txt`,
- Verfügbarkeit von `raspi-config` oder `vcgencmd`,
- dhcpcd, NetworkManager, systemd-networkd oder Netplan als einziges Netzwerkbackend.

Raspberry-Pi-spezifische Werkzeuge sind optionale Enhancement Provider. Standard-Linux-Schnittstellen haben Vorrang.

### 3.6 Oarfish Supportstufen

| Stufe | Bedeutung | Abnahmeanforderung |
|---|---|---|
| Verified | Konkrete Board- und OS-Kombination physisch getestet. | Alle ausgewiesenen Kernfunktionen getestet. |
| Compatible | Architektur und OS-Familie werden unterstützt, Kombination noch nicht voll qualifiziert. | Nur nachweislich erkannte Capabilities. |
| Community | Profil wird von der Community gepflegt. | Deutliche Kennzeichnung und eigener Maintainerstatus. |
| Experimental | Funktion oder Profil ist unvollständig. | Opt-in und Warnung. |
| Generic | Kein passendes Boardprofil, Standard-Linux-Fähigkeiten vorhanden. | Keine board-spezifischen Schreibaktionen. |
| Unsupported | Mindestvoraussetzungen fehlen. | Diagnosehinweis, kein unsicherer Teilbetrieb. |

### 3.7 Board- und SoC-Profilmodell

Oarfish verwendet ein zweistufiges Profilmodell für Laufzeitfähigkeiten:

**SoC Family Profile** enthält gemeinsame Sensor-, Frequenz- und Thermal-Eigenschaften.

**Board Profile** ergänzt Hersteller, Modell, Revision, Speicher- und Boottopologie, getestete OS-Kombinationen, Supportstufe und bekannte Einschränkungen.

Profile dürfen keine beliebigen Shellkommandos enthalten. Sie referenzieren ausschließlich versionierte Handler im Core oder in signierten Plugins.

### 3.8 Technische Referenzplattformen

| Referenz | Zweck | Mindestnachweis |
|---|---|---|
| Raspberry Pi Zero 2 W mit Raspberry Pi OS Lite 64-bit | Ressourcenbaseline und MVP-Abnahme | Installation, Dashboard, Systemfunktionen, Self-Update, Rollback und Plugins. |
| Ubuntu Server LTS ARM64 auf Raspberry Pi oder ARM64-Testsystem | Verbindliche Folgequalifikation | OS-Erkennung, APT, systemd, Netzwerkadapter und identisches Release-Bundle. |
| Generic ARM64 Testumgebung | Schnelle automatisierte Tests | API, Frontend, Generic Mode und Adapter-Fixtures. |

## 4. Scope und Abgrenzung

### 4.1 Im MVP enthalten

- Einzelgeräte-Verwaltung für ARM64-SBCs mit systemd.
- Raspberry Pi Zero 2 W mit Raspberry Pi OS Lite 64-bit als Verified-Plattform.
- Architekturvorbereitung für Ubuntu Server LTS ARM64.
- Lokale Anmeldung und sichere Sitzung für einen Administrator.
- Geräteerkennung mit Board-, OS-, Kernel- und Architekturinformationen.
- Home-Dashboard mit realen Ressourcen-, Temperatur-, Speicher- und Netzwerkdaten.
- Anzeige von Throttling, CPU-Frequenz und Power-Warnungen, soweit erkannt.
- systemd-Dienste, Journal, Prozesse, Netzwerk- und Storage-Übersicht.
- Sicheres Reboot und Shutdown mit Bestätigung.
- Plugin Manager mit versionierten Manifesten und Capability-Freigabe.
- First-Party-Plugins für Tailscale, Containers und Oarfish Assist.
- GitHub-Actions-CI und Linux-ARM64-Release-Builds.
- Erstinstallation über `install.sh` aus einem GitHub Release.
- GUI-basierter Self-Update-Prozess mit Signaturprüfung, Healthcheck und Rollback.

### 4.2 Core und First-Party-Plugins

| Bestandteil | Zweck | Deaktivierbar |
|---|---|---|
| Oarfish Core | Dashboard, System, Auth, Audit, Installer-Metadaten und Self-Update. | nein |
| Tailscale Plugin | Remote-Konnektivität und Tailnet-Status. | ja |
| Containers Plugin | Docker- und Podman-Verwaltung. | ja |
| Oarfish Assist Plugin | LLM-gestützte Diagnose und Erklärungen. | ja |

Der Core muss ohne alle Plugins vollständig starten. Tailscale ist weder für Installation, Updates noch Recovery erforderlich.

### 4.3 Nicht im MVP enthalten

- Eigene Linux-Distribution oder eigenes SBC-Image.
- Kernel-, U-Boot-, Firmware- oder Device-Tree-Buildsystem.
- Physische Rockchip-Qualifikation als Release-Gate.
- Fleet Management und zentrale Steuerung vieler Geräte.
- Kubernetes, Swarm, Nomad oder Cluster-Orchestrierung.
- Öffentlicher Plugin-Marketplace und ungeprüfte Drittplugins.
- Vollständiger Docker-Compose- und Podman-Quadlet-Editor.
- Lokale LLM-Inferenz auf dem Zero 2 W.
- GitHub-Deployment per SSH auf das Zielgerät.
- Self-hosted GitHub Runner auf MacBook oder SBC.
- Öffentliche Internet-Exponierung des Management-UIs als Default.
- Ubuntu Core oder nicht-systemd-basierte Distributionen.

### 4.4 Ziel nach dem MVP

- Ubuntu Server LTS ARM64 als Verified-Plattform.
- Armbian-Qualifikation und weitere Raspberry-Pi-Modelle.
- Rockchip-, Allwinner- und Amlogic-Boardprofile.
- Signierte externe Plugins und kuratierter Katalog.
- Vollständige Compose- und Quadlet-Workflows.
- Wi-Fi-, Access-Point-, WireGuard- und Firewall-Plugins.
- Read-only RootFS, OverlayFS und SD-Karten-Schonmodus.
- Fleet-Inventar und optionaler zentraler Managementdienst.
- GPIO-, I2C-, SPI-, UART- und HAT-Erweiterungen über Hardware-Plugins.

## 5. Leitende Anwendungsfälle und Aktionsmodell

### 5.1 Anwendungsfälle

| ID | Anwendungsfall | Kurzablauf | Ergebnis |
|---|---|---|---|
| UC-01 | Ersteinrichtung | Admin installiert Oarfish, öffnet die GUI, erstellt ein Konto und prüft die erkannte Plattform. | Geschütztes, einsatzbereites Gerät. |
| UC-02 | Gerät verstehen | Home zeigt Boardmodell, OS, Temperatur, Last, Speicher und Warnungen. | Zustand ist ohne Shell verständlich. |
| UC-03 | Thermisches Problem analysieren | Oarfish erkennt hohe Temperatur oder Throttling und zeigt Ursache und Maßnahmen. | Nutzer kann Kühlung oder Last gezielt prüfen. |
| UC-04 | Dienst sicher neu starten | Dienst auswählen, Auswirkungen prüfen, bestätigen und verifizieren. | Aktion und Ergebnis sind auditiert. |
| UC-05 | Tailscale einrichten | Plugin aktivieren, Berechtigungen bestätigen, anmelden und Status prüfen. | Gerät ist optional über das Tailnet erreichbar. |
| UC-06 | Container verwalten | Plugin aktivieren, Engine erkennen, Logs ansehen und Lifecycle steuern. | Workload wird kontrolliert verwaltet. |
| UC-07 | Anwendung bereitstellen | Vorlage wählen, Werte setzen, Plan prüfen und deployen. | Container läuft oder Fehler ist verständlich. |
| UC-08 | AI-Diagnose | Frage stellen, erlaubte Daten sammeln und Antwort mit Quellen erhalten. | Fakten, Vermutungen und Maßnahmen sind getrennt. |
| UC-09 | Plugin verwalten | Pluginrechte prüfen, aktivieren, aktualisieren oder deaktivieren. | Erweiterung bleibt kontrollierbar. |
| UC-10 | Oarfish installieren | Nutzer startet geprüftes `install.sh`, Architektur und OS werden erkannt, Release wird verifiziert. | Oarfish läuft als systemd-Dienst. |
| UC-11 | Oarfish aktualisieren | GUI erkennt neues Release, zeigt Änderungen, installiert und prüft es. | Neue Version läuft oder Rollback ist aktiv. |
| UC-12 | Board ohne Profil | Oarfish startet im Generic Mode und bietet nur sichere Standardfunktionen. | Keine falschen Hardwareaktionen werden angeboten. |

### 5.2 Einheitliches Aktionsmuster

1. Benutzer wählt ein Ziel oder beschreibt es in natürlicher Sprache.
2. Oarfish liest den relevanten Ist-Zustand und prüft Hardware-, OS- und Pluginfähigkeiten.
3. Oarfish zeigt verständliche Optionen und empfohlene Defaults.
4. Oarfish validiert Eingaben und erstellt eine technische Änderungsvorschau.
5. Der Administrator bestätigt eine zustandsändernde Aktion.
6. Oarfish autorisiert, führt aus und verifiziert den neuen Zustand.
7. Ergebnis, Recovery-Hinweis und Audit-Eintrag werden sichtbar gemacht.

### 5.3 Recovery-Prinzip

Änderungen an Netzwerk, Bootpfad, Self-Update oder Storage dürfen nur angeboten werden, wenn Oarfish einen nachvollziehbaren Rückfallweg besitzt. Wenn ein sicherer Rückfallweg fehlt, ist die Aktion read-only oder nicht verfügbar.

## 6. Funktionale Anforderungen

Die folgenden Anforderungen sind verbindliche Grundlage für Implementierung, Tests und Abnahme. MUSS ist zwingend, SOLL ist nur begründet abweichbar, KANN ist optional. Anforderungs-IDs bleiben über Versionen stabil.

### 6.1 Grundprinzipien und Onboarding

| ID | Anforderung | Prio | Abnahmekriterium |
|---|---|---|---|
| ORB-GEN-001 | Alle als unterstützt ausgewiesenen Administrationsaufgaben müssen vollständig über die Web-GUI ausführbar sein. | MUSS | Für jede Aufgabe existiert ein dokumentierter GUI-Pfad ohne Shell, YAML oder Dateiedit. |
| ORB-GEN-002 | Die Oberfläche muss aufgabenorientierte Begriffe verwenden und Linux-Interna standardmäßig ausblenden. | MUSS | Standardansichten zeigen Ziele, Zustand und Auswirkungen. |
| ORB-GEN-003 | Erweiterte Optionen müssen über Progressive Disclosure verfügbar sein. | MUSS | Erweiterte Felder erscheinen erst nach expliziter Auswahl. |
| ORB-GEN-004 | Oarfish muss Funktionen anhand real erkannter Capabilities aktivieren. | MUSS | Nicht verfügbare Hardware- oder Pluginfunktionen werden nicht als aktive Aktion gezeigt. |
| ORB-GEN-005 | Das Produkt muss vollständig ohne Container- oder AI-Plugin starten. | MUSS | Core-Dashboard, Anmeldung und Plugin Manager funktionieren. |
| ORB-GEN-006 | Die GUI muss Deutsch unterstützen; Englisch muss technisch vorbereitet sein. | SOLL | UI-Texte stammen aus Übersetzungsressourcen. |
| ORB-ONB-001 | Der erste Start muss einen geführten Einrichtungsassistenten anbieten. | MUSS | Konto, Binding, Gerätename, erkannte Plattform und optionale Plugins werden in einem Ablauf gezeigt. |
| ORB-ONB-002 | Beim ersten Start muss ein lokales Administratorkonto eingerichtet werden. | MUSS | Ohne Kontoanlage ist kein normaler Verwaltungszugriff möglich. |
| ORB-ONB-003 | Oarfish muss standardmäßig auf Loopback oder einem explizit ausgewählten privaten Interface lauschen. | MUSS | Keine öffentliche Bindung ohne bewusste Warnung und Freigabe. |
| ORB-ONB-004 | Plugins dürfen nicht ungefragt installiert oder aktiviert werden. | MUSS | Jede Installation zeigt Paketquelle, Berechtigungen und Auswirkungen. |
| ORB-ONB-005 | AI-Funktionen müssen standardmäßig deaktiviert sein. | MUSS | Ohne Konfiguration erfolgt kein Provider-Aufruf. |

### 6.2 Board-, OS- und Capability-Erkennung

| ID | Anforderung | Prio | Abnahmekriterium |
|---|---|---|---|
| ORB-HW-001 | Oarfish muss Boardmodell, Hersteller, SoC-Familie, Architektur, OS und Kernel erkennen. | MUSS | Erkennung ist in API und GUI nachvollziehbar. |
| ORB-HW-002 | Hardwareerkennung und Betriebssystemerkennung müssen voneinander getrennt sein. | MUSS | Raspberry-Pi-Hardware wird unter Raspberry Pi OS und Ubuntu identisch klassifiziert. |
| ORB-HW-003 | Betriebssystemidentität muss primär aus `/etc/os-release` gelesen werden. | MUSS | ID, ID_LIKE, VERSION_ID und PRETTY_NAME werden getestet. |
| ORB-HW-004 | Funktionen müssen anhand realer Capabilities aktiviert werden. | MUSS | Fehlende Tools oder Backends erzeugen keinen fehlerhaften Aktionspfad. |
| ORB-HW-005 | Oarfish darf Benutzername `pi`, `/home/pi` oder ein bestimmtes Netzwerkbackend nicht voraussetzen. | MUSS | Tests mit abweichendem Benutzer und Backend bestehen. |
| ORB-HW-006 | Standard-Linux-Schnittstellen wie procfs, sysfs, hwmon, systemd und journald haben Vorrang. | MUSS | Pi-spezifische Tools sind nur optionale Enhancements. |
| ORB-HW-007 | Ein unbekanntes ARM64-System muss im Generic Mode starten. | MUSS | Core-Dashboard funktioniert, board-spezifische Aktionen fehlen. |
| ORB-HW-008 | Board- und SoC-Profile müssen schema-validiert und datenbasiert sein. | MUSS | Profile enthalten keine freien Shellkommandos. |
| ORB-HW-009 | Supportstatus und bekannte Einschränkungen müssen sichtbar sein. | MUSS | Verified, Compatible, Experimental oder Generic erscheint in der GUI. |
| ORB-HW-010 | Ubuntu Server LTS ARM64 muss als eigener OS-Adapter vorgesehen sein. | MUSS | Architekturtest bestätigt getrennte OS- und Hardwareinterfaces. |

### 6.3 Dashboard, Temperatur, Power und Performance

| ID | Anforderung | Prio | Abnahmekriterium |
|---|---|---|---|
| ORB-DASH-001 | Home muss Hostname, Board, SoC, OS, Uptime, CPU, RAM, Storage und Netzwerk darstellen. | MUSS | Werte stammen vom realen Gerät und werden ohne Vollreload aktualisiert. |
| ORB-DASH-002 | Oarfish muss verfügbare Temperaturquellen anzeigen und sinnvoll benennen. | MUSS | Zero 2 W zeigt mindestens die SoC-Temperatur; weitere Boards werden nur nach Verifikation als unterstützt ausgewiesen. |
| ORB-DASH-003 | Oarfish muss CPU-Frequenz und Governor anzeigen, soweit vom Kernel bereitgestellt. | SOLL | Aktuelle und maximale Frequenz sind nachvollziehbar. |
| ORB-DASH-004 | Board-spezifische Throttling- oder Unterspannungswarnungen müssen angezeigt werden, wenn ein sicherer Adapter existiert. | SOLL | Pi-Warnstatus oder äquivalentes Board-Signal wird korrekt interpretiert. |
| ORB-DASH-005 | Der Systemzustand muss kritische, relevante und informative Hinweise priorisieren. | MUSS | Warnungen nennen Ursache, Auswirkung und nächsten Schritt. |
| ORB-DASH-006 | Metrikabfragen müssen adaptive Intervalle und keine hochfrequente Dauersammlung verwenden. | MUSS | Idle-CPU und Schreiblast bleiben innerhalb NFR-Budgets. |
| ORB-DASH-007 | Oarfish muss laufende Plugins und Anwendungen zusammenfassen. | SOLL | Home verlinkt direkt zu Plugin- oder Containerdetails. |
| ORB-PWR-001 | Reboot und Shutdown müssen als bestätigungspflichtige GUI-Aktionen verfügbar sein. | MUSS | Aktion wird autorisiert und auditiert. |
| ORB-PWR-002 | Ein Neustart muss wartende Aktionen und verbundene Benutzer sichtbar machen. | SOLL | Bestätigungsdialog nennt mögliche Auswirkungen. |

### 6.4 Systemdienste, Journal, Prozesse und Updates

| ID | Anforderung | Prio | Abnahmekriterium |
|---|---|---|---|
| ORB-SVC-001 | systemd-Dienste müssen filterbar gelistet werden. | MUSS | Name, Status, Startmodus und Kurzbeschreibung sind sichtbar. |
| ORB-SVC-002 | Start, Stop und Neustart müssen mit Auswirkungsbeschreibung und Bestätigung möglich sein. | MUSS | Aktion wird autorisiert, protokolliert und verifiziert. |
| ORB-SVC-003 | Kritische Systemdienste müssen besonders gekennzeichnet sein. | SOLL | Netzwerk, SSH, Oarfish und Containerengines zeigen zusätzliche Warnung. |
| ORB-LOG-001 | Das Journal muss nach Zeit, Priorität, Unit und Freitext filterbar sein. | MUSS | Filter sind kombinierbar und Ergebnisse werden begrenzt nachgeladen. |
| ORB-LOG-002 | Loganzeige muss Secrets und definierte sensible Muster maskieren. | MUSS | Testfälle decken Tokens, Keys und Passwörter ab. |
| ORB-PRC-001 | Oarfish muss Prozesse nach CPU und RAM anzeigen. | SOLL | PID, Benutzer, Prozessname, CPU und RAM werden dargestellt. |
| ORB-OSU-001 | APT-Updates müssen klassifiziert angezeigt werden. | SOLL | Sicherheits- und reguläre Updates sind getrennt. |
| ORB-OSU-002 | OS-Updates benötigen Plan, Bestätigung und Ergebnisprüfung. | KANN | Pakete, Downloadgröße und Neustartbedarf erscheinen vor Ausführung. |
| ORB-OSU-003 | Kernel-, Bootloader- und Firmware-Updates dürfen nicht als generische Paketaktion behandelt werden. | MUSS | Board- oder OS-spezifische Risiken werden erkannt; unsichere Aktualisierung bleibt read-only. |

### 6.5 Storage, Bootmedium und Schreiblast

| ID | Anforderung | Prio | Abnahmekriterium |
|---|---|---|---|
| ORB-STO-001 | Dateisysteme, Mounts, Kapazität und Belegung müssen dargestellt werden. | MUSS | Gerät, Mountpoint, Typ, Gesamtgröße und Nutzung sind sichtbar. |
| ORB-STO-002 | Oarfish muss das wahrscheinlich aktive Boot- und Rootmedium identifizieren. | MUSS | microSD, eMMC, USB, SATA oder NVMe werden soweit erkennbar korrekt zugeordnet. |
| ORB-STO-003 | Removable Media und nicht persistente Mounts müssen markiert werden. | SOLL | Nutzer erkennt, welche Datenträger entfernt werden könnten. |
| ORB-STO-004 | Die GUI muss Hinweise auf geringe freie Kapazität und ungewöhnliche Schreiblast geben können. | SOLL | Schwellwerte sind dokumentiert und führen zu verständlicher Warnung. |
| ORB-STO-005 | Oarfish darf ohne explizite Freigabe keine Partitionierung oder Dateisystemerstellung durchführen. | MUSS | MVP bietet diese Funktionen nicht oder nur read-only. |
| ORB-STO-006 | Storage-Health wird nur angezeigt, wenn der zugrunde liegende Bus und das Tool verlässliche Daten liefern. | MUSS | Keine erfundenen SMART-Werte für microSD oder nicht unterstützte Medien. |
| ORB-STO-007 | Eigene Logs und Konfigurationen müssen schreibarm und rotiert sein. | MUSS | Keine unbeschränkte lokale Historie oder Metrikdatenbank. |

### 6.6 Netzwerk und Konnektivität

| ID | Anforderung | Prio | Abnahmekriterium |
|---|---|---|---|
| ORB-NET-001 | Interfaces, Adressen, Linkstatus, Standardroute und DNS müssen sichtbar sein. | MUSS | Aktueller Zustand wird ohne Konfigurationsänderung angezeigt. |
| ORB-NET-002 | WLAN-Status und Signalstärke sollen angezeigt werden, soweit verfügbar. | SOLL | Zero 2 W zeigt SSID, Signal und Verbindungsstatus ohne Secrets. |
| ORB-NET-003 | Änderungen an primärer Konnektivität benötigen einen Rückfallmechanismus. | MUSS | Ohne sicheren Rollback werden Änderungen im MVP nicht angeboten. |
| ORB-NET-004 | Remote-Zugriff ist kein Core-Zwang. | MUSS | Oarfish funktioniert lokal ohne Tailscale, VPN oder Cloudservice. |
| ORB-NET-005 | Plugins dürfen Netzwerkfunktionen nur mit expliziter Capability-Freigabe verändern. | MUSS | Berechtigungsdialog nennt Dienste, Routen und Firewallauswirkungen. |

### 6.7 Plugin-Plattform

| ID | Anforderung | Prio | Abnahmekriterium |
|---|---|---|---|
| ORB-PLG-001 | Plugins müssen ein versioniertes Manifest mit ID, Version, API-Kompatibilität, Berechtigungen, Plattformen und Abhängigkeiten besitzen. | MUSS | Inkompatible Plugins werden nicht aktiviert. |
| ORB-PLG-002 | Plugins müssen benötigte Capabilities explizit deklarieren. | MUSS | Installation zeigt jede Capability und erfordert Bestätigung. |
| ORB-PLG-003 | Plugins können Navigation, Dashboard, Detailansichten und Workflows beitragen. | MUSS | Mindestens Tailscale und Container Plugin registrieren sichtbare Beiträge. |
| ORB-PLG-004 | Installieren, Aktivieren, Deaktivieren, Aktualisieren und Entfernen müssen GUI-gesteuert sein. | MUSS | Lifecycle benötigt keine Dateibearbeitung. |
| ORB-PLG-005 | Drittcode darf nicht ungeprüft im privilegierten Core laufen. | MUSS | MVP nutzt First-Party-Plugins; externe Plugins bleiben deaktiviert. |
| ORB-PLG-006 | Pluginzustand darf den Core-Start nicht blockieren. | MUSS | Fehlerhaftes Plugin wird isoliert deaktiviert und sichtbar gemeldet. |
| ORB-PLG-007 | Plugins müssen Board-, OS- und Architekturkompatibilität deklarieren. | MUSS | Tailscale- oder Containerplugin wird nur auf unterstützten Plattformen angeboten. |
| ORB-PLG-008 | Pluginupdates müssen atomar und rollbackfähig sein. | SOLL | Fehlerhaftes Update reaktiviert vorherige Version. |
| ORB-PLG-009 | Ein öffentliches Marketplace-Versprechen ist nicht Bestandteil des MVP. | MUSS | UI zeigt ausschließlich real verfügbare First-Party-Plugins. |
| ORB-PLG-010 | Plugin-SDK, Manifest-Schema und Beispielplugin müssen dokumentiert sein. | SOLL | Beispiel lässt sich automatisiert bauen und testen. |

### 6.8 First-Party-Plugin Tailscale

| ID | Anforderung | Prio | Abnahmekriterium |
|---|---|---|---|
| ORB-TS-001 | Tailscale muss als optionales Plugin implementiert werden. | MUSS | Core startet und funktioniert ohne Plugin oder `tailscaled`. |
| ORB-TS-002 | Das Plugin muss Installation, vorhandene Installation und Version unterscheiden. | MUSS | GUI zeigt „nicht installiert“, „installiert“ oder „Update verfügbar“. |
| ORB-TS-003 | Eine Installation darf nur nach expliziter Freigabe und aus einer dokumentierten Paketquelle erfolgen. | MUSS | Paketquelle, Pakete und Dienständerungen sind vorab sichtbar. |
| ORB-TS-004 | Die Anmeldung muss über einen sicheren browser- oder device-basierten Flow erfolgen. | MUSS | Kein dauerhaft gespeicherter Klartext-Auth-Key ist erforderlich. |
| ORB-TS-005 | Plugin muss Node-Status, Tailnet-Name, Tailscale-Adressen und Verbindungszustand anzeigen. | MUSS | Daten stammen vom lokalen Tailscale-Dienst. |
| ORB-TS-006 | Aktivieren, Deaktivieren und Abmelden müssen GUI-gesteuert sein. | MUSS | Ergebnis wird verifiziert und auditiert. |
| ORB-TS-007 | Subnet-Router, Exit Node und Tailscale SSH sind nicht automatisch aktiviert. | MUSS | Erweiterte Funktionen benötigen separate Freigabe und sind im MVP KANN. |
| ORB-TS-008 | Deinstallation muss klar zwischen Plugin und Tailscale-Systempaket unterscheiden. | MUSS | Nutzer kann Plugin entfernen, ohne Tailscale ungefragt zu deinstallieren. |
| ORB-TS-009 | Tailscale darf nicht die einzige Oarfish-Authentifizierung ersetzen. | MUSS | Lokales Oarfish-Konto bleibt erforderlich. |
| ORB-TS-010 | Installation, Self-Update und Recovery dürfen nicht vom installierten Oarfish-Tailscale-Plugin abhängen. | MUSS | Bootstrap, Update und Pluginreparatur bleiben ohne Plugin möglich. |

### 6.9 First-Party-Plugin Containers

| ID | Anforderung | Prio | Abnahmekriterium |
|---|---|---|---|
| ORB-CNT-001 | Das Containerplugin muss Docker und Podman unabhängig erkennen. | MUSS | Es funktioniert mit einer, beiden oder keiner Engine. |
| ORB-CNT-002 | Beide Engines werden in einer gemeinsamen Anwendungsansicht dargestellt, ohne Semantik zu verfälschen. | MUSS | Engine-Herkunft und Einschränkungen bleiben sichtbar. |
| ORB-CNT-003 | Containerdetails müssen Image, Ports, Volumes, Netzwerke, Health, Ressourcen und Restart-Informationen anzeigen. | MUSS | Verfügbare Engine-Daten sind strukturiert sichtbar. |
| ORB-CNT-004 | Logs und Live-Ressourcenwerte müssen in der GUI verfügbar sein. | MUSS | Begrenzte Logs und ressourcenschonende Live-Daten funktionieren. |
| ORB-CNT-005 | Start, Stop und Neustart müssen bestätigt, autorisiert, protokolliert und verifiziert werden. | MUSS | Status wird nach Aktion erneut gelesen. |
| ORB-CNT-006 | Oarfish darf Socket-Berechtigungen nicht automatisch lockern. | MUSS | Kein automatisches chmod oder pauschales Gruppenmitglied. |
| ORB-CNT-007 | Ein GUI-Assistent muss mindestens eine getestete Anwendungsvorlage bereitstellen. | MUSS | Vorlage kann ohne manuelle Konfigurationsdatei deployed werden. |
| ORB-CNT-008 | Der Assistent muss Ressourcenlimits für kleine SBCs berücksichtigen. | MUSS | Warnung bei offensichtlich ungeeigneter RAM- oder Architekturanforderung. |
| ORB-CNT-009 | Image-Architektur muss vor Deployment gegen Hostarchitektur geprüft werden. | MUSS | Nicht passendes Image wird blockiert oder klar als emuliert markiert. |
| ORB-CNT-010 | Docker Compose und Podman Quadlet bleiben getrennte zukünftige Backends. | SOLL | Keine erzwungene 1:1-Konvertierung. |
| ORB-CNT-011 | Das Plugin darf keine Engine ungefragt installieren. | MUSS | Installation ist eigener, explizit bestätigter Workflow. |

### 6.10 First-Party-Plugin Oarfish Assist

| ID | Anforderung | Prio | Abnahmekriterium |
|---|---|---|---|
| ORB-AI-001 | Oarfish Assist muss vollständig deaktivierbar sein. | MUSS | Deaktivierung beendet Provider-Aufrufe und Hintergrundkommunikation. |
| ORB-AI-002 | Providerabstraktion muss Cloud-Modelle und lokale OpenAI-kompatible Endpunkte unterstützen. | MUSS | Mindestens ein Remote- und ein lokaler Endpoint sind konfigurierbar. |
| ORB-AI-003 | Provider, Endpoint, Modell und Zugangsdaten müssen über die GUI konfigurierbar sein. | MUSS | Verbindungstest benötigt keine manuelle Dateiänderung. |
| ORB-AI-004 | Zugangsdaten müssen serverseitig geschützt und in UI sowie Logs maskiert werden. | MUSS | Schlüssel sind nach Speicherung nicht im Klartext abrufbar. |
| ORB-AI-005 | Das Modell darf nur typisierte Oarfish-Tools aufrufen. | MUSS | Es existiert kein generisches Shell- oder Root-Tool. |
| ORB-AI-006 | Lesezugriff, Diagnose, Vorschlag, Freigabe, Ausführung und Verifikation müssen getrennt sein. | MUSS | Write-Tools sind ohne Freigabe technisch gesperrt. |
| ORB-AI-007 | Hostdaten dürfen nur nach transparenter Zustimmung an externe Provider gesendet werden. | MUSS | GUI zeigt Datenkategorien und Zielprovider. |
| ORB-AI-008 | Secrets müssen vor Modellaufrufen entfernt werden. | MUSS | Automatisierte Redaction-Tests bestehen. |
| ORB-AI-009 | Logs und externe Texte gelten als untrusted content. | MUSS | Prompt-Injection kann keine Tool-Policy erweitern. |
| ORB-AI-010 | Antworten müssen verwendete Hostquellen nennen und Fakten von Vermutungen trennen. | SOLL | Antwort verweist auf Messwerte, Units oder Logs. |
| ORB-AI-011 | Lokale Inferenz darf nicht auf dem Zero 2 W vorausgesetzt werden. | MUSS | Lokales Modell läuft auf separatem Rechner oder stärkerem Gerät. |
| ORB-AI-012 | MVP-Funktionalität ist read-only. | MUSS | AI kann keine reale Zustandsänderung ausführen. |

### 6.11 Authentifizierung, Sicherheit und Audit

| ID | Anforderung | Prio | Abnahmekriterium |
|---|---|---|---|
| ORB-AUTH-001 | Das MVP muss ein lokales Administratorkonto unterstützen. | MUSS | Anmeldung, Abmeldung und Passwortänderung funktionieren. |
| ORB-AUTH-002 | Sitzungen müssen serverseitig invalidierbar und durch sichere Cookies geschützt sein. | MUSS | HttpOnly, SameSite und angemessene Lebensdauer sind umgesetzt. |
| ORB-AUTH-003 | Anmeldeversuche müssen rate-limitiert werden. | MUSS | Wiederholte Fehlversuche führen zu zeitlicher Begrenzung. |
| ORB-SEC-001 | Core und Webprozess müssen unprivilegiert laufen. | MUSS | Dedizierter Benutzer ohne Login-Shell und ohne pauschale sudo-Rechte. |
| ORB-SEC-002 | Privilegierte Operationen müssen über einen eng begrenzten Broker erfolgen. | MUSS | Nur definierte Operationen mit typisierten Argumenten sind verfügbar. |
| ORB-SEC-003 | Der Broker muss Board- und Plugin-Capabilities prüfen. | MUSS | Nicht freigegebene Operationen werden technisch abgelehnt. |
| ORB-SEC-004 | Zustandsändernde Webanfragen müssen gegen CSRF und unerlaubte Herkunft geschützt sein. | MUSS | Sicherheitstests bestehen. |
| ORB-SEC-005 | Konfigurations- und Secret-Dateien müssen restriktive Rechte verwenden. | MUSS | Dateien sind maximal 0600 oder begründet enger freigegeben. |
| ORB-SEC-006 | Administrative Aktionen müssen auditiert werden. | MUSS | Zeit, Benutzer, Plugin, Aktion, Ziel, Ergebnis und Korrelations-ID sind vorhanden. |
| ORB-SEC-007 | Audit und Logs dürfen keine Secrets enthalten. | MUSS | Redaction-Tests bestehen. |
| ORB-SEC-008 | Drittanbieter-GitHub-Actions müssen auf unveränderliche Commit-SHAs gepinnt werden. | MUSS | Workflow-Review findet keine unversionierten produktiven Actions. |
| ORB-SEC-009 | Abhängigkeiten und Artefakte müssen automatisiert geprüft werden. | SOLL | Go-, npm-, SBOM- und Vulnerability-Checks laufen in CI. |
| ORB-SEC-010 | Oarfish darf nicht automatisch einen öffentlichen Managementport öffnen. | MUSS | Öffentliche Bindung erfordert explizite Konfiguration und Warnung. |

### 6.12 GitHub Actions, Releases, Installation und Self-Update

| ID | Anforderung | Prio | Abnahmekriterium |
|---|---|---|---|
| ORB-CICD-001 | Pull Requests müssen Linting, Unit-Tests, Frontend-Checks und ARM64-Build ausführen. | MUSS | PR-Pipeline ist reproduzierbar grün. |
| ORB-CICD-002 | Builds müssen auf GitHub-hosted Runnern oder lokal auf dem MacBook erfolgen. | MUSS | Auf dem SBC sind keine Buildtools oder Runner installiert. |
| ORB-CICD-003 | Frontend muss gebaut und in ein Go-Binary eingebettet werden. | MUSS | Zielgerät benötigt kein Node.js. |
| ORB-CICD-004 | CI muss mindestens `linux/arm64` erzeugen. | MUSS | Artefakt startet in ARM64-Smoke-Test oder Fixture. |
| ORB-CICD-005 | GitHub Actions darf nicht direkt auf den Pi deployen. | MUSS | Kein SSH-Key, Pi-Host oder Tailscale-Deploypfad im Workflow. |
| ORB-REL-001 | Ein versionierter Tag muss einen Release-Workflow auslösen können. | MUSS | Release enthält Binary, Manifest, Checksums und Signatur. |
| ORB-REL-002 | Release-Artefakte müssen Commit, Version, Architektur, Mindestanforderungen und SHA-256 ausweisen. | MUSS | Manifest ist maschinenlesbar und geprüft. |
| ORB-REL-003 | Release-Artefakte müssen kryptografisch signiert werden. | MUSS | Installer und Updater lehnen ungültige Signaturen ab. |
| ORB-INS-001 | Erstinstallation muss über ein kleines, lesbares Installationsskript möglich sein. | MUSS | Frisches Raspberry Pi OS Lite 64-bit wird erfolgreich eingerichtet. |
| ORB-INS-002 | Installer muss OS, Architektur, systemd, freien Speicher und benötigte Basistools prüfen. | MUSS | Ungeeignete Systeme werden ohne Teilerfolg abgelehnt. |
| ORB-INS-003 | Installer muss einen dedizierten Systembenutzer, Verzeichnisse und systemd-Unit anlegen. | MUSS | Dienst läuft unprivilegiert und startet nach Reboot. |
| ORB-INS-004 | Installer muss Release und Signatur vor Aktivierung prüfen. | MUSS | Manipuliertes Paket wird nicht installiert. |
| ORB-UPD-001 | Oarfish muss neue Releases über GitHub Releases erkennen können. | MUSS | GUI zeigt installierte und verfügbare Version. |
| ORB-UPD-002 | Update muss durch den Administrator in der GUI bestätigt werden. | MUSS | Kein automatisches Upgrade ohne Policy und Freigabe im MVP. |
| ORB-UPD-003 | Update muss in ein Staging- und danach versioniertes Release-Verzeichnis installieren. | MUSS | Aktive Version bleibt bis zur finalen Aktivierung unverändert. |
| ORB-UPD-004 | Aktivierung muss atomar über den `current`-Symlink erfolgen. | MUSS | Kein halb aktualisierter Zustand. |
| ORB-UPD-005 | Nach Aktivierung müssen Dienst und Health-Endpunkte geprüft werden. | MUSS | Update gilt nur bei erfolgreichem Healthcheck als erfolgreich. |
| ORB-UPD-006 | Bei Fehler muss automatisch zur vorherigen gesunden Version zurückgerollt werden. | MUSS | Rollback-Test besteht. |
| ORB-UPD-007 | Der privilegierte Update Helper darf nur Oarfish-Releases verwalten. | MUSS | Kein generischer Shell- oder Paketmanagerzugriff. |
| ORB-UPD-008 | Tailscale darf für Installation, Release-Erkennung oder Update nicht erforderlich sein. | MUSS | Vollständiger Test ohne Tailscale besteht. |

## 7. UX- und Interaktionsanforderungen

### 7.1 Gestaltungsprinzipien

- **Gerät zuerst:** Board, Zustand und Fähigkeiten stehen vor abstrakten Linux-Komponenten.
- **Aufgabe vor Technik:** „Remote-Zugriff einrichten“ statt „tailscaled konfigurieren“.
- **Progressive Disclosure:** Standardpfade zeigen wenige sichere Entscheidungen.
- **Sichere Defaults:** Oarfish wählt konservative Werte und erklärt Abweichungen.
- **Klarer Supportstatus:** Verified, Compatible, Community, Experimental oder Generic ist sichtbar.
- **Keine Fake-Funktion:** Nicht implementierte oder nicht getestete Funktionen werden nicht als aktiv dargestellt.
- **Direktes Feedback:** Lade-, Erfolgs-, Fehler- und Recovery-Zustände sind sichtbar.
- **Einheitliche Bestätigung:** Ziel, Auswirkung, Berechtigung und Rückfallweg erscheinen vor kritischen Aktionen.

### 7.2 Informationsarchitektur

| Bereich | Inhalt |
|---|---|
| Home | Board, Zustand, Temperatur, Ressourcen, Storage, Netzwerk, Alerts und Empfehlungen. |
| Gerät | Modell, SoC, OS, Kernel, Supportstatus, Sensoren, Bootmedium und Capabilities. |
| System | Dienste, Journal, Prozesse, Netzwerk, Storage, Updates, Reboot und Shutdown. |
| Anwendungen | Beiträge des Containerplugins, Templates, Logs und Lifecycle. |
| Konnektivität | Beiträge von Tailscale und später WireGuard oder Access Point. |
| Erweiterungen | Plugins, Berechtigungen, Versionen, Updates und Kompatibilität. |
| Oarfish Assist | Kontextuelle Diagnose und Erklärungen. |
| Einstellungen | Konto, Binding, Sprache, Updatekanal und wenige produktrelevante Optionen. |

### 7.3 Plugin-UX

Ein Plugin muss sich wie ein nativer Produktbestandteil anfühlen. Es darf dennoch nie verschleiern, dass zusätzliche Systempakete, Dienste oder Netzwerkänderungen erforderlich sind.

Der Installationsdialog zeigt mindestens:

1. Zweck und Hersteller.
2. Unterstützte Boards, OS und Architekturen.
3. Benötigte Oarfish-Capabilities.
4. Zu installierende Systempakete oder Dienste.
5. Netzwerk- und Sicherheitsauswirkungen.
6. Deinstallations- und Recovery-Verhalten.

### 7.4 GUI-Vollständigkeitsregel

Eine Funktion gilt erst dann als fertig, wenn:

- der gesamte Happy Path über die GUI möglich ist,
- Validierungsfehler verständlich dargestellt werden,
- ein Fehler- und Recovery-Pfad existiert,
- der technische Ist-Zustand nach der Aktion erneut gelesen wird,
- die Dokumentation keinen Shell-Schritt als normalen Produktpfad verlangt.

## 8. Technische Zielarchitektur

### 8.1 Komponenten

| Komponente | Aufgabe |
|---|---|
| Oarfish Web UI | Svelte 5, TypeScript, responsive GUI und Plugin-Slots. |
| Oarfish Core | Go-HTTP-Server, Auth, API, Eventstream, Capability Registry und Plugin Registry. |
| Hardware Detector | Board, SoC, Architektur, Device Tree und Sensoren erkennen. |
| OS Detector und Adapter | `/etc/os-release`, Raspberry Pi OS, Ubuntu Server und generische Debian-Familie. |
| System Adapters | systemd, journald, procfs, sysfs, APT, Netzwerk und Storage. |
| Board Profile Registry | SoC- und Boardprofile, Supportstatus und Capability Mapping. |
| Privilege Broker | Eng begrenzte, typisierte privilegierte Operationen. |
| Update Manager | Release-Erkennung, Download, Verifikation, Staging und Status. |
| Update Helper | Root-eigene, eng begrenzte Aktivierung und Rollback. |
| Plugin Host | Lifecycle, Manifestprüfung, UI-Beiträge und Capability-Bindung. |
| First-Party-Plugins | Tailscale, Containers und Oarfish Assist. |
| Audit | Strukturierte Ereignisse über journald oder append-only Datei. |

### 8.2 Runtime- und Dateilayout

| Pfad | Zweck |
|---|---|
| `/opt/oarfish/releases/<version>` | Versionierte, unveränderliche Releases. |
| `/opt/oarfish/current` | Atomarer Symlink auf aktives Release. |
| `/etc/oarfish/config.json` | Validierte Hauptkonfiguration. |
| `/etc/oarfish/secrets/` | Restriktiv geschützte Secrets. |
| `/var/lib/oarfish/` | Pluginzustand, Profile, Updatezustand und kleine persistente Daten. |
| `/var/cache/oarfish/` | Wiederherstellbare Caches und Release-Downloads. |
| `/run/oarfish/` | Sockets, Locks und ephemere Zustände. |
| `/usr/libexec/oarfish-update-helper` | Eng begrenzter privilegierter Update Helper. |
| journald | Strukturierte Laufzeit- und Auditlogs mit Rotation. |

### 8.3 Hardware- und OS-Abstraktion

Die Core-Interfaces müssen Hardware und Betriebssystem getrennt modellieren. Ein Adapter darf nur seinen Verantwortungsbereich kennen. Beispielhafte Zielstruktur:

```text
internal/platform/
  detector/
  hardware/
    generic_arm64.go
    raspberry_pi.go
  operating_system/
    debian_family.go
    raspberry_pi_os.go
    ubuntu.go
  packages/
    apt.go
  network/
    network_manager.go
    networkd.go
    netplan.go
  services/
    systemd.go
```

Diese Struktur ist eine Architekturgrenze, keine Verpflichtung, alle Adapter bereits in Gate G1 zu implementieren.

### 8.4 Board Profile Registry

Profile werden als versionierte, schema-validierte Daten ausgeliefert. Ein Profil kann im Core-Bundle oder in einem signierten Profilpaket liegen. Das Schema enthält keine freien Kommandos.

Minimalfelder:

- `profile_id`, `version`, `vendor`, `model`, `soc_family`,
- `compatible_patterns`, `os_constraints`, `kernel_constraints`,
- `support_tier`, `maintainers`, `tested_combinations`,
- `capabilities`, `sensor_bindings`, `storage_hints`,
- `known_limitations`, `documentation_links`.

### 8.5 Pluginmodell

Im MVP werden Plugins als First-Party-Bundles entwickelt. Sie besitzen Manifest, Frontendbeiträge und Backendmodule. Privilegierte Aktionen laufen nicht direkt im Plugin, sondern über Core und Broker.

Das spätere Zielmodell für externe Plugins umfasst signierte Pakete, explizite API-Kompatibilität, Prozess- oder WASM-Isolation, begrenzte Rechte und atomare Installation.

### 8.6 Privilege Broker und Update Helper

Der Broker stellt keine allgemeine Shell bereit. Er implementiert eine feste Allowlist, zum Beispiel:

- `service.restart(unit)`
- `system.reboot()`
- `tailscale.login(flow_id)`
- `container.restart(engine, container_id)`

Der Update Helper ist separat und darf ausschließlich:

- ein bereits heruntergeladenes, signiertes Oarfish-Bundle prüfen,
- Release-Verzeichnisse unter `/opt/oarfish/releases` verwalten,
- den `current`-Symlink atomar umschalten,
- `oarfish.service` neu starten,
- einen Rollback auf eine bekannte gesunde Version ausführen.

### 8.7 API- und Ereignismodell

- Versionierte REST-Endpunkte unter `/api/v1`.
- Server-Sent Events für Metriken, Status und Workflowfortschritt.
- Korrelations-ID für jede zustandsändernde Aktion.
- Typisierte Fehler mit Nutzertext, technischem Code und Recovery-Hinweis.
- OpenAPI-Spezifikation aus Code oder gemeinsamem Schema.
- Plugin-API wird separat versioniert.

### 8.8 Konfiguration und Persistenz

Das MVP verwendet keine externe Datenbank. Konfigurationen werden typisiert, validiert und atomar geschrieben. Sessions können im Arbeitsspeicher liegen und werden bei Neustart invalidiert. Auditdaten werden rotiert gespeichert.

### 8.9 Technologiestack

- Go, bevorzugt Standardbibliothek und kleine, geprüfte Abhängigkeiten.
- Svelte 5, TypeScript und Vite nur für Entwicklung und Build.
- `go:embed` für statische Frontendassets.
- `CGO_ENABLED=0` für portable ARM64-Cross-Builds, solange keine zwingende Abhängigkeit dagegen spricht.
- Keine Node.js-, Python- oder Datenbank-Runtime auf dem Zielgerät.
- Kein mandatory Container für Oarfish selbst.

## 9. GitHub Actions, Releases, Installation und Self-Update

### 9.1 Grundprinzip

Der Delivery Flow ist pull-basiert:

```text
MacBook
  Claude Code und lokale Entwicklung
        |
        | git push
        v
GitHub Repository
        |
        | GitHub Actions
        v
Tests, Frontend-Build, ARM64-Build und Release
        |
        | HTTPS Pull
        v
Raspberry Pi
  install.sh für Erstinstallation
  Oarfish GUI für spätere Updates
```

GitHub Actions kennt keine IP-Adresse, keinen SSH-Key und keine Anmeldedaten des Pi. Es gibt keinen Self-hosted Runner auf MacBook oder SBC.

### 9.2 Workflow-Struktur

| Workflow | Trigger | Zweck |
|---|---|---|
| `ci.yml` | Pull Request, Push auf `main` | Go- und Frontend-Checks, Tests, Linting und Linux-ARM64-Cross-Build. |
| `release.yml` | Versionstag oder manueller Release-Start | Reproduzierbarer Build, Manifest, Checksums, Signatur und GitHub Release. |
| `profiles.yml` | Änderung an Boardprofilen | Schema-, Fixture- und Match-Tests. |

Ein Workflow für SSH-Deployment auf reale Geräte ist ausdrücklich nicht vorgesehen.

### 9.3 Release-Artefakte

Das erste Releaseformat ist ein signiertes tar.gz-Bundle für Linux ARM64. Ein Release enthält mindestens:

```text
oarfish_<version>_linux_arm64.tar.gz
manifest.json
checksums.txt
checksums.txt.sig
install.sh
```

Später können ARMv7, AMD64 und native Pakete hinzukommen. Das tar-basierte Bundle bleibt der kleinste gemeinsame Nenner für Installer und Updater.

### 9.4 Erstinstallation

Das Installationsskript:

1. erkennt Kernel, Architektur und `/etc/os-release`,
2. prüft systemd, freien Speicher und benötigte Basistools,
3. ermittelt das passende Release-Artefakt,
4. lädt Bundle, Manifest, Checksums und Signatur,
5. prüft Hash und Signatur,
6. legt Benutzer, Verzeichnisse und systemd-Unit an,
7. installiert nach `/opt/oarfish/releases/<version>`,
8. setzt `/opt/oarfish/current` atomar,
9. startet Oarfish und prüft `/healthz`,
10. zeigt Version und lokale URL an.

Das Skript muss klein, lesbar, idempotent und ohne versteckte Telemetrie sein.

### 9.5 GUI-basierter Self-Update-Prozess

Unter `System > Oarfish Update` zeigt die GUI:

- installierte Version,
- verfügbare Version,
- Updatekanal,
- Release Notes,
- Downloadgröße,
- Signaturstatus,
- erforderliche Migrationen,
- Rollbackfähigkeit.

Nach Bestätigung:

1. Release-Metadaten abrufen,
2. Architektur und Mindestversion prüfen,
3. Bundle in Staging herunterladen,
4. Hash und Signatur prüfen,
5. Konfiguration und freien Speicher prüfen,
6. neues Release-Verzeichnis anlegen,
7. vorherige gesunde Version als Rollbackziel markieren,
8. `current` atomar umschalten,
9. Dienst neu starten,
10. Healthcheck und kurze Sanity-Prüfung ausführen,
11. bei Fehler automatisch zurückrollen,
12. Ergebnis auditiert anzeigen.

### 9.6 Releasekanäle

- `stable` ist der Defaultkanal.
- `beta` kann später explizit aktiviert werden.
- Ein Downgrade ist nur über einen klaren Recovery-Workflow erlaubt.
- Plugins und Boardprofile besitzen eigene Versionen, müssen aber mit dem Core kompatibel sein.

### 9.7 Repository-Sichtbarkeit und Downloadzugriff

Gate G1 kann in einem privaten Repository erfolgen. Spätestens vor dem produktnahen Installer muss ein anonym lesbarer Releasekanal festgelegt werden. Der bevorzugte Zielzustand ist ein öffentliches GitHub-Repository mit öffentlichen Releases.

Ein privater Releasekanal mit dauerhaftem GitHub-Token auf dem Pi ist nicht die Defaultarchitektur. Falls er für frühe Entwicklung erforderlich ist, muss er als zeitlich begrenzter Entwicklungsmodus dokumentiert und später entfernt werden.

### 9.8 Signierung und Supply Chain

- Release-Builds laufen aus versionierten Tags.
- Actions verwenden minimale `permissions`.
- Drittanbieter-Actions werden auf unveränderliche Commit-SHAs gepinnt.
- Das Release-Bundle wird mit einem dedizierten Ed25519-basierten Signierschlüssel signiert.
- Der öffentliche Prüfschlüssel ist in Installer und Updater eingebettet.
- Der private Schlüssel wird nicht im Repository gespeichert.
- Installer und Updater vertrauen nicht allein auf HTTPS oder einen SHA-256-Wert aus derselben Downloadquelle.

## 10. Security, Datenschutz und AI Governance

### 10.1 Sicherheitsgrenzen

1. Browser ist nicht vertrauenswürdig.
2. Plugin ist nicht automatisch privilegiert.
3. LLM-Ausgabe ist nicht vertrauenswürdig.
4. Logs, Containerinhalte und externe Texte sind nicht vertrauenswürdig.
5. Boardprofil ist Datenquelle, kein ausführbarer Code.
6. Privilegierte Änderung erfolgt ausschließlich über den Broker.
7. Update Helper und normaler Oarfish-Webprozess sind strikt voneinander getrennt.

### 10.2 Plugin-Berechtigungen

Beispielhafte Capabilities:

- `system.read`
- `service.read`
- `service.control:tailscaled`
- `package.install:tailscale`
- `network.read`
- `network.routes.write`
- `container.read`
- `container.control`
- `ai.context.read`

Berechtigungen müssen eng, verständlich und widerrufbar sein. Eine Capability darf nicht durch einen allgemeineren Shellzugriff ersetzt werden.

### 10.3 AI-Datenklassen

| Klasse | Beispiel | Standard |
|---|---|---|
| Öffentlich | Boardmodell, OS-Version, Oarfish-Version | nach Konfiguration teilbar |
| Betriebsdaten | CPU, Temperatur, Dienststatus, Logauszüge | explizite Zustimmung bei Cloudprovider |
| Sensibel | Hostnamen, IPs, Container- und Prozessnamen | standardmäßig minimieren |
| Secret | Tokens, Passwörter, private Schlüssel, Umgebungssecrets | niemals an Modell senden |

### 10.4 Supply-Chain-Anforderungen

- Lockfiles und reproduzierbare Builds.
- SBOM pro Release.
- Checksums und verpflichtende kryptografische Signaturen.
- Dependency- und Vulnerability-Scanning.
- Gepinnte Actions.
- Reviewpflicht für Änderungen an Broker, Profilen und Pluginmanifesten.
- Keine Installation aus unkontrollierten Curl-Pipes im Produktpfad.

### 10.5 Datenschutz

- Keine Telemetrie ohne Opt-in.
- Kein Cloudkonto für lokalen Betrieb erforderlich.
- Tailscale- und AI-Plugins sind optional.
- Diagnoseexporte entfernen Secrets und ermöglichen Vorschau vor Download.
- Auditdaten besitzen definierte Retention und Rotation.

## 11. Nichtfunktionale Anforderungen

| ID | Anforderung | Prio | Abnahmekriterium |
|---|---|---|---|
| ORB-NFR-001 | Oarfish Core Idle-RSS auf Zero 2 W soll 35 MB nicht überschreiten; 45 MB ist tolerierter SOLL-Grenzwert. | SOLL | Messung nach fünf Minuten Idle dokumentiert. |
| ORB-NFR-002 | Gesamter Oarfish-Prozesssatz soll im normalen Betrieb unter 80 MB Peak-RSS bleiben. | SOLL | Dashboard-, Log- und Plugin-Test bleibt im Budget. |
| ORB-NFR-003 | Idle-CPU soll unter 1 Prozent eines Kerns im Fünf-Minuten-Mittel liegen. | SOLL | Reproduzierbare Messung. |
| ORB-NFR-004 | Kaltstart bis `readyz` soll auf Zero 2 W unter zwei Sekunden liegen. | SOLL | Hardware-Abnahmeprotokoll dokumentiert den Wert. |
| ORB-NFR-005 | ARM64-Release inklusive Frontend soll unter 35 MB bleiben. | SOLL | CI dokumentiert Größe. |
| ORB-NFR-006 | Komprimiertes initiales Frontendbundle soll unter 750 KB bleiben. | SOLL | Bundlebudget in CI. |
| ORB-NFR-007 | Oarfish muss bei 512 MB RAM ohne dauerhafte Swap-Aktivität nutzbar bleiben. | MUSS | Abnahmetest auf Zero 2 W. |
| ORB-NFR-008 | Zielgerät benötigt keine Node.js-, Python- oder DB-Runtime. | MUSS | Frische Installation bestätigt. |
| ORB-NFR-009 | Schreiblast auf microSD muss minimiert werden. | MUSS | Keine hochfrequente Persistenz; Logs rotiert; Messung dokumentiert. |
| ORB-NFR-010 | Core-Funktionen müssen nach Ersteinrichtung ohne Internet funktionieren. | MUSS | Offline-Test besteht. |
| ORB-NFR-011 | UI-Kernpfade müssen tastaturbedienbar und auf WCAG 2.2 AA ausgelegt sein. | SOLL | Automatisierter und manueller Test. |
| ORB-NFR-012 | UI muss auf aktuellem Chromium, Firefox und Safari funktionieren. | SOLL | Smoke-Tests bestehen. |
| ORB-NFR-013 | Konfigurationsänderungen müssen validiert und atomar gespeichert werden. | MUSS | Unterbrechung hinterlässt keine unlesbare Konfiguration. |
| ORB-NFR-014 | Oarfish muss mit zeitweise instabiler WLAN-Verbindung umgehen. | MUSS | UI reconnectet; keine beschädigten Aktionen. |
| ORB-NFR-015 | Generic Mode muss auf unbekanntem ARM64-System sicher funktionieren. | MUSS | Keine board-spezifische Aktion, Core-Dashboard verfügbar. |
| ORB-NFR-016 | Profile und Plugins müssen getrennt vom Core aktualisierbar sein. | SOLL | Versionierte Bundles und Kompatibilitätsprüfung. |
| ORB-NFR-017 | Tailscale- und andere Fremddaemons zählen nicht zum Oarfish-Core-RSS, müssen aber in Ressourcenansicht sichtbar sein. | MUSS | UI trennt Oarfish und externe Dienste. |

## 12. Test- und Abnahmekonzept

### 12.1 Testpyramide

| Ebene | Inhalt |
|---|---|
| Unit | Parser, OS-Erkennung, Hardwareerkennung, Profile, Policies, Manifest und Adapter. |
| Contract | REST, SSE, Plugin API, Broker, Update Manager und Providerinterfaces. |
| Integration | systemd, journald, APT, Netzwerkbackends, Docker, Podman und Tailscale in kontrollierten Umgebungen. |
| UI | Kernworkflows, Accessibility, Fehlerzustände, responsive Layouts und Update-Dialoge. |
| Hardware | Raspberry Pi Zero 2 W mit Raspberry Pi OS Lite 64-bit. |
| Release | GitHub Release, Installer, atomare Aktivierung, Healthcheck und Rollback. |
| Compatibility | Ubuntu Server LTS ARM64 nach der MVP-Basis. |

### 12.2 Hardware- und OS-Testmatrix

| Gerät | OS | Pflicht vor MVP |
|---|---|---|
| Raspberry Pi Zero 2 W | Raspberry Pi OS Lite 64-bit | ja |
| Raspberry Pi oder ARM64-Testsystem | Ubuntu Server LTS ARM64 | nein für MVP, verbindliches Folgeziel |
| Generic ARM64 Testumgebung | Debian- oder Ubuntu-Familie | ja für Generic Mode und Fixtures |
| Rockchip-Board | Armbian oder Ubuntu | nein, Post-MVP |
| ARMv7-Gerät | Debian-basiert | nein |

### 12.3 Abnahmetests

| ID | Test | Erwartetes Ergebnis |
|---|---|---|
| AT-001 | CI-Grundprüfung | Lint, Tests, Frontend und ARM64-Build sind grün. |
| AT-002 | OS-Erkennung Pi OS | Raspberry Pi OS wird aus `/etc/os-release` korrekt erkannt. |
| AT-003 | Hardwareerkennung Pi | Zero 2 W, Architektur, Kernel und Sensoren werden korrekt erkannt. |
| AT-004 | Hardware-OS-Trennung | Pi-Hardware und OS-Adapter können unabhängig getestet werden. |
| AT-005 | Generic Mode | Unbekanntes ARM64-System bleibt sicher nutzbar. |
| AT-006 | Installer | Frisches Pi OS wird aus signiertem Release erfolgreich installiert. |
| AT-007 | Reboot | Oarfish startet automatisch und bleibt erreichbar. |
| AT-008 | Reale Metriken | CPU, RAM, Temperatur, Storage und Uptime stimmen innerhalb Toleranz. |
| AT-009 | Serviceaktion | Bestätigung, Autorisierung, Aktion, Verifikation und Audit sind vollständig. |
| AT-010 | Update-Erkennung | GUI zeigt ein neueres kompatibles Release an. |
| AT-011 | Erfolgreiches Update | Neues Release wird atomar aktiviert und Healthcheck ist grün. |
| AT-012 | Rollback | Absichtlich fehlerhaftes Release aktiviert automatisch die vorherige Version. |
| AT-013 | Manipuliertes Release | Installer und Updater lehnen ungültige Signatur ab. |
| AT-014 | Tailscale nicht installiert | Plugin zeigt Setup-Zustand, Core funktioniert. |
| AT-015 | Tailscale Setup | Installation oder Anbindung, Login und Status funktionieren. |
| AT-016 | Keine Containerengine | Plugin zeigt Nicht-verfügbar-Zustand, Core bleibt nutzbar. |
| AT-017 | Containerengine | Liste, Details, Logs und Lifecycle funktionieren. |
| AT-018 | AI ohne Provider | Kein Netzwerkaufruf, nur Setup-Workflow. |
| AT-019 | AI read-only | Nur freigegebene Daten, keine Zustandsänderung und kein Shellzugriff. |
| AT-020 | Pluginfehler | Fehlerhaftes Plugin blockiert den Core nicht. |
| AT-021 | Ressourcenbudget | Messwerte werden dokumentiert und MUSS-Grenzen eingehalten. |
| AT-022 | microSD-Schreiblast | Keine unbeschränkten Logs oder hochfrequente Schreibschleifen. |
| AT-023 | Secrets | Keys erscheinen nicht in UI, Browserstorage, Journal oder Action-Logs. |
| AT-024 | GUI-Komplettheit | Kein normaler Produktpfad benötigt Shell, YAML oder Dateiedit. |
| AT-025 | Accessibility | Kernpfade funktionieren ohne Maus. |
| AT-026 | Offline-Betrieb | Core funktioniert nach Setup ohne Internet. |
| AT-027 | Ubuntu Architecture Readiness | Adaptergrenzen und Fixtures zeigen, dass Ubuntu ohne Core-Rewrite integrierbar ist. |

### 12.4 Nachweisführung

- Jede MUSS-Anforderung wird mindestens einem Test zugeordnet.
- GitHub Actions archiviert Testberichte, Buildgrößen und Release-Metadaten.
- Hardwaremessungen nennen Gerät, Revision, OS, Kernel, Commit und Messmethode.
- Nicht physisch verifizierte Kombinationen erhalten nie den Status Verified.
- Nicht implementierte Funktionen werden klar ausgeschlossen und nicht als funktionierende Beta dargestellt.

## 13. Betrieb, Recovery und Observability

### 13.1 Health und Diagnose

- `/healthz`: Prozess lebt und Kerninitialisierung ist abgeschlossen.
- `/readyz`: Konfiguration, Profile und erforderliche lokale Ressourcen sind bereit.
- `/api/v1/about`: Version, Commit, Builddatum, Architektur, Boardprofil und Plugins.
- Strukturierte Logs mit Level, Komponente, Plugin und Korrelations-ID.
- Diagnosepaket mit Vorschau und Secret-Redaction.

### 13.2 Backup und Wiederherstellung

Im MVP müssen folgende Daten dokumentiert sicherbar sein:

- Oarfish-Konfiguration,
- Secret-Store,
- Pluginmanifest und Pluginzustand,
- lokale Boardprofil-Erweiterungen,
- Auditkonfiguration.

Ein vollständiges Gerätebackup ist nicht Teil des MVP. Ein späteres Backup-Plugin darf System- und Anwendungsdaten getrennt behandeln.

### 13.3 Recovery-Modi

- Start ohne Plugins durch Recovery-Flag.
- Fallback auf vorheriges Oarfish-Release.
- Deaktivierung eines fehlerhaften Plugins ohne UI-Zugriff über eng dokumentierten Notfallpfad.
- Zurücksetzen der Netzwerkbindung auf Loopback.
- Keine automatische Änderung von Bootloader oder Partitionstabelle.

### 13.4 Updates

Oarfish Core, Profile und Plugins besitzen getrennte Versionen. Der Core-Updater ist kein Plugin. Er muss auch dann funktionieren, wenn alle Plugins deaktiviert sind.

Ein Core-Update zeigt Herkunft, Signaturstatus, Kompatibilität, Migrationshinweise und Rollbackfähigkeit. Der Updater speichert keine dauerhaften GitHub-Credentials für den normalen öffentlichen Releasepfad.

## 14. Schrittweiser Realisierungsplan mit Claude Code

### 14.1 Arbeitsmodus

Nach Freigabe erhält Claude Code keinen allumfassenden Implementierungsauftrag. Jede Session bearbeitet genau ein Gate oder ein klar abgegrenztes Teilpaket. Am Ende stehen getesteter Code, aktualisierte Dokumentation, ein sauberer Commit und ein messbares Exit-Kriterium.

Claude Code muss vor jeder Arbeit:

1. `CLAUDE.md`, dieses Pflichtenheft und den aktuellen Gate-Prompt lesen.
2. Bestehenden Code und CI-Status prüfen.
3. Nur den freigegebenen Scope implementieren.
4. Tests und Abnahmekriterien ausführen.
5. Keine nächste Stage ungefragt beginnen.

### 14.2 Stage-Gates

| Gate | Ziel | Lieferumfang | Exit-Kriterium |
|---|---|---|---|
| G0 | Pflichtenheft | Scope, Plattformbaseline, Delivery Flow und Pluginmodell. | Version 0.3 freigegeben. |
| G1 | Repository und CI | Repo-Struktur, Go/Svelte-Skeleton, Tests, ARM64-Build und Docs. | CI grün, kein Release und kein Deployment. |
| G2 | Release Pipeline | Signiertes Linux-ARM64-Bundle, Manifest, Checksums und GitHub Release. | Release aus Tag reproduzierbar erstellt. |
| G3 | Installer | `install.sh`, OS- und Architekturprüfung, systemd-Installation und Healthcheck. | Frisches Pi OS erfolgreich installiert. |
| G4 | Oarfish Core und Board Detection | OS-, Hardware- und Capability-Erkennung, Generic Mode und Geräteseite. | Pi korrekt erkannt, Ubuntu-Adaptergrenze vorhanden. |
| G5 | Embedded Dashboard | CPU, RAM, Temperatur, Frequenz, Storage, Netzwerk und Alerts. | Reale Daten innerhalb Budgets. |
| G5.1 | Lokaler Zugriffsschutz und Audit-Grundlage | Erstkonto-Einrichtung (ORB-ONB-002), lokales Administratorkonto mit Anmeldung, Abmeldung und Passwortänderung (ORB-AUTH-001), serverseitig invalidierbare Sitzungen mit sicheren Cookies (ORB-AUTH-002), Rate-Limit für Anmeldeversuche (ORB-AUTH-003), CSRF- und Herkunftsschutz (ORB-SEC-004), restriktive Datei-Rechte (ORB-SEC-005), Audit-Log (ORB-SEC-006) ohne Secrets (ORB-SEC-007), explizite Konfiguration für nicht-lokale Bindung (ORB-SEC-010). Design-Stopp mit ADR zu Passwort-Hashing, Sitzungsspeicher, Audit-Speicher unter Berücksichtigung der microSD-Schreiblast und Erstkonto-Einrichtung. | Zustandsändernde Endpunkte lehnen Anfragen ohne gültige Sitzung und ohne CSRF-Schutz ab; Audit-Einträge enthalten keine Secrets; Anmeldung auf dem Raspberry Pi verifiziert. |
| G6 | Self-Update | GUI, Update Manager, Signaturprüfung, atomare Aktivierung und Rollback. Update Helper als separater privilegierter Prozess. | Erfolgs- und Fehlerupdate auf Pi verifiziert. |
| G7 | System Core | Dienste, Journal, Prozesse, Reboot und Shutdown. Privilege Broker mit fester Allowlist (ORB-SEC-002). | Sicherer Dienstneustart End-to-End. |
| G8 | Plugin Runtime | Manifest, Lifecycle, Capability-Modell und Beispielplugin. Capability-Prüfung im Broker (ORB-SEC-003). | Fehlerhaftes Plugin blockiert Core nicht. |
| G9 | Tailscale Plugin | Setup, Login, Status, Enable/Disable und Audit. | Realer Workflow auf Pi. |
| G10 | Containers Plugin | Engine Detection, Liste, Details, Logs und Lifecycle. | Mindestens eine reale Engine verifiziert. |
| G11 | Oarfish Assist | Provider-Setup, Read-only Tools, Redaction und UI. | Reale read-only Diagnose. |
| G12 | Ubuntu Qualification | Ubuntu Server LTS ARM64 Adapter und physische oder gleichwertige Qualifikation. | Definierte Kombination erhält Verified oder Compatible. |
| G13 | Hardening und MVP | Security Review, UX, Performance, Dokumentation und Release. | Alle MVP-MUSS-Abnahmen bestanden. |
| Post-MVP | Weitere SBCs | Armbian, Rockchip und weitere Boardfamilien. | Je Board dokumentierter Supportstatus. |

G6 beginnt erst, wenn G5.1 PASS ist.

### 14.3 Prompt-Struktur für Claude Code

Jeder Gate-Prompt enthält:

- Rolle und Ziel der Session,
- verbindliche Quelldokumente,
- In-Scope- und Out-of-Scope-Liste,
- erwartete Dateien und Architekturgrenzen,
- Test- und Abnahmekriterien,
- Regeln für Git und GitHub Actions,
- Abschlussformat mit Ergebnissen und offenen Punkten.

Der mit diesem Paket gelieferte erste Prompt ist `docs/prompts/G1_REPOSITORY_AND_CI.md`. Er erstellt Repository und CI, aber noch keine Produktfunktionen, Releases, Installer oder Pi-Installation.

## 15. Risiken und Gegenmaßnahmen

| ID | Risiko | Bewertung | Gegenmaßnahme |
|---|---|---|---|
| R-01 | Zu breiter Board-Support ohne reale Hardwaretests | hoch | Supportstufen, Verified nur mit physischem Nachweis, Generic Mode. |
| R-02 | Raspberry-Pi-OS-Sonderfälle werden in den Core hartcodiert | hoch | Hardware- und OS-Trennung, Capability Detection, Ubuntu-Fixtures. |
| R-03 | Ressourcenlimit des Zero 2 W | hoch | Single Binary, keine DB, adaptive Intervalle und harte Budgets. |
| R-04 | microSD-Verschleiß | hoch | Logrotation, kein Metrics Store, Schreibbündelung und Caches. |
| R-05 | Instabiles WLAN während Update | hoch | Vollständiger Download vor Aktivierung, atomarer Symlink und Rollback. |
| R-06 | Release Supply Chain wird kompromittiert | hoch | Signaturprüfung, minimale Actions-Rechte, gepinnte Actions und reproduzierbare Metadaten. |
| R-07 | Privates Repository erschwert Installer und Updates | mittel | Vor Gate G2 öffentlichen Releasekanal festlegen, kein dauerhaftes Token als Default. |
| R-08 | Tailscale wird unbeabsichtigt Core-Abhängigkeit | mittel | Pluginvertrag und Installations- sowie Update-Test ohne Tailscale. |
| R-09 | Docker-Socket führt zu Root-Aequivalent | hoch | Explizite Warnung, separater Adapter, keine automatische Rechteerweiterung. |
| R-10 | Plugin Supply Chain | hoch | MVP nur First-Party, Signaturmodell vor externen Plugins. |
| R-11 | LLM-Halluzination oder Prompt Injection | hoch | Read-only MVP, typisierte Tools, Redaction und Quellen. |
| R-12 | Kernel-, Netzwerk- und Bootunterschiede zwischen Pi OS und Ubuntu | hoch | Capability Detection und getrennte OS-Adapter, keine festen Pfade. |
| R-13 | Scope Creep durch Armbian- und Boardvielfalt | hoch | Pi Zero 2 W als einziges MVP-Gate, weitere Boards Post-MVP. |
| R-14 | Plugin-UI wirkt inkonsistent | mittel | Gemeinsames Designsystem, UI-Slots und Reviewregeln. |

## 16. Definition of Done für das MVP

Der MVP gilt als abgenommen, wenn:

1. Ein GitHub-Repository mit vollständiger, versionierter Dokumentation existiert.
2. Pull Requests automatisierte Tests, Linting und Linux-ARM64-Build durchlaufen.
3. Ein signiertes Linux-ARM64-Release aus einem Versionstag erstellt werden kann.
4. `install.sh` Oarfish auf einem frischen Raspberry Pi OS Lite 64-bit installiert.
5. Oarfish nach Reboot automatisch startet und erreichbar ist.
6. Ein neues Release über die GUI erkannt, installiert und verifiziert werden kann.
7. Ein fehlerhaftes Update automatisch auf die vorherige gesunde Version zurückrollt.
8. Raspberry Pi Zero 2 W mit Raspberry Pi OS Lite 64-bit als Verified dokumentiert ist.
9. Hardware- und OS-Erkennung getrennt implementiert sind.
10. Ubuntu Server LTS ARM64 ohne grundlegenden Core-Umbau integrierbar ist und durch Fixtures oder Adaptertests vorbereitet wurde.
11. Board, OS, Kernel, Sensoren und Supportstatus korrekt angezeigt werden.
12. Dashboard, Dienste und Journal auf realer Hardware funktionieren.
13. Tailscale als optionales Plugin eingerichtet, gesteuert und deaktiviert werden kann.
14. Containerplugin mit mindestens einer realen Engine funktioniert.
15. Oarfish Assist optional, read-only und ohne Shellzugriff einen realen Zustand erklären kann.
16. Core ohne Tailscale, Containerengine und AI-Provider vollständig startet.
17. GUI-Kernpfade kein Terminal, YAML oder Dateiedit benötigen.
18. Ressourcen-, Schreiblast- und Startzeitmessungen die MUSS-Grenzen einhalten.
19. Security Review für Auth, Broker, Plugins, Updatepfad, Container und AI dokumentiert ist.
20. Es keinen bekannten kritischen oder hohen unbehandelten Security-Fund im MVP-Pfad gibt.

## 17. Verbleibende Entscheidungen und Defaultwerte

Keine der folgenden Entscheidungen blockiert Gate G1. Sie werden spätestens vor dem genannten Gate verbindlich getroffen.

| ID | Entscheidung | Default / Empfehlung | Fällig |
|---|---|---|---|
| D-01 | Repository-Name | `oarfish` | G1 |
| D-02 | Repository-Sichtbarkeit | G1 zunächst privat, vor produktnahem Installer öffentlicher Releasekanal | G2 |
| D-03 | Lizenz | Vor Öffnung des Repositories entscheiden, Apache-2.0 ist technischer Default | G2 |
| D-04 | Release-Signierung | Ed25519-basiert, bevorzugt Minisign-kompatibel | G2 |
| D-05 | Dev-OS | Raspberry Pi OS Lite 64-bit auf Zero 2 W | bestätigt |
| D-06 | Ubuntu-Ziel | Ubuntu Server LTS ARM64, nicht Ubuntu Core | bestätigt |
| D-07 | MVP-Authentifizierung | Lokales Single-Admin-Konto mit sicherer Session | G5.1 |
| D-08 | Erste Containerengine | Nach Ressourcen- und Supporttest auf dem Pi entscheiden | G10 |
| D-09 | AI-Provider | OpenAI-kompatibler Remote-Endpoint plus Ollama-kompatibler lokaler Endpoint | G11 |
| D-10 | ARMv7 | Kein erster Release, später optional | Post-MVP |
| D-11 | UI-Sprache | Deutsch als Default, Englisch technisch vorbereitet | bestätigt |
| D-12 | Produktbezeichnung | `Oarfish, SBC and Embedded Linux Management` | bestätigt |

Das erste physische Rockchip-Testgerät ist keine offene MVP-Entscheidung. Es wird erst relevant, wenn eine konkrete Rockchip-Kombination den Status Verified erhalten soll.

## 18. Glossar, Referenzen und Versionshistorie

### 18.1 Glossar

| Begriff | Bedeutung |
|---|---|
| SBC | Single-Board-Computer, zum Beispiel Raspberry Pi oder Rockchip-Board. |
| SoC | System-on-Chip, zentrale Prozessor- und Peripherieplattform eines Boards. |
| Board Profile | Oarfish-Datenmodell für ein konkretes Board. |
| OS Adapter | Betriebssystemspezifische Implementierung, getrennt vom Hardwareprofil. |
| Capability | Eng definierte, technisch überprüfbare Fähigkeit eines Systems oder Plugins. |
| Generic Mode | Sicherer Betrieb ohne spezifisches Boardprofil. |
| Plugin | Optionales Oarfish-Modul mit Manifest und expliziten Berechtigungen. |
| Privilege Broker | Eng begrenzter Prozess für autorisierte Systemaktionen. |
| Update Helper | Eng begrenzter Root-Helper für Oarfish-Aktivierung und Rollback. |
| Verified | Physisch getestete Board- und OS-Kombination. |
| Pull-Update | Das Zielgerät lädt ein Release selbstständig, statt von CI aus bespielt zu werden. |

### 18.2 Normative Dokumente im Repository

| Datei | Zweck |
|---|---|
| `docs/PFLICHTENHEFT.md` | Verbindliche Produkt- und Systemanforderungen. |
| `CLAUDE.md` | Kompakte Arbeitsregeln für Claude Code. |
| `docs/adr/` | Architekturentscheidungen und ihre Begründung. |
| `docs/prompts/` | Versionierte Gate-Prompts für Claude Code. |
| `README.md` | Einstieg, Build und Projektstatus. |

### 18.3 Informative Referenzen

- Armbian Documentation: https://docs.armbian.com/
- Raspberry Pi Documentation: https://www.raspberrypi.com/documentation/
- Ubuntu Server Documentation: https://documentation.ubuntu.com/server/
- GitHub Actions Documentation: https://docs.github.com/actions
- GitHub Releases REST API: https://docs.github.com/rest/releases
- Tailscale Documentation: https://tailscale.com/kb/

### 18.4 Versionshistorie

| Version | Datum | Änderung |
|---|---|---|
| 0.1 | 21. Oktober 2026 | Initialer Entwurf mit generischem Linux- und Single-Host-Fokus. |
| 0.2 | 21. Oktober 2026 | Fokus auf ARM-SBCs, Board Profiles und First-Party-Plugins. |
| 0.3 | 22. Oktober 2026 | Pi Zero 2 W als einziges MVP-Referenzgerät, Raspberry Pi OS zuerst, Ubuntu Server LTS ARM64 als verbindliches Folgeziel, pull-basierte GitHub-Releases, Installationsskript und GUI-Self-Update; Self-hosted Runner und Push-Deployment entfernt. |
| 0.3.1 | 23. Oktober 2026 | Produkt von Orbit in Oarfish umbenannt (Name, Binary, Pfade, Repository). Anforderungs-IDs `ORB-*` bleiben unverändert. |
| 0.3.2 | 27. Oktober 2026 | Abschnitt 3.2: Andere Raspberry-Pi-Modelle sind Compatible, sobald ein Board- oder Familienprofil existiert; bis dahin Generic mit den erkannten Pi-Fähigkeiten (Entscheidung D-C26, ADR 0007). |
| 0.3.3 | 27. Oktober 2026 | Abschnitt 14.2: neues Gate G5.1 "Lokaler Zugriffsschutz und Audit-Grundlage" zwischen G5 und G6, G6 setzt G5.1 PASS voraus (D-C27); Update Helper in G6, Privilege Broker in G7, Capability-Prüfung im Broker in G8 (D-C28); bis G5.1 PASS keine nicht-lokale Listen-Adresse (D-C29); Abschnitt 17: D-07 fällig in G5.1; Abschnitt 6.4: Anforderungs-IDs ORB-UPD-001 bis 003 in ORB-OSU-001 bis 003 umbenannt, Abschnitt 6.12 unverändert (D-C30); Kopfzeile: Status und Stand aktualisiert. |
