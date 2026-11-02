# G6.1 device spike: findings

Step G6.1 of [ADR 0010](../adr/0010-update-helper.md) (update helper). The kit, the runbook and the blocks are in [`g6-1/`](g6-1/README.md). This document records what was measured and gives one verdict per assumption V1 to V13. It proposes amendments to ADR 0010; **none of them is applied**. No normative document was edited in this step.

**Evidence label.** Every result below is a *device observation with throwaway units, not the production path*. Each is one reading on one device, one image and one moment. Nothing here makes the Raspberry Pi Zero 2 W Verified: the support status stays **Compatible**. Block numbers (B05 and so on) are those of the runbook; `X1` to `X8` are the scenarios of the exploration block `v11y`.

## Refuted or surprising: read this first

| # | Result | Why it matters |
|---|---|---|
| 1 | **V11 refuted.** systemd 257 starts `OnFailure=` when the service ends with a failed exit status, even when `Restart=on-failure` restarts it right away. The deliberate exit code 75 of U15 rule 4 therefore starts the recover unit, which switches `active` back to `known-good` before the restarted helper runs. The new binary never runs. | The hand-over of U15 rule 4 steps 2 and 3 does not work with the unit text of U10. A text change of both units is needed (amendment A1). |
| 2 | **V13 refuted.** `systemctl restart` of the helper's own socket unit from inside the running service fails (`Socket service ... already active, refusing`) and leaves the socket **stopped**: the next connection gets `ENOENT`. `--no-block` does not help. | `socket_restart_pending` as designed in U15 rule 4 step 6 does not work (amendment A2). A restart from a separate unit started by `OnSuccess=` did work. |
| 3 | **V3 partly refuted.** An anonymous conditional request answered `304` **does count** against the 60-per-hour limit. | The ETag in U3 saves bandwidth, not quota (amendment A3). |
| 4 | **`MemoryMax=64M` is not enforced on the reference image.** The kernel command line has `cgroup_disable=memory`; the memory controller is absent. systemd shows `MemoryMax=67108864` and says nothing. | U10 and U12 call it a hard guard (amendment A4). Not in the V table. |
| 5 | **Enabling a linked unit records the resolved release path** in the wants link (`.../releases/R1/...`), not the path through `current`. | Retention (U5) will remove that directory. Measured: harmless live and at boot, but a relative wants link is cleaner (amendment A5). |
| 6 | **The stock start limit never ends a crash loop of `oarfish.service`.** 20 restarts in 45 s with `RestartSec=2`, no `start-limit-hit`. | The health criteria of U6 already rely on `NRestarts`, not on the start limit. Text only (amendment A6). |

V1, V2, V10 and V12 are confirmed. Details follow.

## Device and method

| Item | Value |
|---|---|
| Model | Raspberry Pi Zero 2 W Rev 1.0, revision code `902120` |
| OS | Debian GNU/Linux 13 (trixie), Raspberry Pi OS Lite arm64 |
| Kernel | `6.18.50+rpt-rpi-v8`, aarch64 |
| systemd | 257 (`257.13-1~deb13u1`), `+SECCOMP +BPF_FRAMEWORK +BTF` |
| Kernel features | `CONFIG_BPF`, `CONFIG_BPF_SYSCALL`, `CONFIG_CGROUP_BPF`, `CONFIG_SECCOMP_FILTER` = `y`; cgroup v2 (`cgroup2fs`); controllers `cpuset cpu io pids` |
| Root file system | ext4 `rw,noatime`, one file system for `/`, `/opt`, `/usr/libexec`, `/etc/systemd/system`, `/var/tmp` |
| Production service | `oarfish.service`, release `0.1.0-alpha.6`, running throughout |

No host name, user name, address, serial number or MAC address is recorded. Placeholders: `DEVICE`, `USER`, `ADDRESS`.

**How the evidence was collected.** The owner ran B01 to B15 and the exploration `v10x` on the device and pasted the output. After that the owner gave the assistant SSH access to this development device, and the assistant ran the postflight, the second run of `v13` (with the journal added), the explorations `v13 nb`, `v13 os` and `v11y`, the reboot, the post-reboot block and the cleanup. The kit changed between runs (kit revision `kit_script_sha256` in every report); the probe changed twice, once for the `v13 nb` exploration and once for `v13 os`. Blocks B13 to B15 ran with the first kit revision. The first run of `v11y` had a defective part 1 (a drop-in did not remove `Before=`), so the recover unit's `Before=oarfish-spike-h11.service` was removed from the template and the block was run again; only the second run is used for part 1. Two lines of the first revision were not ADR-faithful (that `Before=`) and are marked where they matter.

**Incidents that touch the evidence.**

- While the V3 logic of the driver was tested on the development machine, the script reset its own `PATH` and the test double of `curl` was not used. About 14 anonymous requests went from the development machine to the GitHub API. They are not used as evidence. The device's own window began at 60 remaining, so they did not disturb the measurement.
- The setup of the SSH access first failed to install the `sudo` rule because a cached `sudo` password was mistaken for "no password needed". After the reboot `sudo` asked for a password again; the post-reboot block was run after the access was repaired. The rule `/etc/sudoers.d/oarfish-spike-dev` is not part of the kit and stays on the device until the owner revokes it.

## Verdicts

| # | Assumption (short) | Verdict | Consequence for ADR 0010 |
|---|---|---|---|
| V1 | `systemctl restart`, `daemon-reload`, `reset-failed` work from uid 0 with an empty capability set in the helper sandbox | **Confirmed** | None |
| V2 | Socket activation, `SocketGroup=`, `DirectoryMode=`, `IPAddressDeny=any`/`IPAddressAllow=localhost` work | **Confirmed** | None (text note on the failure mode) |
| V3 | GitHub anonymous limit, `304` handling, redirect hosts, `latest` and prereleases | **Partly confirmed** (the `304` part refuted) | Text change (A3) |
| V4 | Go TLS client and resolver work under the service unit; root certificates present | **Partly confirmed**: Raspberry Pi OS confirmed; Ubuntu not verified, no Ubuntu device; G6.7 and G12 | None |
| V5 | `os.OpenRoot` | **Not in G6.1** (G6.5) | |
| V6 | journald persistent? stock start limit hit by a crash loop? | **Confirmed** (both answered: volatile; not hit) | Text change (A6) |
| V7 | ext4 and microSD durability, resource numbers | **Not in G6.1** (G6.9) | |
| V8 | No battery-backed clock | **Partly confirmed** (no RTC device; cold start not tested) | Text note (A8) |
| V9 | Release sizes | **Not in G6.1** (G6.2) | |
| V10 | Linked units from `/etc/systemd/system` into `/opt`, reload, boot, drop-in, helper link | **Confirmed** | Text change (A5); primary layout stays |
| V11 | Exit 75, `Restart=on-failure`, start limit, `OnFailure=`, `RuntimeMaxSec=` | **Refuted** | Design-level text change of the unit lines (A1) |
| V12 | `clock_gettime(CLOCK_BOOTTIME)` under `SystemCallFilter=@system-service` | **Confirmed** | None |
| V13 | Restarting the own socket as the last action | **Refuted** | Design change of one step (A2) |

## V1: systemctl from a uid-0 process with an empty capability set

Block B05, unit `oarfish-spike-v1-run.service` with the sandbox of U10 copied verbatim (`fragments/helper-sandbox.inc`; only `ReadWritePaths=/opt/oarfish-spike` and `StateDirectory=oarfish-spike-update` differ). **Confirmed.**

```text
Uid: 0 0 0 0     CapEff: 0000000000000000   CapBnd: 0000000000000000   NoNewPrivs: 1   Seccomp: 2   Threads: 5
$ systemctl daemon-reload                         exit=0 elapsed_ms=3252
$ systemctl restart oarfish-spike-target.service  exit=0 elapsed_ms=131
$ systemctl restart oarfish-spike-sock.socket     exit=0 elapsed_ms=69
$ systemctl reset-failed oarfish-spike-failing.service  exit=0 elapsed_ms=36
```

The effects were visible: the target's `MainPID` changed, the socket's `ActiveEnterTimestampMonotonic` changed, the failed unit became `inactive`/`Result=success`, and `NeedDaemonReload` went from `yes` to `no` with the new drop-in description loaded. The unit result was `success`. `daemon-reload` took 3.25 s on this device; later blocks show that this dominates the duration of any recovery run.

Additional observations from the same run (not an assumption of the ADR, but U3 and U10 rely on them): the probe, uid 0 without capabilities, could read a file owned by the service user with mode 0644 (`ok, 7 bytes`) and not one with mode 0600 (`EACCES`), and `chown` of its own file to another uid failed with `EPERM`. So there is no `CAP_DAC_OVERRIDE` and no `CAP_CHOWN`, and the cache modes of U3 are necessary and sufficient. On the device `/var/cache/oarfish` is `755` (the installer creates it with 0750; the unit's `CacheDirectory=` makes it 0755; U3 depends on 0755). The probe ran with 5 to 6 threads under `TasksMax=16`.

The exploration block `v1x` was not needed and not run. `CAP_SYS_ADMIN` was not tried.

## V2: socket activation, peer credentials, address filter

Block B07 (`v2`). **Confirmed.**

```text
Listen=/run/oarfish-spike-update/v2.sock (Stream)  SocketUser=root  SocketGroup=oarfish  SocketMode=0660  DirectoryMode=0755
drwxr-xr-x root root /run/oarfish-spike-update
srw-rw---- root oarfish /run/oarfish-spike-update/v2.sock
label=root           connect=ok   reply=[peer_uid=0 ... verdict=refused-by-uid-check]
label=service-user   connect=ok   reply=[peer_uid=999 peer_gid=985 ... verdict=accepted]
label=unrelated-user connect=FAIL kind=EACCES (permission denied)
```

Root passes the file mode and is refused by the `SO_PEERCRED` check, the service user passes both, an unrelated user (`nobody`) is stopped by the file mode. The first connection started the service by socket activation.

IP filter, unit with the full sandbox (`IPAddressAllow=::1/128 127.0.0.0/8`, `IPAddressDeny=0.0.0.0/0 ::/0`):

```text
tcp loopback v4: connected     tcp loopback v6: connected     udp loopback v4: delivered
tcp ADDRESS: connect FAIL timeout after 5015 ms
udp ADDRESS: write FAIL EPERM (operation not permitted)
control without sandbox: tcp ADDRESS: connected after 2 ms
```

A blocked TCP connect **times out** instead of failing at once; UDP fails with `EPERM`. Irrelevant for the helper (its only destination is loopback), but a bug that connects elsewhere would hang until the timeout.

Facts for the filter: systemd 257.13, `+BPF_FRAMEWORK`, kernel 6.18.50 with `CONFIG_CGROUP_BPF=y`, cgroup v2. The manager log contains one BPF line, unrelated to the filter: `bpf-restrict-fs: BPF LSM hook not enabled in the kernel`. U10 uses no `RestrictFileSystems=`.

## V3: GitHub (anonymous, from the device)

Block B08, 11 requests (cap 20), no credential. **Partly confirmed.**

```text
rate limit before: "limit":60,"remaining":60,"used":0
GET releases?per_page=5 -> 200     etag: W/"..."     x-ratelimit-remaining: 59
entries: v0.1.0-alpha.6 ... v0.1.0-alpha.2, all "prerelease":true, "draft":false
GET releases?per_page=5 with If-None-Match -> 304      x-ratelimit-remaining: 58   x-ratelimit-used: 2
GET /rate_limit (before, after list, after 304, after latest, final): free
GET /repos/.../releases/latest -> 404  {"message":"Not Found"}        remaining 57
GET github.com/.../releases/latest (web, no redirect followed) -> 302, target host github.com, not a tag page
asset chain (checksums.txt): hop 1 https://github.com -> 302;  hop 2 https://release-assets.githubusercontent.com -> 200, 441 bytes
cost of the 200 list request: 1   cost of the conditional request: 1   cost of /releases/latest: 1
```

- **Limit of 60 per hour per source address: confirmed** (`x-ratelimit-limit: 60`). `/rate_limit` itself is free.
- **`304` does not count: refuted.** The conditional request answered `304` and still consumed one request (`used` 1 to 2, `remaining` 59 to 58).
- **`latest` ignores prereleases: confirmed.** The API answers 404 while every release is a prerelease; the web address redirects to the host root, not to a tag page.
- **Redirect hosts:** a release asset is served by `github.com` (302) and then by `release-assets.githubusercontent.com` (200). That host matches the allow-list of U3 (`*.githubusercontent.com`). The signed query string of the second hop was not recorded.

## V4: Go DNS and TLS under the unchanged service sandbox

Block B09, unit with `User=oarfish`, `UMask` and the hardening block of `install/oarfish.service` verbatim (`fragments/service-sandbox.inc`; `CacheDirectory=oarfish-spike-cache` replaces the production directories). **Partly confirmed** (Raspberry Pi OS yes; Ubuntu: **not verified, no Ubuntu device; G6.7 and G12**).

```text
Uid: 999 999 999 999   CapEff: 0000000000000000   NoNewPrivs: 1   Seccomp: 2
CA bundle /etc/ssl/certs/ca-certificates.crt: exists, mode 644, 222073 bytes
resolv.conf: readable, 2 nameserver lines     nsswitch.conf: readable
lookup api.github.com: ok, 1 IPv4 and 0 IPv6 answers, 7 ms          (Go resolver, cgo off)
HEAD api.github.com/rate_limit: status 404 proto HTTP/2.0 total 403 ms (dns 5, connect 13, tls 368)
tls: TLS 1.3, cipher TLS_CHACHA20_POLY1305_SHA256, verified chains 2
cache directory (CacheDirectory=): write and chmod 0644: ok, mode now 644
create in /opt/oarfish-spike: EROFS     create in /etc: EROFS     create in /var/lib/oarfish-spike-update: EACCES
```

The `404` is the API's answer to `HEAD` on that path; the TLS and HTTP/2 exchange worked. The same probe without a sandbox behaved the same. Read-only facts (B03): `ca-certificates` `20250419+deb13u1` installed, `Status: install ok installed`; 297 entries in `/etc/ssl/certs`.

## V6: journal and start limit

Blocks B03 (read-only) and B10 (`v6`). **Confirmed** (both questions answered).

- **journald is volatile.** `/usr/lib/systemd/journald.conf.d/40-rpi-volatile-storage.conf` sets `Storage=volatile`. `/var/log/journal` exists but is a leftover (two boots are listed); the current journal is under `/run/log/journal`. The helper's journal lines and the service's logs do not survive a reboot. The persistent record is `state.json` and the service's audit entry, as the fallback of U8 says. The specification (8.2, journald row) speaks of structured logs with rotation; on this image they are not persistent.
- **The stock start limit does not end a crash loop of the production unit's restart settings.** Defaults `DefaultStartLimitIntervalUSec=10s`, `DefaultStartLimitBurst=5`; `oarfish.service` inherits them with `Restart=on-failure`, `RestartUSec=2s`. The throwaway unit with the same lines and a command that exits at once:

```text
t=5s  ActiveState=activating SubState=auto-restart NRestarts=2
t=15s NRestarts=7     t=30s NRestarts=13     t=45s NRestarts=20     started_lines=21  start_limit_lines=0
```

Five starts in 10 s are never reached with a restart every 2.2 s, so the unit stays in `auto-restart` for ever. Detection must use `NRestarts`, as U6 does.

## V8: clock

Blocks B03, B11 (`v8`), B17 (`reboot`) and B18 (`post-reboot`). **Partly confirmed.**

- No `/dev/rtc*`, no `/sys/class/rtc` entry, `RTCTimeUSec=` empty, `hwclock` not installed, `fake-hwclock` not installed (`not-found`). So the Zero 2 W has no battery-backed clock: confirmed.
- The clock is restored at boot by systemd, not by `fake-hwclock`: `systemd[1]: System time advanced to timestamp on /var/lib/systemd/timesync/clock` at 10.5 s of uptime. `systemd-timesyncd` is active and enabled.
- Boot record of the soft reboot, taken at 19.6 s of uptime: `realtime_utc=2026-11-02T21:58:30Z ... ntp_synchronized=no`. The wall clock at the first boot record was 18 s after the wall clock at the reboot command, so the clock kept going or was restored from the timestamp file; NTP was not yet synchronised. Later `NTPSynchronized=yes`.
- **Not tested:** a real power cut. After one, the clock would start from the timestamp file's value, which is the time of the last write. The conclusion for U6 (cooldowns and staged-release expiry use `CLOCK_BOOTTIME`, never the wall clock) is unchanged and is supported.

## V10: linked units, reload, boot, drop-in, helper through links

Blocks B12 (`v10`), B12x (`v10x`, exploration), B17 and B18. **Confirmed.** The primary layout of U5 and U15 rule 3 (Option L) works on this systemd; the copy layout (Option C) is not needed.

Chain `/etc/systemd/system/<unit> -> /opt/oarfish-spike/current/<unit>` with `current -> releases/R1` (a link to a link), as in the planned `oarfish.service`:

- **Loads and starts.** `LoadState=loaded`, `FragmentPath=/etc/systemd/system/oarfish-spike-v10a.service`, `systemd-analyze verify` exit 0 for both units.
- **Atomic switch picked up by `daemon-reload`.** Switch with a temporary link and `rename` (`mv -T`). Before the reload: `NeedDaemonReload=yes` and the loaded `Description=... release R1`. After `daemon-reload`: `NeedDaemonReload=no`, `Description=... release R2`, and a restart ran the R2 text (`release=R2` in the log). (systemd does notice the changed link target: `NeedDaemonReload` turns `yes` by itself.)
- **Drop-in on a linked unit works**, live (`Environment=OARFISH_SPIKE_DROPIN=yes`, `DropInPaths=/etc/systemd/system/oarfish-spike-v10a.service.d/dropin.conf`) and after the reboot.
- **Both ways of enabling work:**

```text
manual link + enable:  is-enabled before: linked (exit 1)   after systemctl enable: enabled
  Created symlink multi-user.target.wants/oarfish-spike-v10a.service -> /opt/oarfish-spike/releases/R1/oarfish-spike-v10a.service
systemctl link + enable: is-enabled before: linked (exit 1)  after: enabled
  Created symlink multi-user.target.wants/oarfish-spike-v10b.service -> /opt/oarfish-spike/releases/R1/oarfish-spike-v10b.service
```

  Note the target of the wants link: the **resolved release directory**, not `current`.

- **Wants link into a removed release directory (exploration `v10x`).** v10c was enabled with `systemctl enable` (wants link to `releasesx/RX/...`), v10d with a relative wants link `../oarfish-spike-v10d.service`. After the switch to RY and the removal of RX, as the retention of U5 would do: the v10c wants link dangled (`find -L` lists it as broken), yet `is-enabled=enabled`, `LoadState=loaded`, `list-dependencies multi-user.target` still listed it, `daemon-reload` and `verify` were clean.
- **At boot (soft reboot).** All four linked units, v10a to v10d, started at 19.6 s of uptime in the *switched* release (R2 and RY), including v10c with its dangling wants link, and the drop-in of v10a took effect (`dropin_env=yes`). `systemctl --failed` was empty.
- **Helper binary through `/usr/libexec/oarfish-spike-helper -> /opt/oarfish-spike/components/active/oarfish-spike-helper -> c1/...`** under `ProtectSystem=strict`, `ReadWritePaths=/opt/oarfish-spike`: started (`argv0 as started: /usr/libexec/oarfish-spike-helper`, resolved to `.../components/c1/...`), `CapEff: 0000000000000000`. Writes that must work did (create file below `components`, atomic link switch below `components`, file in the state directory). The negative test failed as required:

```text
create file in /usr/libexec: EROFS     create file in /etc/systemd/system: EROFS
replace the link /usr/libexec/oarfish-spike-helper (temporary link + rename): EROFS
replace the linked unit /etc/systemd/system/oarfish-spike-v10a.service (temporary link + rename): EROFS
create file in /etc/systemd/system/oarfish-spike-v10a.service.d: EROFS
```

- **`systemd-analyze security`** of the spike helper unit and of the helper-like unit used for V11: `Overall exposure level ... 1.7 OK`. Relaxed items: runs as root (0.4), `PrivateNetwork=` (0.5), `RestrictAddressFamilies` for `AF_INET`/`AF_INET6` (0.3) and `AF_UNIX` (0.1), `PrivateUsers=` (0.2), `@resources` and `@privileged` in the call list (0.2 each), `IPAddressDeny=` allow list (0.1). Informational, as in CI.

## V11: exit code 75, start limit, `OnFailure=`, `RuntimeMaxSec=`

Blocks B13 (`v11a`), B14 (`v11b`) and the exploration B14y (`v11y`, second run). Helper-like unit with `Restart=on-failure`, `RestartSec=1`, `StartLimitIntervalSec=10`, `StartLimitBurst=3`, `OnFailure=` and the sandbox of U10; `RuntimeMaxSec=10` instead of 900 (the only deliberate difference). **Refuted.**

**1. A deliberate exit 75 starts `OnFailure=` and the recovery undoes the hand-over.** Scenario S1 (and the same in S2 to S4, in X1 and X2 with the recover unit exactly as in U1):

```text
c-a1 mode=update75: start, switch active -> c-a2, exit75
systemd: Main process exited, code=exited, status=75/TEMPFAIL
systemd: Failed with result 'exit-code'.
systemd: Triggering OnFailure= dependencies.
t=exit+0.3 s   recover-start (known-good c-good): switch active back -> c-good, daemon-reload, reset-failed
t=exit+3.5 s   Scheduled restart job, restart counter is at 1   -> the restarted helper runs comp=c-good, not c-a2
```

The restart is delayed to about 3.5 s because the recover run (`daemon-reload` alone takes about 3.3 s on this device) holds the manager. In the first revision the recover unit carried `Before=oarfish-spike-h11.service`, which is not in U1; the second run without it (X1, X2) shows the same: recovery started 0.3 s after the exit and reverted `active` before `RestartSec=1` elapsed. The restart was not made safe by the ordering.

**2. Restarts after exit 75 do count against `StartLimitBurst`.** Measured with exit 75 declared a clean exit (exploration, see 5): one update 2 starts (X3), two updates in a row 3 starts, no limit (X4), **three updates in a row 4 starts within about 4 s: `Start request repeated too quickly`, `Failed with result 'start-limit-hit'`, `OnFailure=` runs** (X5), three updates with 4 s of work in each: 4 starts, no limit (X6). The window of 10 s with a burst of 3 only matters for restarts that follow each other almost at once.

**3. A new binary that exits at once (crash): `OnFailure=` runs at the first failed exit, not after a start-limit hit.** S5, S6, X7: `exit1`, then `Triggering OnFailure=`, recover (`switch=ok daemon_reload_rc=0 reset_failed_rc=0`), restart with `c-good`. U15's text "a restart loop ends at `StartLimitBurst=3` within 10 s, then `OnFailure=` runs" does not describe what happens: nothing loops, because the recovery acts after the first failure. The same recover unit was started by `OnFailure=` again in S6 and in every later scenario (many runs in the session): **a oneshot recover unit can be started repeatedly: confirmed**.

**4. A hanging binary is ended by `RuntimeMaxSec` and takes the same path.** S7, X8: `Service reached runtime time limit. Stopping.`, `Failed with result 'timeout'.`, `Triggering OnFailure= dependencies.`, recover, restart with `c-good`. The hang ended about 10.2 s after its start. (My expectation that a long `RuntimeMaxSec` would loop for ever without reaching `OnFailure=` was wrong; the unit fails, `OnFailure=` runs at once.)

**5. Exploration, not adopted: exit 75 as a clean exit that still forces the restart.** With `SuccessExitStatus=75` and `RestartForceExitStatus=75` added to the helper service (X3 to X6):

```text
X3: start c-a1 -> exit75 -> Scheduled restart job (restart counter 1) -> start c-a2 (mode ok) -> exit0.  recover_runs=0
X4: two updates in a row: starts=3 exit75=2 recover_runs=0, active ends on c-a3
X5: three in a row, no delay: starts=3 exit75=3 -> Start request repeated too quickly -> start-limit-hit -> OnFailure -> recover (active back on c-good)
X6: three updates, 4 s of work each: starts=4 exit75=3 recover_runs=0, active ends on c-a4
X7 (exit 1) and X8 (hang) still reach OnFailure= and the recovery.
```

The restart came about 1.3 to 1.6 s after the exit (RestartSec=1 plus start time) and started the new binary. No `OnFailure=` run happened for exit 75.

Throwaway recover units had no ordering or dependency on any production unit. Timing note: a recovery run (switch, `daemon-reload`, `reset-failed`) took 3.3 to 3.5 s on this device in every scenario.

## V12: `clock_gettime(CLOCK_BOOTTIME)` under the system call filter

Block B06 (`v12`). **Confirmed.**

```text
inside the full helper sandbox (SystemCallFilter=@system-service, SystemCallErrorNumber=EPERM, MemoryDenyWriteExecute=yes):
  clock_gettime(CLOCK_MONOTONIC) via syscall.Syscall: ok, 606658.022 s
  clock_gettime(CLOCK_BOOTTIME)  via syscall.Syscall: ok, 606658.022 s
  read /proc/uptime: FAIL ENOENT (no such file or directory)        <- ProcSubset=pid hides it, as U10 says
outside any sandbox: both ok; read /proc/uptime ok; CLOCK_BOOTTIME minus /proc/uptime: 0.007 s
```

No `EPERM`, no `SIGSYS`. The statically linked Go probe (go1.27.3) also started and ran under `MemoryDenyWriteExecute=yes`, `RestrictNamespaces=yes`, `TasksMax=16` and `NoNewPrivs`.

## V13: restart of the own socket unit as the last action

Blocks B15 (`v13`, run twice: once by the owner with the first kit revision, once with the journal added), the explorations `v13 nb` and `v13 os`. **Refuted.** Throwaway socket and service with the socket text of U10 (`Accept=no`, `RemoveOnStop=yes`, `SocketGroup=oarfish`); the service serves one connection (3 s of work), replies, then restarts its own socket while the accepted connection is still open.

```text
$ systemctl restart oarfish-spike-v13.socket        | Job failed. See "journalctl -xe" for details.   exit=1 elapsed_ms=47
journal: Stopping oarfish-spike-v13.socket ...
         oarfish-spike-v13.socket: Socket service oarfish-spike-v13.service already active, refusing.
         Failed to listen on oarfish-spike-v13.socket
state afterwards: socket ActiveState=inactive SubState=dead, service inactive, /run/oarfish-spike-update/v13.sock gone
client D (next connection): connect FAIL ENOENT
```

- The socket is stopped, closed, and then **cannot be started while the service that it triggers is still active**. The restart does not stop the service (`Triggers=` and `After=` only; `Requires=` of the service is `sysinit.target system.slice -.mount`).
- **Connections.** The accepted connection A was not affected: reply, then clean EOF. A connection waiting in the backlog (C) got `ECONNRESET` when the old socket closed. A connection made during the restart (E) got `ECONNRESET` or `ENOENT` depending on the instant.
- **Exploration `v13 nb`:** `systemctl restart --no-block` returned `exit=0` at once, with the same journal line and the same end state (socket stopped, `ENOENT` for D).
- **Exploration `v13 os`:** the service only exits; `OnSuccess=oarfish-spike-v13os-restart.service` (a oneshot with `ExecStart=/usr/bin/systemctl restart oarfish-spike-v13os.socket`, sandbox of U10) restarts the socket once the service is inactive. It worked: after the run the socket was `active (listening)` and client D was served by a new service start. When a connection was already queued (C), the service started again at once, the first restart attempt was refused (`already active, refusing`, the oneshot failed) and the next service exit triggered it again, which succeeded. A pending-flag must therefore be retried at each service exit until it succeeds.

## Additional observations

- **O1. Memory controller (not in the V table).** `cgroup.controllers` lists `cpuset cpu io pids`; the command line has `cgroup_disable=memory`. For the helper unit systemd reports `MemoryAccounting=yes`, `MemoryMax=67108864`, `TasksMax=16`, with no journal message. Whether the limit is enforced was not tested with an allocation; without the controller it cannot be.
- **O2. `daemon-reload` and recovery cost.** `daemon-reload` took 3.25 s; a recovery run 3.3 to 3.5 s. U12 has "a few seconds" for a component transaction; add that a reload alone is about 3 s on this device.
- **O3. Production unchanged.** B02 snapshot against B16, B18 and the cleanup: `is_active`, `ActiveState`, `SubState`, `UnitFileState`, `FragmentPath`, the SHA-256 of `/etc/systemd/system/oarfish.service` (`a68d5edd...f178`), `current -> releases/0.1.0-alpha.6`, the SHA-256 of the binary (`0deefc19...cd98`) and the listing of `/opt/oarfish` (54 lines) were unchanged at every comparison, also after the reboot. `MainPID`, `ActiveEnterTimestampMonotonic` and the boot marker differ after the reboot, as expected; `NRestarts` stayed 0. One line differed at the first postflight: the `..` entry of the recursive listing, which is `/opt` itself (creating `/opt/oarfish-spike` changes it). The driver now leaves that entry out; the contents of `/opt/oarfish` never changed.
- **O4. State of the device afterwards.** The cleanup removed every unit, link, directory and report with the prefix `oarfish-spike`. A search of `/etc /opt /usr /var /run` finds only `/etc/sudoers.d/oarfish-spike-dev` (the owner's SSH access, not part of the kit). The kit directory in the owner's home is still there. `systemctl --failed` is empty and `oarfish.service` is active.

## Proposed amendments to ADR 0010 (draft text, not applied)

These are for the owner to confirm, change or reject at the stop point of G6.1.

**A1. Exit code 75 and recovery (U10 unit text, U15 rule 4 steps 2 to 4 and failure handling, U15 rule 8, V11).** Add to `oarfish-update-helper.service`: `SuccessExitStatus=75` and `RestartForceExitStatus=75`, keep `Restart=on-failure`, `RestartSec=1`, `StartLimitIntervalSec=10`, `StartLimitBurst=3`, `OnFailure=`. Replace the sentence "A new helper that does not start ends in a restart loop; `StartLimitBurst=3` within 10 s stops it, `OnFailure=` runs the recovery" by: *"`OnFailure=` starts the recovery at the first failed exit of the helper (a start failure, a crash or `RuntimeMaxSec`), not after a start-limit hit. Exit code 75 is declared a clean exit that forces the restart, so a deliberate hand-over does not start the recovery. Every restart counts against `StartLimitBurst=3` per 10 s: a transaction may restart the helper at most twice within 10 s; a third restart in that window ends in `start-limit-hit` and the recovery. `daemon-reload` takes about 3 s on the reference device, a recovery run 3.5 s."* The lint canonical form (U15 rule 3) must contain the two new lines. The recover unit stays as in U1 (no ordering against the helper service); its run is not delayed by any `Before=` of the helper.

**A2. `socket_restart_pending` (U15 rule 4 step 6 and failure handling, U10 table row "RuntimeMaxSec/Restart/OnFailure", V13).** Replace "the helper runs `systemctl restart oarfish-update-helper.socket` as its last action; the next connection starts the helper again" by one of the two variants below; the owner chooses.

- *Variant (a), no restart of the socket:* *"The helper does not restart the socket. A changed socket unit text takes effect at the next boot or the next installer run; `status` reports `socket_restart_pending` until then."* Smallest change; nothing to add to the closed list. The cost: a changed socket text waits for a reboot.
- *Variant (b), a fifth unit:* *"A oneshot unit `oarfish-update-socket-restart.service` (U10 sandbox, `ExecStart=/usr/bin/systemctl restart oarfish-update-helper.socket`) is started by `OnSuccess=` of the helper service. If a connection is queued the first restart is refused and the next helper exit tries again; the helper clears `socket_restart_pending` only after it observed the socket restarted."* This adds a unit name and a replaceable component file to U2 and U15 rule 2 (the closed lists), a canonical form to the lint, and the `systemctl` shape is executed by systemd, not by the helper; `restart --no-block` is **not** a working alternative.
In both variants add: a connection waiting in the backlog is reset when the old socket closes, so the service must retry a refused or reset request once; an accepted connection is not affected.

**A3. Rate limit and ETag (U3, U12, V3).** Replace "answers with `304` to a matching ETag do not count" by *"a `304` counts like any other request (measured 2026-11-02, anonymous, one reading); the ETag saves bandwidth, not quota. The limit of 60 per hour and per source address is confirmed; `/rate_limit` is free."* Add to the redirect sentence: *"a release asset is served by `github.com` and then by `release-assets.githubusercontent.com` (measured); the allow-list `github.com` and `*.githubusercontent.com` covers both."* The request budget of U12 ("a handful of API requests a day") is unaffected.

**A4. `MemoryMax` (U10, U12, T13).** Replace "`MemoryMax=64M` as a hard guard" by *"`MemoryMax=64M` stays in the unit text, but it is not enforced where the memory cgroup controller is absent; Raspberry Pi OS starts with `cgroup_disable=memory` (observed). The memory peak of the helper is measured in G6.9 with `VmHWM`. `TasksMax=16` works through the `pids` controller."* The change to the kernel command line is not proposed (specification 13.3).

**A5. Enabling linked units (U5, U11 item 2, U15 rule 3, V10).** Add: *"`systemctl enable` and `systemctl link` followed by `enable` write a wants link that points to the resolved release directory (observed). The installer therefore creates the wants link itself as a relative link to the unit in `/etc/systemd/system` (`../oarfish.service`), which does not depend on a release directory. A dangling wants link did not stop the unit from loading or from starting at boot (observed), so retention is not blocked by it."* The primary layout (links from `/etc/systemd/system` into `/opt/oarfish`) is confirmed; the fallback Option C of U10 and U15 rule 3 stays unused.

**A6. Start limit and journal (U6, U8, V6).** Add to U6: *"The stock start limit (5 starts in 10 s) is not reached by `Restart=on-failure` with `RestartSec=2`: a crash loop of `oarfish.service` never reaches `start-limit-hit` (20 restarts in 45 s, measured). The health criteria therefore rely on `NRestarts`."* Replace the assumption in U8 by *"journald is volatile on Raspberry Pi OS Lite (`Storage=volatile`, observed): the helper's journal lines do not survive a reboot; the persistent record is `state.json` and the service's audit entry."*

**A7. Cache directory modes (U3, U10).** Add the fact: *"the installer creates `/var/cache/oarfish` with mode 0750; the unit's `CacheDirectory=` sets 0755 when the service starts (observed `755`). The helper reads the cache as uid 0 without `CAP_DAC_OVERRIDE`: a 0644 file is readable, a 0600 file is not (observed)."*

**A8. Clock (T15, U6, V8).** Add: *"The reference device has no RTC and no `fake-hwclock`; systemd restores the clock from `/var/lib/systemd/timesync/clock` at boot and `systemd-timesyncd` synchronises later (observed after a soft reboot; a cold start was not tested)."*

## Documentation observations (no document edited)

1. **ROADMAP wording.** The row G6.1 of `docs/ROADMAP.md` says the step "verifies the assumptions V1 to V13". The ADR's table "Assumptions to verify" gives V5 to G6.5, V7 to G6.9 and V9 to G6.2, and V4 "G6.1 and G6.7". The U14 row G6.1 has the same wording as the roadmap. Suggested wording: "V1 to V4 (Raspberry Pi OS part), V6, V8, V10 to V13 on the device; V5, V7 and V9 are checked in G6.5, G6.9 and G6.2".
2. **Threat T1 and `components_confirmed`.** T1 says "`components_confirmed` is an interlock, not a boundary" and that the attacker can trigger an update to a genuine newer release "without a click". U2 says the helper cannot see the click. That is correct but does not say who sets the flag. It does not say that the **fresh password of Q3 and both confirmations are checked only by the unprivileged service**, so a compromised service skips them. Proposed sentence for the residual risk of T1 (and a pointer in T7 and T17): *"`components_confirmed` is a flag set by the unprivileged service. The password confirmation and both dialogs are consent in the UI and protect against a stolen or left-open session, not against a compromised service: such a service can ask the helper to activate any genuinely signed, strictly newer release, including one that replaces the helper and its units, without a password and without the second confirmation."* This does not change the design (the helper still verifies everything), but it limits what U9 and U15 rule 7 promise.
3. **Specification 8.2 and the component tree.** Specification 8.2 (version 0.3.5) lists `/usr/libexec/oarfish-update-helper` as a path and `/run/oarfish/` for sockets; it has no component tree and no links. With V10 confirmed, the rows to add (conflict C10) are: `/opt/oarfish/components/<id>/` (helper binary and units), `/opt/oarfish/components/active` and `known-good` (links), `/usr/libexec/oarfish-update-helper` as a link into `components/active`, the unit links in `/etc/systemd/system`, and `/run/oarfish-update/` for the helper socket (the ADR uses it instead of `/run/oarfish/`). The journald row should say that the journal may be volatile (A6).
4. **U10 table text.** The sentence in U10 that a helper exit code 75 plus `Restart=on-failure` "is what makes ... work" and the row "`RuntimeMaxSec=900`, `Restart=on-failure`, `OnFailure=`" describe a mechanism that V11 refutes in this form; they change with A1.
5. **The `Before=` line of the ADR text.** U1 orders the recover unit before `oarfish-update-helper.socket` and `oarfish.service`. That is untested here (the spike has no such socket and no production ordering is allowed). It should be tested in G6.3 on the device.

## What was not verified

- **Ubuntu Server LTS ARM64**, any other board or image: V4 on Ubuntu "not verified, no Ubuntu device; G6.7 and G12". The whole spike is one device, one image, one moment per reading.
- V5 (`os.OpenRoot`), V7 (ext4 and card durability, memory, CPU and write numbers) and V9 (release sizes): not in G6.1.
- A **cold power cycle** for V8, and whether the memory limit is enforced anywhere (V8 and A4 are readings of configuration and logs).
- IPv6 name resolution (the lookup returned no IPv6 answer), a `HEAD` against a real release asset, and download timing.
- The order `Before=oarfish-update-helper.socket oarfish.service` of the real recover unit at boot, a socket-activated helper with `Restart=` and `OnFailure=` together (the V11 helper had no socket), and `WatchdogSec=`.
- The behaviour under `daemon-reload` while the production service is restarting, concurrent installer runs, and any behaviour over many days.
- The exploration results (`SuccessExitStatus`/`RestartForceExitStatus`, `OnSuccess=`, `--no-block`, the relative wants link) are measurements of throwaway units. None is adopted, and none has been tried with the real helper or real units.
