# ADR 0008: Dashboard metrics from files, streamed with Server-Sent Events, sampled only while watched

- Status: **PROPOSED** (not accepted; it becomes Accepted only by a decision of the project owner)
- Date: 2026-10-27
- Gate: G5

## Context

Gate G5 adds the Home page with live values of the device (spec 6.3, 7.2, 8.7). The device is a Raspberry Pi Zero 2 W with 512 MB of RAM, an SD card and a service sandbox without `AF_NETLINK` (unit file, ADR 0006). The idle budgets are strict: at most 35 MB RSS, under 1 % of one core in a five-minute average, and no high-frequency writes to the SD card (ORB-NFR-001, -003, -009). ADR 0007 and decision D-C19 already settled that detection reads files and runs no program. The specification does not say how metrics are collected, delivered or bounded. This ADR proposes the answers; each numbered decision is also listed as a `D-DRAFT` item in the pull request. None of it is in `docs/DECISIONS.md` yet.

## Proposed decisions

**1. Metrics are read from files; no program runs (extends D-C19).** CPU time from `/proc/stat`, load, uptime, memory and swap from procfs, the host name from `/proc/sys/kernel/hostname`, frequency and governor from `cpufreq` in sysfs, temperatures from `thermal_zone*` and `hwmon*`, mounts from `/proc/self/mountinfo`, network state and counters from `/sys/class/net/<if>/`, addresses from `/proc/net/fib_trie` and `/proc/net/if_inet6`. All reads go through the read-only, size-limited file source of ADR 0007. The one system call is `statfs(2)` (`syscall.Statfs`, no cgo) for the size of a mounted file system; it needs no privilege and runs no program. The file allow-list is enforced by a test. Why: it is the same guarantee as for detection, it works under the unchanged unit, and it is testable with fixture trees.

**2. No netlink.** `net.Interfaces` and `net.InterfaceAddrs` use netlink, which the unit does not allow (`RestrictAddressFamilies` has no `AF_NETLINK`). Network data therefore comes from sysfs and procfs only. Cost: `/proc/net/fib_trie` lists IPv4 addresses without their interface. To assign them, the collector reads `/proc/net/route` as well (the directly connected networks per interface) and gives an address to the interface whose network contains it. An address without such a route is not shown. This reads one file more than the task text names; it is the only file-based way to get the assignment.

**3. Server-Sent Events, not polling.** `GET /api/v1/events` pushes one `metrics` event about every two seconds, an `alerts` event when the list changes, a `history` event once at the start and a heartbeat comment every 15 seconds. `GET /api/v1/metrics` returns one snapshot for the first paint. Why: one open connection is cheaper for a Pi than a request every two seconds (headers, routing, JSON per request), and the browser reconnects by itself. Cost: a stream holds a connection and a goroutine; see decision 6.

**4. Sampling only while somebody watches.** The sampler has no goroutine, no timer and no file read until the first stream client connects, and it stops when the last one leaves. While watched it samples every 2 seconds; storage, network addresses, link speed, the sensor list and the governor are read every 30 seconds. The browser closes the stream when its tab is hidden. Why: ORB-DASH-006 asks for adaptive intervals and no permanent collection; an unwatched dashboard costs nothing.

**5. History is a ring in memory only.** 60 points per series (CPU usage, memory use, CPU temperature), that is two minutes at the default interval, about 3 KB. It is cleared when the last client leaves, so it never shows stale data and nothing is written to the SD card (ORB-NFR-009).

**6. Everything is bounded.** At most 4 stream clients (the fifth gets a typed `503`), 16 sensors, 8 mounts (after de-duplication, from at most 64 candidate devices), 8 interfaces, 4 addresses per interface and family, every file read limited in size, every directory listing limited to 512 entries. A client that does not read is dropped by a per-write deadline of 10 seconds. The sampler never waits for a client: each client holds one frame and the newest replaces the unread one.

**7. The stream and the server timeouts.** `http.Server.WriteTimeout` covers a whole response and would end every stream after 30 seconds. The stream handler sets its own write deadline (`http.ResponseController`) before each write instead, which replaces the server's. `ReadTimeout` needs no change (verified by a test with 200 ms timeouts). On shutdown the handler closes all streams; without that, a graceful `Shutdown` would wait for connected browsers until its timeout. Why per-write deadlines rather than no timeout: a stalled client must free its goroutine and its slot.

**8. Alert rules are code with named thresholds.** A small rule engine with three levels. Alerts carry a stable id, a level and cause, impact and next step as translation keys with parameters; Go builds no user text. Thresholds are constants in one table (docs/DASHBOARD.md) and are not configurable in G5. Table: CPU temperature 70 / 80 °C; memory available below 15 / 8 %; swap in use from 1 MiB; a real file system above 85 / 95 %; no link on any interface; one-minute load above the number of cores; informational for Generic Mode and Compatible. The 1 MiB for swap is a choice of this ADR: the task text says "swap in use", and a few KiB that the kernel parks there are normal.

**9. Privacy by construction.** The host name and the IP addresses are shown on the live page and in the live API only. The hardware address of an interface (`/sys/class/net/<if>/address`) is never read. IPv6 link-local addresses and IPv6 addresses built from the hardware address (EUI-64) are not shown, because both contain the MAC address. Mount points under `/home/<user>`, `/media/<user>` and `/run/media/<user>` are shortened so that no user name appears. Machine id, serial number, environment, process list, user databases and SSH data are never read. A test lists every file the collector may open.

**10. The listen guard (D-C29).** The binary refuses to start unless `-listen` is a literal loopback IP address with a port. No override. This stays until Gate G5.1.

**11. Frontend.** Svelte 5, no chart library, no new dependency. Sparklines are inline SVG. Transfer rates are computed in the browser from the counters. Only the alert list is an `aria-live` region. The old "Status" view and its `/healthz` check are replaced by Home, whose connection state tells the same thing and more.

## Consequences

- A request to `/api/v1/metrics` without a stream reads the device twice, 250 ms apart, so that CPU usage exists; the answer is reused for one second.
- The first sample of a stream is published 0.5 seconds after the first client connects, so that CPU usage is not empty.
- More than four browser windows on Home are refused until one closes. Browsers also limit connections per host; the cap keeps the device from being the one that gives.
- The collector reads about 15 small files per sample. The benchmark in `internal/metrics` measures the work, not the time on the device; idle CPU and RSS on the Pi are still to be measured (docs/DASHBOARD.md, "Verification status").
- Not decided here: throttling and undervoltage warnings (ORB-DASH-004, needs a safe board adapter), the plugin and application summary (ORB-DASH-007), the resource view of external daemons (ORB-NFR-017), persisted history, configurable thresholds, authentication of the API (Gate G5.1).
