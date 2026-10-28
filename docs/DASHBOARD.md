# Dashboard and metrics (Gate G5)

The Home page shows live values of the device without a page reload: host name, board, SoC, operating system, uptime, CPU (usage, load, frequency, governor), memory and swap, temperatures, storage per file system, network interfaces, and prioritised alerts that name cause, impact and next step. The normative sources are specification 6.3 (ORB-DASH-001..007), 7.2, 8.7 and 11. The design decisions are recorded in [ADR 0008](adr/0008-dashboard-metrics.md) (Accepted, D-C31 to D-C41).

Scope: read-only collection from files, the two API endpoints, the alert rules, the Home page, and the listen guard of decision D-C29. Not in G5: authentication, accounts and audit (G5.1, D-C27), any state-changing endpoint, throttling and undervoltage warnings (ORB-DASH-004, needs a safe board adapter), the plugin and application summary (ORB-DASH-007), processes, journal and services (G7), the resource view of external daemons (ORB-NFR-017), persisted history, configurable thresholds, `/readyz`.

**Status: implemented and tested against synthetic fixtures. Measured and checked once on one Raspberry Pi Zero 2 W with `v0.1.0-alpha.4`** ([hardware result](INSTALL.md#gate-g5-dashboard)), and the redesigned Home viewed once with `v0.1.0-alpha.5` ([result](INSTALL.md#ui-foundation-v010-alpha5)); support status stays Compatible, not Verified. What is still open is listed under [Verification status](#verification-status).

## Where the values come from

Everything is read from files through the read-only, size-limited file source of [ADR 0007](adr/0007-detection-and-profiles.md) (`internal/platform/source`). No program is executed, there is no cgo, and nothing uses netlink (the service sandbox has no `AF_NETLINK`). All text from the system is untrusted: reads are bounded, text is cleaned (invalid UTF-8 replaced, control characters removed, length capped), and file content is never used as a command. The only system call is `syscall.Statfs`.

| Metric | File | Parser | Read limit | Tier |
|---|---|---|---|---|
| Host name | `/proc/sys/kernel/hostname` | `source.Clean`, 64 characters | 128 B | fast |
| Uptime | `/proc/uptime` | `parse.ParseUptime` | 128 B | fast |
| Load average | `/proc/loadavg` | `parse.ParseLoadavg` | 128 B | fast |
| CPU usage, logical cores | `/proc/stat` | `parse.ParseStat`; usage is the delta of two samples | 64 KiB | fast |
| CPU frequency, current | `/sys/devices/system/cpu/cpu0/cpufreq/scaling_cur_freq` | kHz to MHz, 0 to 10 GHz | 128 B | fast |
| CPU frequency, maximum | `.../cpufreq/cpuinfo_max_freq`, else `scaling_max_freq` | same | 128 B | slow |
| Governor | `.../cpufreq/scaling_governor` | `source.Clean`, 32 characters | 128 B | slow |
| Memory, swap | `/proc/meminfo` | `parse.ParseMeminfo` (`MemTotal`, `MemAvailable`, `SwapTotal`, `SwapFree`; without `MemAvailable`: free + buffers + cached) | 16 KiB | fast |
| Temperature sources | `/sys/class/thermal/thermal_zone*/{type,temp}`, `/sys/class/hwmon/hwmon*/{name,temp1..8_input,temp1..8_label}` | millidegrees, -60 to 200 °C | 128 B | list: slow, value: fast |
| Storage | `/proc/self/mountinfo` plus `statfs` per mount | `parse.ParseMountinfo` | 1 MiB | slow |
| Interface list, kind, speed | `/sys/class/net/<if>/{type,speed,wireless,phy80211}` | strict interface names | 128 B | slow |
| Link state, counters | `/sys/class/net/<if>/{operstate,statistics/rx_bytes,statistics/tx_bytes}` | known operstate values, counters up to 2^56 | 128 B | fast |
| IPv4 addresses | `/proc/net/fib_trie` (LOCAL entries) and `/proc/net/route` (to assign an address to an interface) | `parse.ParseFibTrieLocal`, `parse.ParseRoutes` | 256 KiB | slow |
| IPv6 addresses | `/proc/net/if_inet6` | `parse.ParseInet6` | 256 KiB | slow |

Fast values are read on every sample (about every 2 seconds while somebody watches), slow values every 30 seconds. A value that cannot be read is `null`, never an error. Cpufreq that the kernel does not provide is `null`, not a failure.

**Temperature sources.** A zone or sensor without a plausible reading when the list is built (every 30 s) is not listed; one that fails later shows as unavailable until the next rebuild. Thermal zones come first, named by their `type` (`cpu-thermal` on the Raspberry Pi Zero 2 W). A hwmon device whose name equals a thermal zone's name is not listed again (many drivers appear in both). Other hwmon sensors are listed with their label, or their name (with the input number if the device has several). The sensor that stands for the processor (`cpu: true`) is the one with a known processor name (`cpu-thermal`, `soc-thermal`, `x86_pkg_temp`, `coretemp`, `k10temp`, `cpu`); if none has such a name, the first sensor, the same fallback as device detection. The alerts and the sparkline use the hottest `cpu` sensor.

**Storage.** Only file systems of real storage types are listed: `ext2`, `ext3`, `ext4`, `vfat`, `exfat`, `f2fs`, `xfs`, `btrfs`, `ntfs3`. Pseudo, memory (`tmpfs`) and network file systems are skipped. One entry per device: bind mounts and btrfs subvolumes show the same storage and are merged, keeping the mount whose root is `/` and then the shortest mount point. Used and size are computed like `df` does (used / (used + available); blocks reserved for root count as neither). A mount that cannot be read is skipped. The mount point from `mountinfo` is decoded (`\040` is a space), checked (absolute, printable, no `.` or `..`) and used only as the argument of `statfs`, never to open a file. To keep user names out of the page, `/home/<user>…` is shown as `/home/…`, and `/media/<user>/<label>` and `/run/media/<user>/<label>` as `/media/<label>`.

**Network.** The loopback interface is not listed, and neither are entries of `/sys/class/net` that are not interfaces (a file such as `bonding_masters`). Interfaces with a physical-looking name (`en*`, `eth*`, `wl*`, `ww*`, `usb*`) come first, so that container interfaces cannot push them out of the cap. The kind is `wifi` if the interface has `wireless` or `phy80211`, `ethernet` if its type is 1, else `other`. IPv4 addresses are listed in `fib_trie` without their interface. They are assigned to the interface whose directly connected network in `/proc/net/route` contains them (most specific wins); an address without such a route is not shown. The route file prints addresses as native little-endian integers, which holds for ARM64 and x86-64. IPv6 addresses are listed per interface; link-local addresses, tentative or failed addresses and addresses built from the hardware address (EUI-64, `ff:fe` in the middle) are left out, because the first and last contain the MAC address.

## Privacy rules

- The host name and the IP addresses appear in the live GUI and the live API only. They are not logged, not in test output, not in fixtures (except the synthetic ones below), not in documentation, not in commit messages or pull request descriptions.
- Never read, and never shown: MAC addresses (`/sys/class/net/*/address` is not on the allow-list), serial numbers, machine id, user names, environment variables, process lists, SSH data.
- Fixtures contain only the host name `oarfish-test` and IP addresses from the documentation ranges (`192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`, `2001:db8::/32`). `TestFixturesContainNoPrivateData` enforces that and nothing else is allowed.
- `TestMetricsOpenOnlyAllowListedFiles` lists every file the collector may open; `TestMetricsNeverReadsIdentifyingFiles` plants identifying files in a tree and shows that none is opened and none reaches the output.

## Sampling model and budgets

```text
no client                 first client                     last client leaves
no goroutine, no timer ─▶ baseline read, wait 0.5 s ─▶ sample every 2 s ─▶ loop stops, ring cleared
no file read              publish first sample        (slow tier every 30 s)
```

- **Only while watched** (ORB-DASH-006, ORB-NFR-003, ORB-NFR-009). The sampler is idle until `GET /api/v1/events` has a client. The browser closes the stream when its tab is hidden and opens it again when the tab is shown.
- **Nothing is written to disk.** History is a ring of 60 points per series (CPU usage, memory use, CPU temperature), two minutes at the default interval, about 3 KB, in memory, cleared when the last client leaves.
- **Bounds.**

| What | Limit |
|---|---|
| Concurrent stream clients | 4 (then `503 too_many_stream_clients`) |
| Time one write to a stream client may take | 10 s, then the client is dropped |
| Frames a client holds | 1 (the newest replaces an unread one) |
| Sensors | 16 |
| Mounts shown / devices asked for their size | 8 / 64 |
| Interfaces | 8 |
| Addresses per interface and family | 4 |
| Directory entries read | 512 per directory |
| Lines parsed per file | 8192 (mountinfo: 4096) |
| History points | 60 per series |

- **A one-shot request** (`GET /api/v1/metrics`) while nobody streams reads the device twice, 250 ms apart, so that CPU usage exists, and reuses the result for one second. It does not start a sampling session.
- **Server timeouts.** `http.Server.WriteTimeout` (30 s) bounds a whole response and would cut every stream. The stream handler sets a write deadline before each write instead, which replaces the server's. `ReadTimeout` needs no change; `TestEventStreamOutlivesTheServerTimeouts` runs a stream through a server with 200 ms timeouts. On shutdown the handler ends all streams, so `Shutdown` does not wait for browsers (`TestShutdownEndsStreams`).
- **Work per sample.** About 15 small file reads. `BenchmarkCollect` on the Pi Zero 2 W fixture (a developer Mac, so it shows work, not the time on the device) measured about 184 µs, 28 KB and 219 allocations per sample. Idle CPU and RSS on the Pi were measured once, see the [hardware result](INSTALL.md#gate-g5-dashboard) and [Verification status](#verification-status).

## API

All endpoints are under `/api/v1`, `GET` only, read-only, without request body, with `Cache-Control: no-store` and the security headers of every response. `GET /api/v1/device` is unchanged ([`DETECTION.md`](DETECTION.md)). Errors are typed: `{"error":{"code","message","recovery"}}` (spec 8.7).

| Status | `code` | When |
|---|---|---|
| 404 | `not_found` | unknown path under `/api/` |
| 405 | `method_not_allowed` | any method except `GET` (`Allow: GET`) |
| 400 | `request_body_not_allowed` | a `GET` with a body |
| 503 | `metrics_unavailable` | no sampler in this build, or the device could not be read |
| 503 | `too_many_stream_clients` | the fifth stream client (`Retry-After: 3`) |
| 503 | `shutting_down` | a stream requested during shutdown (`Retry-After: 3`) |

### `GET /api/v1/metrics`

One snapshot. `schema_version` changes only when a field is changed or removed, not when one is added. Unknown values are `null`, lists are never `null`.

| Field | Meaning |
|---|---|
| `schema_version` | `1` |
| `collected_at` | RFC 3339, UTC, milliseconds |
| `hostname` | text or `null` |
| `uptime_seconds` | number or `null` |
| `cpu.usage_percent` | 0 to 100, since the previous sample; `null` for the first |
| `cpu.cores` | logical cores or `null` |
| `cpu.load` | `{one, five, fifteen}` or `null` |
| `cpu.frequency` | `{current_mhz, max_mhz}`, each a number or `null` |
| `cpu.governor` | text or `null` |
| `memory` | `total_bytes`, `available_bytes`, `used_bytes` (total minus available), `swap_total_bytes`, `swap_used_bytes`; each a number or `null`; swap is `null` when the kernel gives no swap data |
| `temperatures[]` | `id` (`thermal_zone0`, `hwmon0_temp1`), `name`, `source` (`thermal_zone`, `hwmon`), `cpu` (bool), `celsius` (0.1 °C or `null`) |
| `storage[]` | `mount_point`, `fs_type`, `total_bytes`, `used_bytes`, `available_bytes`, `used_percent` (0.1) |
| `network[]` | `name`, `kind` (`ethernet`, `wifi`, `other`), `state` (operstate), `link_up`, `speed_mbps` or `null`, `rx_bytes` and `tx_bytes` (counters) or `null`, `ipv4[]`, `ipv6[]` |
| `device` | static summary for Home: `model`, `soc_family`, `arch`, `os_pretty_name`, `kernel_release`, `support_tier`, `support_reason`, `support_explanation_key`, `generic_mode` (from device detection) |
| `alerts[]` | see [Alerts](#alerts) |
| `history` | `{schema_version, interval_seconds, cpu_percent[], memory_used_percent[], temperature_celsius[]}`, oldest first, `null` for an unreadable point; empty while nobody streams |

### `GET /api/v1/events` (Server-Sent Events)

`Content-Type: text/event-stream`. Every message is one line of JSON.

| Message | When |
|---|---|
| `retry: 3000` | first line: the browser reconnects after 3 s |
| `event: history` | once, right after the start: the ring |
| `event: alerts` | after `history`, and whenever the alert list (or a value in it) changes: `{"schema_version":1,"alerts":[…]}` |
| `event: metrics` | about every 2 s: the snapshot above without `device`, `alerts` and `history` |
| `: heartbeat` | a comment every 15 s, so that the line is seen to be alive |

A client that joins a running session receives the latest sample at once; the first client gets its first sample 0.5 s after connecting. A refused connection (`503`) is not retried by the browser's `EventSource`; the page reconnects by hand with a growing delay (3 s, 6 s, 12 s, up to 30 s).

## Alerts

A small rule engine (ORB-DASH-005) with three levels: `critical`, `relevant`, `informational`. An alert has a stable `id` (the same rule gives the same id; for storage the id ends with the mount point), a `level`, and `cause`, `impact` and `next_step` as `{key, params}`: a translation key with parameters. Go builds no user text; German and English texts are in `frontend/src/lib/locales/`. Alerts are sorted by level, then in the order of this table. The thresholds are named constants in `internal/metrics/alerts.go` and are not configurable in G5.

| Rule (`id`) | Level | Condition | Parameters |
|---|---|---|---|
| `cpu_temperature` | relevant | hottest CPU sensor ≥ **70 °C** | `celsius`, `threshold` |
| `cpu_temperature` | critical | hottest CPU sensor ≥ **80 °C** | `celsius`, `threshold` |
| `memory_available` | relevant | available memory < **15 %** of the total | `percent`, `threshold` |
| `memory_available` | critical | available memory < **8 %** | `percent`, `threshold` |
| `swap_in_use` | relevant | swap total known and > 0, and swap occupied ≥ **25 %** of the swap total | `percent`, `used_mib` |
| `storage_usage:<mount>` | relevant | a real file system used > **85 %** | `mount`, `percent`, `threshold` |
| `storage_usage:<mount>` | critical | used > **95 %** | `mount`, `percent`, `threshold` |
| `network_no_link` | relevant | interfaces exist, but none has `operstate` `up` | none |
| `cpu_load` | relevant | one-minute load > number of logical cores | `load`, `cores` |
| `support_generic` | informational | support tier is Generic (Generic Mode) | none |

Translation keys: `alert.<rule>.<level>.<cause|impact|next>` for the three rules with two wordings (temperature, memory, storage), `alert.<rule>.<cause|impact|next>` for the others. A value that is unknown (a sensor that cannot be read, no memory data) raises no alert. A system that lists no network interface at all is not alerted either: that is far more often an unreadable sysfs than a missing network. Swap that is occupied is not swap that is active: pages the kernel parks there stay without any swap activity (1.5 MiB of 415 MiB were found on the real device). The swap rule therefore looks at the share of the swap total, a swap total that is unknown or 0 raises no alert, and the texts say "belegt" / "occupied", not that the system is swapping. The support tier "Compatible" raises no alert (D-C44); it is shown in the Home header and on the Device page. Only Generic Mode has an informational alert.

## Home page

Top to bottom: a summary strip (host name, board and SoC, OS, uptime, support status with a link to the Gerät page), the alerts (grouped by level with a count per level, in the only `aria-live` region of the page; the alerts are the first section of the page), and cards for CPU, memory (with swap), temperature (every source, the SoC one labelled), storage (one usage bar per file system), and network (per interface a state badge, addresses, transfer rate computed in the browser from the counters). Each alert shows a level icon, a short title, the cause, the impact and the next step. CPU, memory and temperature have an inline-SVG sparkline from the ring. Governor, core count, longer load averages, file system types, sensor ids, interface kind, speed and totals are behind "Technische Einzelheiten" (progressive disclosure, ORB-GEN-003). The connection state is a small indicator ("Live") at the top right. The page sits in an app shell (sidebar, or top bar on a narrow screen); the look, the components and the states are described in [`UI.md`](UI.md).

- **Connection (ORB-NFR-014).** `EventSource` with the server's retry hint. A dropped connection is shown by the indicator ("Verbindet neu …") and a notice, every card is marked "Veraltet" and dimmed, and the old values stay visible until fresh data arrives. An open but silent stream (7 s without a value) is marked stale as well. A refused connection is retried by hand. A malformed event is ignored and counted, never shown, and does not clear the last good values. A reading older than the one on screen never replaces it.
- **Only while visible.** The stream is closed when the tab is hidden and opened again when it is shown, and when the page is left.
- **Texts** are in `frontend/src/lib/locales/de.ts` (German, default) and `en.ts` (prepared, same keys and placeholders); no text is in a component. The state and label texts are one named group (`deUi`), the sentences chosen by an API key another (`deApi`). Unit symbols (`MiB`, `Mbit/s`) are in `lib/format.ts`.
- **Accessibility (ORB-NFR-011).** Native elements only (`a`, `details`, `summary`, `button`, `progress`), so the page works from the keyboard; landmarks, a skip link and logical headings; sparklines are images with a description and icons are hidden from assistive technology; only the alert list is `aria-live`, not every metric update; nothing animates when a value updates and `prefers-reduced-motion` switches all transitions off. Colour is never the only signal: levels, link states and the stale state have text or a shape. Contrast is computed and tested for both themes ([`UI.md`](UI.md#accessibility)).
- **Size.** The initial frontend is 41,045 bytes (gzip -9: `index.html` 265, CSS 4,522, JS 36,258) against the budget of 750,000 (ORB-NFR-006); it was 29,191 before the UI foundation task. No dependency was added.
- **Interface honesty.** An interface without a link is neutral grey "Kein Link" and an unknown link state is neutral grey "Unbekannt", not a warning. The only network alert stays "no interface has a link".

## Listen guard (D-C29)

Until Gate G5.1 is PASS, the binary starts only if `-listen` is a literal loopback IP address with a port: `127.0.0.0/8` or `[::1]`. It refuses an empty host (`:8484`), the unspecified addresses, every other IP address, every host name including `localhost` (the message says to use `127.0.0.1`), a zone, an IPv4-mapped IPv6 address, and port `0`. There is no override flag and no environment variable. The error is logged and the exit status is 1. The default and the systemd unit stay `127.0.0.1:8484`. After G5.1 a non-loopback address will need explicit configuration, a warning and enabled authentication (ORB-SEC-010).

## Requirement to test

Each MUST requirement in scope maps to at least one test (spec 12.4). `TestDashboardTraceabilityTableIsComplete` parses this table: every in-scope requirement must be listed, and every named Go test or frontend test must exist.

| Requirement | Tests |
|---|---|
| ORB-DASH-001 Home shows host name, board, SoC, OS, uptime, CPU, RAM, storage, network; real values, no page reload | `TestCollectPiZero2W`, `TestCPUUsageIsComputedFromDeltas`, `TestFastTierFollowsEverySample`, `TestMetricsEndpointPiZero2W`, `TestMetricsSchemaUsesNullsAndEmptyLists`, `TestEventStreamProtocol`, `TestCollectOnTheRealHost`, `Home.test.ts "shows host name, board, SoC, OS, uptime, CPU, memory, storage and network, and updates them live"`, `Home.test.ts "computes the transfer rates in the browser from the counters"` |
| ORB-DASH-002 available temperature sources, sensibly named; the SoC temperature on the Pi | `TestCollectPiZero2W`, `TestCollectGenericARM64`, `TestCollectMinimalSystem`, `TestCollectHostileFixture`, `Home.test.ts "labels every temperature source and shows an unreadable one as unavailable"`, `Home.test.ts "shows empty states for missing sensors, storage and interfaces"` |
| ORB-DASH-005 prioritised alerts with cause, impact and next step | `TestHealthySystemRaisesNoAlert`, `TestAlertThresholds`, `TestInformationalAlertsFollowTheSupportTier`, `TestCPUTemperatureRuleUsesTheCPUSensors`, `TestAlertOrderIsByLevelThenRule`, `TestEveryAlertHasCauseImpactAndNextStepAsKeys`, `TestAlertKeysAreUniqueAndComplete`, `TestThresholdConstants`, `TestAlertsVersionChangesOnlyWhenAlertsChange`, `TestEventStreamSendsAlertsWhenTheyChange`, `Home.test.ts "shows alerts first, grouped by level, each with cause, impact and next step"`, `Home.test.ts "words the swap alert as occupied swap, in German and in English"`, `Home.test.ts "does not alert for the Compatible tier; the status stays in the header"` |
| ORB-DASH-006 adaptive intervals, no permanent high-frequency collection | `TestNoSamplingWithoutClients`, `TestSamplerStartsAndStopsWithClients`, `TestNoReadsAfterTheLastClientLeaves`, `TestSlowTierIsReadOnlyWhenDue`, `TestClientCap`, `TestSlowClientDoesNotBlockAndKeepsTheNewestFrame`, `TestNoGoroutineLeakAfterDisconnect`, `TestConcurrentSubscribeAndClose`, `TestOnceWithoutClients`, `TestEventStreamClientCap`, `TestEventStreamDisconnectStopsSampling`, `TestStuckStreamClientIsDroppedByTheWriteDeadline`, `TestEventStreamOutlivesTheServerTimeouts`, `TestShutdownEndsStreams`, `TestCollectBoundsOversizedAndNumerousInputs`, `Home.test.ts "closes the stream when the tab is hidden and opens it when the tab is shown"` |
| ORB-NFR-009 minimal write load on the microSD | `TestHistoryRingIsBounded`, `TestDefaultHistorySize`, `TestNoSamplingWithoutClients`, `TestArchitectureMetricsSystemCallIsConfinedToStatfs`, `TestArchitectureMetricsParsersTouchNothing` |
| ORB-NFR-014 copes with an unstable WLAN connection | `TestStreamIsRefusedWhileShuttingDown`, `TestEventStreamClientCap`, `stream.test.ts "lets the browser retry a dropped connection and shows it as reconnecting"`, `stream.test.ts "reconnects by hand when the connection is refused, with a growing delay that resets after a success"`, `stream.test.ts "marks the values stale when an open stream delivers nothing, and recovers on the next value"`, `stream.test.ts "ignores a malformed event, reports it, and keeps working"`, `Home.test.ts "shows a lost connection, marks the values stale, and recovers when fresh data arrives"`, `Home.test.ts "ignores malformed events without crashing and keeps the last good values"`, `Home.test.ts "never lets an older reading replace a newer one"` |
| ORB-NFR-015 Generic Mode: core dashboard available, no board-specific claim | `TestCollectMinimalSystem`, `TestCollectGenericARM64`, `Home.test.ts "shows Generic Mode, unknown values, and no board-specific claims"` |
| ORB-NFR-010 core works without Internet | `TestMetricsEndpointHeaders`, `TestArchitectureMetricsDoesNotImportTheServerOrDetection`, `Home.test.ts "uses only native interactive elements, so everything works from the keyboard (ORB-NFR-011)"` |
| ORB-GEN-003 advanced options behind progressive disclosure | `Home.test.ts "keeps the technical details collapsed (progressive disclosure, ORB-GEN-003)"` |
| ORB-GEN-006 texts from translation resources | `TestEveryAPIValueHasATranslation`, `i18n.test.ts "English has exactly the keys of German"`, `i18n.test.ts "has the same placeholders in German and English"`, `i18n.test.ts "fills in parameters, formats numbers the German way, and keeps a missing parameter visible"` |
| ORB-ONB-003 loopback by default | `TestValidateListen`, `TestMainRefusesANonLoopbackListenAddress` |
| D-C29 the binary refuses a non-loopback listen address | `TestValidateListen`, `TestValidateListenErrorIsBounded`, `TestMainRefusesANonLoopbackListenAddress`, `TestListenGuardIgnoresTheEnvironment` |
| Parsers survive hostile input | `TestParseStat`, `TestParseStatCapsTheCoreCount`, `TestParseLoadavgAndUptime`, `TestParseMeminfo`, `TestParseMountinfo`, `TestParseMountinfoBoundsHostileInput`, `TestParseFibTrieLocal`, `TestParseFibTrieLocalIsBounded`, `TestParseRoutes`, `TestParseInet6`, `TestParsersSurviveHostileInput`, `FuzzParseStat`, `FuzzParseMeminfo`, `FuzzParseFibTrie`, `FuzzParseInet6`, `FuzzParseMountinfo`, `FuzzParseRoutes` |
| Privacy rules | `TestFixturesContainNoPrivateData`, `TestMetricsOpenOnlyAllowListedFiles`, `TestMetricsNeverReadsIdentifyingFiles`, `TestMetricsOutputContainsNoPrivateData`, `TestLinkLocalAndDerivedIPv6AreNeverShown`, `TestDisplayMountPointHidesUserNames`, `TestArchitectureMetricsNamesNoIdentifyingFile`, `TestDashboardDocsContainNoPrivateValues` |
| API is read-only | `TestMetricsEndpointsAreReadOnly`, `TestMetricsEndpointsWithoutASamplerAre503`, `TestMetricsEndpointHeaders` |
| Architecture: no program, no HTTP in metrics, hardware and OS stay separate | `TestArchitectureMetricsDoesNotImportTheServerOrDetection`, `TestArchitectureNoProductCodeRunsAProgram`, `TestArchitectureTestHelpersAreOnlyImportedByTests`, `TestArchitectureHardwareAndOperatingSystemAreSeparate` |
| ORB-NFR-011 keyboard and WCAG (SOLL) | `Home.test.ts "announces only the alert list, not the metric updates"`, `Home.test.ts "uses only native interactive elements, so everything works from the keyboard (ORB-NFR-011)"` |

Acceptance tests: AT-008 (real metrics), AT-021 (resource budget), AT-022 (microSD write load) and AT-025 (accessibility) need the device or a browser; see below.

## Verification status

**Verified by tests (no hardware, no network, no root):** everything in the table above, against synthetic fixtures (`backend/internal/metrics/testdata/`, see its README): a Raspberry Pi Zero 2 W style tree, a generic ARM64 tree, a tree without cpufreq, sensors, mounts or network, and hostile content (NUL bytes, escape sequences, invalid UTF-8, absurd and negative numbers; huge files, thousands of mounts, sensors and interfaces are generated in memory). The six parsers are fuzzed (5 seconds each in CI, `make fuzz`). `TestCollectOnTheRealHost` runs the collector on the real `/proc` and `/sys` of a Linux CI runner and asserts completeness and cleanliness only; it is skipped on macOS. The frontend is tested in jsdom with a fake `EventSource`.

**Verified on hardware (one device, once):** user-run on 2026-10-27 on a Raspberry Pi Zero 2 W Rev 1.0 (revision code 902120), Raspberry Pi OS (Debian 13 trixie, arm64), kernel `6.18.50+rpt-rpi-v8`, release `v0.1.0-alpha.4`, commit `5a9644edc551c0dc964ed9ab96e6fea1e51d80dc`, in-place upgrade from `v0.1.0-alpha.3` with the stamped installer, service unit unchanged. Method and numbers: [`INSTALL.md`](INSTALL.md#gate-g5-dashboard). This is one device, one image and one moment.

- **The service sandbox.** The unchanged unit allowed every file the dashboard reads (cpufreq, thermal, `/proc/net/*`, `statfs` on both mounts). No blocked read was seen.
- **AT-008.** Memory and swap (`free -b`), uptime, storage (`df -B1`), CPU temperature and current frequency matched the device's own tools (details in INSTALL.md).
- **ORB-NFR-001, ORB-NFR-003.** Idle with no browser, five minutes: 0 of 30000 CPU ticks, `VmRSS` 8480 kB.
- **ORB-NFR-002, one client only.** One Safari tab on Home, 60 s: 17 of 6000 ticks (0.28 percent of one core), `VmRSS` 12680 kB, `VmHWM` 12924 kB. With the tab hidden, 60 s: 0 of 6000 ticks; Safari closed the stream and the sampler was idle.
- **AT-022.** `write_bytes` in `/proc/<pid>/io` was 0 for the process, including the browser session.
- **ORB-NFR-007.** `vmstat 2 6` with the tab open: `si`, `so`, `bi`, `bo` 0, CPU idle 100 percent; 1.5 MiB parked in swap, no swap activity.
- **ORB-NFR-014, in part.** The service was restarted while the tab was open; the owner reported that the page behaved as expected. The lost-connection banner was not recorded separately.
- **Browser.** Home was viewed in Safari through an SSH tunnel, with live values in all cards and a transfer rate in the network card.

**Verified on hardware with the redesigned UI (one device, once):** user-run on 2026-10-28 on the same device model and revision code, same OS and kernel, release `v0.1.0-alpha.5`, commit `c86d15929ce0431b43a19dffe1c447844cac5451`, in-place upgrade from `v0.1.0-alpha.4` (dry run, installation, signature OK, SHA-256 OK, service healthy). Details: [`INSTALL.md`](INSTALL.md#ui-foundation-v010-alpha5) and [`UI.md`](UI.md#verification-status). As reported by the owner:

- **Browsers.** Home and Gerät were viewed in Safari (light and dark), Chrome and Firefox through an SSH tunnel; the layout looked as intended and the connection indicator read "Live".
- **Alerts (D-C44).** On the idle device the alert list was empty: 1.5 MiB of 415 MiB in swap raised no alert, and there was no alert for the Compatible tier.
- **Network card.** The tunnel interface with operstate `unknown` was shown in neutral grey, not red.
- **Keyboard-only use** worked in Chrome or Firefox (not named).

**Not verified:**

- **Screen-reader use.** AT-025 was checked by structure tests (native elements, one live region, named progress bars and images) and, for keyboard-only use, once by hand in Chrome or Firefox (see above); not with a screen reader, and not by keyboard in Safari.
- **The lost-connection display** (banner, "Veraltet" marking) was not recorded on the device, neither with the old nor with the new UI; for the old UI only the owner's statement above exists.
- **Idle CPU and `VmRSS` of `v0.1.0-alpha.5`.** Not re-measured; the readings above are from `v0.1.0-alpha.4`.
- **Four simultaneous clients.** The ORB-NFR-002 reading was taken with one client. The client cap and peak memory at four clients have not been measured on the device.
- **CPU usage percent and network counters** were not compared with a second tool.
- **Real kernels and real files.** The `fib_trie` and `route` formats were written from the kernel sources. One kernel (`6.18.50+rpt-rpi-v8`) was read under the unchanged unit with live values in all cards; other kernels or drivers that print something else give `null` or a missing address, not a crash.
- **Ubuntu, other boards, other Raspberry Pi models.** Fixtures only.
- **Long running.** No test or measurement runs for hours; the leak tests run many connect/disconnect cycles. The device readings above span minutes.
- **Deferred by specification or scope:** ORB-DASH-004 (throttling and undervoltage; needs a safe board adapter), ORB-DASH-007, ORB-NFR-017, authentication (G5.1).

### Checking a device (acceptance, by hand)

On the device, with the service running. The output of the first two commands contains the host name and IP addresses by design; look through it before sharing it, and do not paste it into a public issue.

```bash
curl -s http://127.0.0.1:8484/api/v1/metrics
curl -s -N -m 10 http://127.0.0.1:8484/api/v1/events | head -c 3000

# AT-008: compare with the device's own tools
free -b; uptime; df -B1 /; cat /sys/class/thermal/thermal_zone0/temp; cat /sys/devices/system/cpu/cpu0/cpufreq/scaling_cur_freq

# ORB-NFR-003, ORB-NFR-001: idle with no browser open. Ticks of CPU time over five minutes:
pid=$(systemctl show -p MainPID --value oarfish)
a=$(awk '{print $14+$15}' /proc/$pid/stat); sleep 300; b=$(awk '{print $14+$15}' /proc/$pid/stat)
echo "ticks: $((b-a)) of $((300*$(getconf CLK_TCK)))"       # expect almost 0
grep VmRSS /proc/$pid/status
# the same with one browser tab open on Home, then with the tab hidden
```

Expected with no browser: no CPU time beyond the process's baseline and the same RSS as without G5 (about 9 MB in the G4 reading). With a visible tab: a small, steady share of one core. Record device, revision, OS, kernel, commit and method with the numbers (spec 12.4).
