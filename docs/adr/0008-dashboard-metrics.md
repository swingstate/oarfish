# ADR 0008: Dashboard metrics from files, streamed with Server-Sent Events, sampled only while watched

- Status: **Accepted** (2026-10-28, accepted by the project owner). Decision 8 is amended; see there.
- Date: proposed 2026-10-27, accepted 2026-10-28
- Amended after R1 (2026-11-06, `docs/DECISIONS.md` D-C72, D-C73): section "Amendment after R1" at the end (optional history, default route and DNS display).
- Gate: G5

## Context

Gate G5 adds the Home page with live values of the device (spec 6.3, 7.2, 8.7). The device is a Raspberry Pi Zero 2 W with 512 MB of RAM, an SD card and a service sandbox without `AF_NETLINK` (unit file, ADR 0006). The idle budgets are strict: at most 35 MB RSS, under 1 % of one core in a five-minute average, and no high-frequency writes to the SD card (ORB-NFR-001, -003, -009). ADR 0007 and decision D-C19 already settled that detection reads files and runs no program. The specification does not say how metrics are collected, delivered or bounded. This ADR records the answers. Each numbered decision was a `D-DRAFT-G5-nn` item in the pull request of Gate G5 and is now a confirmed decision D-C31 to D-C41 in `docs/DECISIONS.md` (decision 10 refers to D-C29).

## Decisions

**1. Metrics are read from files; no program runs (extends D-C19).** CPU time from `/proc/stat`, load, uptime, memory and swap from procfs, the host name from `/proc/sys/kernel/hostname`, frequency and governor from `cpufreq` in sysfs, temperatures from `thermal_zone*` and `hwmon*`, mounts from `/proc/self/mountinfo`, network state and counters from `/sys/class/net/<if>/`, addresses from `/proc/net/fib_trie` and `/proc/net/if_inet6`. All reads go through the read-only, size-limited file source of ADR 0007. The one system call is `statfs(2)` (`syscall.Statfs`, no cgo) for the size of a mounted file system; it needs no privilege and runs no program. The file allow-list is enforced by a test. Why: it is the same guarantee as for detection, it works under the unchanged unit, and it is testable with fixture trees.

**2. No netlink.** `net.Interfaces` and `net.InterfaceAddrs` use netlink, which the unit does not allow (`RestrictAddressFamilies` has no `AF_NETLINK`). Network data therefore comes from sysfs and procfs only. Cost: `/proc/net/fib_trie` lists IPv4 addresses without their interface. To assign them, the collector reads `/proc/net/route` as well (the directly connected networks per interface) and gives an address to the interface whose network contains it. An address without such a route is not shown. This reads one file more than the task text names; it is the only file-based way to get the assignment. Hardware note: reading `/proc/net/route` worked on the Raspberry Pi Zero 2 W under the unchanged service unit (`v0.1.0-alpha.4`, see [`INSTALL.md`](../INSTALL.md#gate-g5-dashboard)).

**3. Server-Sent Events, not polling.** `GET /api/v1/events` pushes one `metrics` event about every two seconds, an `alerts` event when the list changes, a `history` event once at the start and a heartbeat comment every 15 seconds. `GET /api/v1/metrics` returns one snapshot for the first paint. Why: one open connection is cheaper for a Pi than a request every two seconds (headers, routing, JSON per request), and the browser reconnects by itself. Cost: a stream holds a connection and a goroutine; see decision 6.

**4. Sampling only while somebody watches.** The sampler has no goroutine, no timer and no file read until the first stream client connects, and it stops when the last one leaves. While watched it samples every 2 seconds; storage, network addresses, link speed, the sensor list and the governor are read every 30 seconds. The browser closes the stream when its tab is hidden. Why: ORB-DASH-006 asks for adaptive intervals and no permanent collection; an unwatched dashboard costs nothing.

**5. History is a ring in memory only.** 60 points per series (CPU usage, memory use, CPU temperature), that is two minutes at the default interval, about 3 KB. It is cleared when the last client leaves, so it never shows stale data and nothing is written to the SD card (ORB-NFR-009).

**6. Everything is bounded.** At most 4 stream clients (the fifth gets a typed `503`), 16 sensors, 8 mounts (after de-duplication, from at most 64 candidate devices), 8 interfaces, 4 addresses per interface and family, every file read limited in size, every directory listing limited to 512 entries. A client that does not read is dropped by a per-write deadline of 10 seconds. The sampler never waits for a client: each client holds one frame and the newest replaces the unread one.

**7. The stream and the server timeouts.** `http.Server.WriteTimeout` covers a whole response and would end every stream after 30 seconds. The stream handler sets its own write deadline (`http.ResponseController`) before each write instead, which replaces the server's. `ReadTimeout` needs no change (verified by a test with 200 ms timeouts). On shutdown the handler closes all streams; without that, a graceful `Shutdown` would wait for connected browsers until its timeout. Why per-write deadlines rather than no timeout: a stalled client must free its goroutine and its slot.

**8. Alert rules are code with named thresholds (amended, see the end of this decision).** A small rule engine with three levels. Alerts carry a stable id, a level and cause, impact and next step as translation keys with parameters; Go builds no user text. Thresholds are constants in one table (docs/DASHBOARD.md) and are not configurable in G5. Table: CPU temperature 70 / 80 °C; memory available below 15 / 8 %; swap in use from 1 MiB (amended below to 25 percent of the swap total); a real file system above 85 / 95 %; no link on any interface; one-minute load above the number of cores; informational for Generic Mode and Compatible. The 1 MiB for swap is a choice of this ADR: the task text says "swap in use", and a few KiB that the kernel parks there are normal.

**Amendment (2026-10-28, project owner).** The swap alert becomes relevant from **25 percent of the swap total in use**, replacing "from 1 MiB". For context: the hardware reading of `v0.1.0-alpha.4` found 1.5 MiB parked in swap without any swap activity, which is above the old threshold. The amendment is implemented by the alert tuning task (D-C42) after `v0.1.0-alpha.4`; that release still alerts from 1 MiB. The rule needs a known swap total greater than 0; with no swap configured it raises nothing, and the text says "occupied", not "swapping", because occupied swap is not swap activity. The informational alert for the Compatible tier was removed by decision D-C44 (the status stays in the Home header and on the Device page); the informational alert for Generic Mode stays.

**9. Privacy by construction.** The host name and the IP addresses are shown on the live page and in the live API only. The hardware address of an interface (`/sys/class/net/<if>/address`) is never read. IPv6 link-local addresses and IPv6 addresses built from the hardware address (EUI-64) are not shown, because both contain the MAC address. Mount points under `/home/<user>`, `/media/<user>` and `/run/media/<user>` are shortened so that no user name appears. Machine id, serial number, environment, process list, user databases and SSH data are never read. A test lists every file the collector may open.

**10. The listen guard (D-C29).** The binary refuses to start unless `-listen` is a literal loopback IP address with a port. No override. This stays until Gate G5.1 (amended by D-C48: it stays after G5.1 as well, until LAN access and HTTPS are designed, D-D10).

**11. Frontend.** Svelte 5, no chart library, no new dependency. Sparklines are inline SVG. Transfer rates are computed in the browser from the counters. Only the alert list is an `aria-live` region. The old "Status" view and its `/healthz` check are replaced by Home, whose connection state tells the same thing and more.

## Consequences

- A request to `/api/v1/metrics` without a stream reads the device twice, 250 ms apart, so that CPU usage exists; the answer is reused for one second.
- The first sample of a stream is published 0.5 seconds after the first client connects, so that CPU usage is not empty.
- More than four browser windows on Home are refused until one closes. Browsers also limit connections per host; the cap keeps the device from being the one that gives.
- The collector reads about 15 small files per sample. The benchmark in `internal/metrics` measures the work, not the time on the device. Idle CPU and RSS were measured on one Raspberry Pi Zero 2 W with `v0.1.0-alpha.4` (docs/INSTALL.md, docs/DASHBOARD.md "Verification status").
- Not decided here: throttling and undervoltage warnings (ORB-DASH-004, needs a safe board adapter), the plugin and application summary (ORB-DASH-007), the resource view of external daemons (ORB-NFR-017), persisted history, configurable thresholds, authentication of the API (Gate G5.1).

## Amendment after R1

Recorded after the owner's requirements decisions of 2026-11-05 and 2026-11-06 (`docs/DECISIONS.md` D-C72 and D-C73; specification 0.4.0). Documentation only: no code, test, unit or workflow changed, and nothing here is verified on a device. Labels: **owner decision**, **fact**, **proposal**, **assumption**. Decisions 1 to 11 above are unchanged except where this section says so.

**R1.1 Optional history (amends decision 5 and D-C35; refines decision 4).**

- **Owner decision:** the live display and the live ring of decision 5 (60 points, memory only, cleared when the last client leaves) are unchanged. Sampling for the live display stays only while somebody watches (decision 4).
- **Owner decision:** in addition there is an optional history of CPU usage, memory use and temperature. It is an additional, opt-in store beside the live ring, with a fixed upper bound, replaced atomically, never unbounded and never a metrics database (specification ORB-STO-007).
- **Owner decision:** the background measurement for it runs only when the administrator switches it on. The default is off. With it off, nothing is measured without a watcher.
- **Owner decision:** a setting is needed for the switch; the settings store belongs to the design stop H1 (D-C73).
- **Assumption, not decided:** guideline of 24 hours at about 5 minutes. Intervals, number of points, size of a snapshot, write rhythm by medium type (rare on microSD and on an unknown medium, more often on eMMC and NVMe), idle CPU and write counts are open until H1 and a measurement on the device. An earlier size estimate of about 8 KB per snapshot is not measured.
- **Open:** the write measurement and the medium-type detection on the reference device are part of the spike S1 and have not been done.

**R1.2 Default route and DNS servers (ORB-NET-001; refines decision 2).**

- **Owner decision:** the network display shows the default route and the DNS servers, plus SSID, signal strength and "IP details" (prefix, gateway and IPv6). The WLAN is shown only, never changed.
- **Fact (repository, checked before this amendment):** the route parser skips routes that have a gateway, and the backend reads no DNS file. ORB-NET-001 is therefore not yet met.
- **Owner decision:** D-C31 and D-C32 stay in force (no program, no netlink, files only) until a later amendment decides a source for the new values. No allow-list entry is added by this amendment.
- **Open until the device spike S1:** the source of the SSID and the signal (if it can only be read through netlink, a further amendment must say so, read-only and with the address families of the unit) and the source of the DNS servers (a local resolver stub may hide the real servers; assumption, not checked).

## Amendment after S1

Recorded after the device spike S1, which the owner ran on 2026-11-06 (`docs/DECISIONS.md` D-C78; specification 0.4.1). Documentation only: no code, test, unit, installer, allow-list or workflow changed. The report file is private and not in the repository; the numbers below are taken from the owner's summary of it. Labels: **owner decision**, **fact (repository)**, **device observation**, **proposal**, **assumption**. Nothing here is Verified: every observation comes from one device and one run. Decisions 1 to 11 and the R1 amendment above are unchanged except where this section says so.

**S1.1 What was observed.** Device: one Raspberry Pi Zero 2 W Rev 1.0, Debian 13 (trixie) arm64, kernel 6.18.50+rpt-rpi-v8, systemd 257, NetworkManager active, systemd-resolved and systemd-networkd inactive.

| Topic | Device observation (class: device observation, one device, one run) |
|---|---|
| Signal strength | `/proc/net/wireless` has a line for the WLAN interface: link quality 61, level -49 dBm. A netlink probe at the same time gave -48 to -50 dBm. A plain file; netlink is not needed. |
| SSID | Not found in any of 4 searched files. Available through nl80211 netlink only (GET_INTERFACE, SSID length 6). With the shipped hardening (`RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6`) the netlink socket fails with errno 97. With `AF_NETLINK` added it works for the service user and for an unrelated unprivileged account with empty capability sets (read-only GET_INTERFACE and GET_STATION requests). |
| Storage medium | Root on the first card (`mmcblk0p2`), ext4, mount options `rw,noatime`; `/sys/block/mmcblk0/device/type` says SD; rotational 0; size about 128 GB. The device serial, CID and CSD files exist and must never be read. |
| Idle write baseline | One window of 300.8 s: 64 sectors = 32,768 bytes written to the card (about 392 KB per hour extrapolated; the counter is coarse). The Oarfish process wrote 0 bytes. The journal is volatile (RAM). One quiet window only. |
| Undervoltage | `/sys/class/hwmon/hwmon1` is `rpi_volt` with `in0_lcrit_alarm` (0 at the time). No file with throttle flags. The vendor tool showed 0x0 (comparison only). |
| CPU frequency and temperature | cpufreq files exist (cur 1,000,000, min 600,000, max 1,000,000 kHz, governor ondemand). One thermal zone, 46.16 degC. |
| DNS | `/etc/resolv.conf` is a regular file with 2 nameserver lines (one IPv4 public, one IPv6 private). No resolver stub on this image. |
| Routes and prefixes | `/proc/net/route` has a default row (WLAN interface, private gateway). `/proc/net/ipv6_route` has a default route (link-local gateway). IPv4 prefix /24 from the connected-route row. IPv6: 4 global addresses (3 unique-local), prefix /64. |
| OS update display | `/var/lib/dpkg/status` 597 KB, 638 installed packages, a counting pass 9 ms. `/var/lib/apt/lists` 33 files, about 155 MB. No update-success stamp and no updates-available file. The device has no battery clock, so file ages are only indicative. |
| Not covered | Reading the new files as the service user under the real unit hardening (only the netlink probe ran under hardening); more than one 300 s window; eMMC or NVMe; other Pi models; Ubuntu; a latched undervoltage state. |

**Fact (repository):** the dashboard collector reads `route`, `if_inet6` and `fib_trie`, skips routes that have a gateway, and does not read `ipv6_route`.

**S1.2 File sources identified (device observation; refines R1.2, ORB-NET-001, ORB-DASH-004).**

- Signal strength: `/proc/net/wireless`.
- Default route and prefixes: the default row of `/proc/net/route` (IPv4) and `/proc/net/ipv6_route` (IPv6); the prefix from the connected-route row and from `if_inet6`.
- DNS servers: `/etc/resolv.conf`. **Assumption, not tested:** an image with a local resolver stub (for example systemd-resolved) lists the stub address there and hides the real servers; such images need a fallback design, which this amendment does not make.
- Storage medium: the sysfs block device name of the root file system and its `device/type`. The identity files of the card are never read.
- Undervoltage: the `rpi_volt` hwmon alarm file. **Assumption, not checked:** whether the alarm is live or latched. A frequency cap is not available as a flag.

**S1.3 Allow-list (proposal, not done).**

- **Proposal:** the tasks N0 and N1 add `/proc/net/wireless`, `/proc/net/ipv6_route` and `/etc/resolv.conf` to the read allow-list of ADR 0007 and ADR 0008, each with a bounded parser and a test. This amendment adds no entry.
- **Open:** reading these files as the service user under the unit hardening is not verified. N0 and N1 must test it on the device.

**S1.4 D-C31 and D-C32 stay in force (owner decision, D-C72).** No netlink and no program. Every source in S1.2 is a file.

**S1.5 SSID (open).**

- **Device observation:** the SSID needs nl80211 netlink, so it needs `AF_NETLINK` in the service unit.
- **Open, owner has not decided:** a further amendment must lift D-C32 for one read-only nl80211 query (GET_INTERFACE only, no write request) and change the unit and the installer. Whether this happens now or together with the unit and installer change of D-D10 is not decided.
- **Recommendation (not a decision):** do it together with D-D10, because both change the unit and the installer. Until then the Network page shows signal strength and no SSID, and ORB-NET-002 is met in part.

**S1.6 Write baseline (input for H1 only).**

- **Device observation:** about 392 KB per hour of idle writes on the SD card in one quiet window, none from the Oarfish process.
- **Open:** the real cost of an atomic replace of a history file (data, journal, directory) is not measured and is part of H1. The baseline decides nothing about intervals or rhythm. On this device the medium is an SD card, which supports the direction of D-C73 (rare writes on microSD); it does not set a number.

**S1.7 Operating system update display (refines D-C74).**

- **Device observation:** counting installed packages is cheap (9 ms), but a count of available updates cannot be derived without parsing the package lists (about 155 MB) and re-implementing the package manager's candidate logic.
- **Owner decision (D-C78):** the display is not part of the MVP. It may come after G7 as a typed broker action, or be dropped; that is open.

