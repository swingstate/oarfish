# G6.1 device spike: runbook

Throwaway kit for step G6.1 of [ADR 0010](../../adr/0010-update-helper.md) (update helper). It measures the assumptions V1, V2, V3, V4 (Raspberry Pi OS part), V6, V8, V10, V11, V12 and V13 on the reference Raspberry Pi Zero 2 W with throwaway units. It is **not product code**: nothing here is installed by the installer, built by CI, or shipped in a release, and nothing here changes a normative document. The results go into [`../g6-1-findings.md`](../g6-1-findings.md) in a second step.

Everything the kit does is **device observation with throwaway units, not the production path**. It does not make the Pi Zero 2 W Verified; the support status stays Compatible.

Not in this step (the ADR table gives them to later steps): V5 (G6.5), V7 (G6.9), V9 (G6.2). V4 on Ubuntu cannot be checked here (no Ubuntu device; G6.7 and G12).

## Safety rules the kit follows

- The reference Pi also runs the production service (`oarfish.service`, alpha.6). The kit never touches, restarts, edits, orders itself against or depends on `oarfish.service`, `/opt/oarfish`, `/etc/oarfish` or `/var/lib/oarfish`. It only **reads** them, in the preflight, the postflight, the post-reboot block and the cleanup, to show that they did not change.
- Every artifact on the device carries the prefix `oarfish-spike`: units, `/opt/oarfish-spike`, `/var/lib/oarfish-spike-update`, `/run/oarfish-spike-update`, `/var/cache/oarfish-spike-cache`, `/usr/libexec/oarfish-spike-*`, drop-in directories and the report files in `/var/tmp`.
- Spike units have `TimeoutStartSec` of at most 30 seconds, no `Requires=` on anything, and no ordering against a production unit. Up to five units are enabled for the reboot (`oarfish-spike-v8-boot`, `oarfish-spike-v10a` to `oarfish-spike-v10d`); they run in parallel to the boot and cannot hold it up for more than their timeout.
- There is no block that runs a command given by the caller. The probe program has fixed subcommands; the only programs it starts are `/usr/bin/systemctl` and `/usr/bin/timedatectl` with fixed argument lists.
- The GitHub block (V3) is anonymous. No credential of any kind is sent or stored. At most 20 requests per run.
- The scripts filter host names, user names, IP addresses, MAC addresses, serial numbers, machine and boot ids and the paths and query strings of URLs out of their own output (placeholders `DEVICE`, `USER`, `ADDRESS`, `MAC`, `SERIAL`). Look at the output yourself before you paste it; the findings step scrubs it again.
- Signed redirect URLs are never printed or stored.

## What is in the kit

| File | Purpose |
|---|---|
| `spike.sh` | The driver, POSIX sh: `sudo sh spike.sh BLOCK`. Prints and stores one report per block. |
| `probe.go` | The single-file Go probe (standard library only, no `go.mod`, outside every module, not built by CI). |
| `build-probe.sh` | Builds the probe on the MacBook (static `linux/arm64`) and writes `probe.sha256`. |
| `probe.sha256` | Checksum of the build, checked by the first block on the device. |
| `fragments/helper-sandbox.inc` | The hardening text of the helper unit from ADR 0010 U10, verbatim, with two spike names. |
| `fragments/service-sandbox.inc` | The hardening block of `install/oarfish.service`, verbatim. |
| `units/` | Templates of the throwaway units; `spike.sh install` inserts the fragments. `units/v10/` holds the two releases of the linked units. |
| `check-kit.sh` | Local checks on the MacBook (syntax, shellcheck, vet, fragments against their sources, safety rules, forbidden identifiers). |

## On the MacBook

**M1 (MacBook, builds a file below `docs/spikes/g6-1/build/`, which git ignores).** Build the probe. It builds twice and stops if the two builds differ.

```sh
sh docs/spikes/g6-1/build-probe.sh
```

The build is static `linux/arm64`, made with `go version go1.27.3 darwin/arm64` (`-trimpath -buildvcs=false -ldflags='-s -w -buildid='`, `CGO_ENABLED=0`). The checksum is only reproducible with the same toolchain. Expected output line:

```text
sha256: 5d3faf7d4ba21433bc423680373f568b344aa57d1c2cc6b9b49b7032208fee12
```

**M2 (MacBook, copies the kit).** Copy the kit including the built probe to the Pi. `USER` and `DEVICE` are your login and the device's name or address, as in [`INSTALL.md`](../../INSTALL.md).

```sh
scp -r docs/spikes/g6-1 USER@DEVICE:oarfish-spike-kit
```

## On the Raspberry Pi

Every block is **one command**, run in your login on the Pi. The label tells what the block does to the device. Destructive and rebooting blocks are separate, and each block is safe to repeat unless it says otherwise. Paste the whole output of a block (from `=== OARFISH G6.1 SPIKE REPORT` to `report file:`).

| # | Label | Command | Answers | Time |
|---|---|---|---|---|
| B01 | READ-ONLY | `sudo sh ~/oarfish-spike-kit/spike.sh kitcheck` | Probe checksum matches, tools present, scrub filter works | seconds |
| B02 | READ-ONLY | `sudo sh ~/oarfish-spike-kit/spike.sh preflight` | Device facts; production service snapshot **before** (stored for the comparison) | seconds |
| B03 | READ-ONLY | `sudo sh ~/oarfish-spike-kit/spike.sh facts` | Read-only parts of V2, V4, V6, V8: systemd and kernel facts, certificates, journal, clock | seconds |
| B04 | CHANGES THE DEVICE | `sudo sh ~/oarfish-spike-kit/spike.sh install` | Creates `/opt/oarfish-spike`, the state directory and the throwaway units; enables and starts nothing | seconds |
| B05 | CHANGES THE DEVICE | `sudo sh ~/oarfish-spike-kit/spike.sh v1` | **V1**: `daemon-reload`, `restart` of a service, `restart` of a socket, `reset-failed`, from uid 0 with an empty capability set in the U10 sandbox | 20 s |
| B06 | CHANGES THE DEVICE | `sudo sh ~/oarfish-spike-kit/spike.sh v12` | **V12**: `clock_gettime(CLOCK_BOOTTIME)` under `SystemCallFilter=@system-service`, and outside | seconds |
| B07 | CHANGES THE DEVICE | `sudo sh ~/oarfish-spike-kit/spike.sh v2 ADDRESS:PORT` | **V2**: socket activation, `SO_PEERCRED`, who is accepted; `IPAddressDeny=any` / `IPAddressAllow=localhost` | 40 s |
| B08 | READ-ONLY (network; writes only its report) | `sudo sh ~/oarfish-spike-kit/spike.sh v3` | **V3**: GitHub anonymous API limit, ETag and 304, `latest`, redirect chain of one asset | 20 s |
| B09 | CHANGES THE DEVICE | `sudo sh ~/oarfish-spike-kit/spike.sh v4` | **V4**: Go DNS and HTTPS under the sandbox of `install/oarfish.service` | 20 s |
| B10 | CHANGES THE DEVICE | `sudo sh ~/oarfish-spike-kit/spike.sh v6` | **V6**: crash loop with the restart settings of `oarfish.service`, 45 s | 1 min |
| B11 | CHANGES THE DEVICE | `sudo sh ~/oarfish-spike-kit/spike.sh v8` | **V8**: enables the boot clock recorder, one baseline record | seconds |
| B12 | CHANGES THE DEVICE | `sudo sh ~/oarfish-spike-kit/spike.sh v10` | **V10**: linked unit layout, both ways of enabling, drop-in, atomic switch of `current`, helper through links, `systemd-analyze verify` and `security` | 1 min |
| B13 | CHANGES THE DEVICE | `sudo sh ~/oarfish-spike-kit/spike.sh v11a` | **V11**: exit code 75, start limit, `OnFailure=` in sequences of one, two, three updates | 2 min |
| B14 | CHANGES THE DEVICE | `sudo sh ~/oarfish-spike-kit/spike.sh v11b` | **V11**: crash loop, second `OnFailure=` run, hang and `RuntimeMaxSec` | 3 min |
| B15 | CHANGES THE DEVICE | `sudo sh ~/oarfish-spike-kit/spike.sh v13` | **V13**: the service restarts its own socket as its last action | 40 s |
| B16 | READ-ONLY | `sudo sh ~/oarfish-spike-kit/spike.sh postflight` | Production service unchanged, compared with B02 | seconds |
| B17 | REBOOTS | `sudo sh ~/oarfish-spike-kit/spike.sh reboot` | Writes a clock marker, then reboots the Pi. A soft reboot is enough | about 1 min |
| B18 | READ-ONLY | `sudo sh ~/oarfish-spike-kit/spike.sh post-reboot` | **V10** linked units started at boot; **V8** clock at boot; production again compared with B02 | seconds |
| B19 | READ-ONLY | `sudo sh ~/oarfish-spike-kit/spike.sh cleanup-dry` | Dry run: prints what the cleanup would stop and remove | seconds |
| B20 | DELETES | `sudo sh ~/oarfish-spike-kit/spike.sh cleanup` | Stops the spike units and removes everything that carries the prefix `oarfish-spike`; compares production once more | seconds |

Only if needed, never as part of the normal run:

| Block | Label | Command | When |
|---|---|---|---|
| B05x | CHANGES THE DEVICE | `sudo sh ~/oarfish-spike-kit/spike.sh v1x` | Only if B05 shows that a call failed. **Exploration, not a recommendation, nothing is adopted:** relaxes one sandbox line at a time in a drop-in (never `CapabilityBoundingSet=`, never a capability) and prints the exit codes of the four calls. |
| B12x | CHANGES THE DEVICE | `sudo sh ~/oarfish-spike-kit/spike.sh v10x` | Added after the V10 result of B12. **Exploration, not adopted:** two more linked units (enabled with `systemctl enable`, and with a relative link in the wants directory); the release directory that the wants link points to is removed, as the retention of ADR 0010 U5 would do. Enables two more units for the reboot; run it before B16/B17. Also reads the memory controller state. |
| B14x | CHANGES THE DEVICE | `sudo sh ~/oarfish-spike-kit/spike.sh v11x` | Only after the owner has seen the hang result of B14. **Exploration, not adopted:** the hang scenario with `StartLimitIntervalSec=120`. |

### Stop points while running

Paste the output and wait for a reply at these points. If the result of V1, V10 or V11 is refuted or surprising it is reported to you at once, and the blocks that depend on it wait for your decision.

1. After **B05** (V1). B13, B14 and B15 call `systemctl` from inside the sandbox and depend on it.
2. After **B12** (V10). It decides the unit layout; B17 reboots with its units enabled.
3. After **B14** (V11).
4. Before **B20**: paste everything first. The cleanup deletes the reports in `/var/tmp`.

B16 should be run right before B17, and B18 right after the Pi is back.

### If the Pi does not return after B17

This is not expected: the enabled spike units are oneshot units with a timeout of at most 30 seconds, ordered against nothing. If it happens anyway, remove the enablement links from the SD card on another machine (a Linux machine, because the root partition is ext4). Power the Pi off, put the card in, mount the larger (root) partition at `ROOT`, then:

```sh
sudo rm -f ROOT/etc/systemd/system/multi-user.target.wants/oarfish-spike-*
sudo rm -rf ROOT/etc/systemd/system/oarfish-spike-*
```

Unmount, put the card back, boot. The remaining spike files can be removed with B20 afterwards. Do not remove anything that does not carry the prefix.

### Notes on single blocks

- **B07** needs the address of another machine on your network and a TCP port it answers on (for example your router and port 80, 443 or 22). The block only opens a TCP connection and sends one UDP datagram of one byte; it does not send anything else and does not use a public address. The address is read at run time from the command line, written to a root-only file for the duration of the block and removed again; it is not part of any repository file and is replaced by `ADDRESS` in the report.
- **B08** counts against the anonymous GitHub limit of the Pi's address (60 per hour, shared with every device behind it): at most 20 requests per run, normally 15. Do not repeat it within the hour.
- **B10** deliberately starts a crash loop of a throwaway unit for 45 seconds and stops it again.
- **B13 and B14** start and stop throwaway units in rapid succession and let `OnFailure=` start the throwaway recover unit; the logs are in `/var/lib/oarfish-spike-update`.
- **B11, B12, B12x** enable throwaway units for the reboot. B20 removes them.

## What the kit has and has not been checked with

Checked on the MacBook by `check-kit.sh` (run it with `sh docs/spikes/g6-1/check-kit.sh`): shell syntax with `sh` and `dash`, `shellcheck` at every severity, `gofmt`, `go vet` for `linux/arm64` and `linux/amd64`, the cross-compile, a local run of the probe's selftest with the Linux-only calls stubbed, the fragments against ADR 0010 U10 and `install/oarfish.service`, the safety rules above (prefix, no dependency on production, timeouts, production names only in read-only commands) and the absence of host, user, address, MAC and serial values in the kit. The report filter was exercised on the MacBook with made-up values.

**Not checked: no block has run against a real systemd.** There is no Linux or systemd runtime on the development machine. The first run of every block is on the Pi. If a block fails for a reason of the kit itself (a typo, a missing tool), paste the output; that is a defect of the kit and not a finding about the ADR.

## After the run

Paste the outputs of the blocks, in order. They are scrubbed again and written into [`../g6-1-findings.md`](../g6-1-findings.md) with one verdict per assumption, the evidence and the consequence for ADR 0010, together with proposed amendments. Nothing in the ADR, the specification or the roadmap is edited in this step.
