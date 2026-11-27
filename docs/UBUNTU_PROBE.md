# Ubuntu probe UBU0

The probe UBU0 (D-C101, D-C103) ran the published release on Ubuntu Server LTS ARM64 on the reference board and looked for every assumption that only holds on Raspberry Pi OS. No product code was changed. All results are class D (production key, production binaries; classes in [`UPDATE.md`](UPDATE.md#evidence-classes)), on **one device and one image**, on 2026-11-27.

## Device and image

- Raspberry Pi Zero 2 W Rev 1.0, a second SD card; the Raspberry Pi OS card was out of the device.
- Ubuntu Server 24.04.5 LTS (64-bit), the official preinstalled server image, with Wi-Fi, a user and an SSH key set by the imaging tool.
- Kernel `6.8.0-1064-raspi`, systemd 255 (`255.4-1ubuntu8.17`), cgroup v2, AppArmor enabled, OpenSSL 3.0.13.
- Network: netplan with systemd-networkd and `netplan-wpa-wlan0.service`; NetworkManager not active. `/etc/resolv.conf` is the systemd-resolved stub (`nameserver 127.0.0.53`).
- 409 MiB usable memory, **no swap and no zram**. unattended-upgrades enabled (security origins and ESM), `needrestart` installed, snapd present.
- Releases: `v0.1.0-alpha.14` installed with its own `install.sh`, then `v0.1.0-alpha.15` through the GUI.

## Checks

| # | Check | Result |
|---|---|---|
| 1 | Baseline: no failed units; first boot 4 min 31 s (cloud-init 3 min 15 s), later boots 41 s | works |
| 2 | `install.sh` of alpha.14: SHA-256 equals the signed `checksums.txt`, signature verified on another computer; dry run and installation without refusal, warning or missing tool; **no package installed** | works |
| 3 | Units active (service, helper socket, broker socket), recover unit enabled, no failed unit, `/healthz` ok, only loopback listens, component layout as on Raspberry Pi OS | works |
| 4 | `systemd-analyze verify` of the six units: no unknown directive under systemd 255; no sandbox error, no AppArmor denial for an Oarfish process | works |
| 5 | `systemd-analyze security`: service 1.7, helper 1.7, broker 0.9, recover 1.7 (equal to Raspberry Pi OS) | works |
| 6 | First-run setup, sign-in, Dashboard: memory, storage (`/`, `/boot/firmware`), temperature, CPU and `wlan0` agree with the device | works |
| 7 | Gerät: profile `raspberry-pi-zero-2-w` by device tree, OS adapter "Ubuntu (noch nicht qualifiziert)", Compatible with the limitation "OS adapter not qualified", all seven capabilities | works, as ADR 0007 states |
| 8 | Network card (alpha.15): IPv4 and IPv6 default route on `wlan0`, DNS `127.0.0.53 (lokal)` with the note that the servers behind it are not visible | works with note (U3) |
| 9 | Aktivität: "Die Dienste konnten nicht gelesen werden." on every load | **fails** (U1) |
| 10 | Restart of a harmless service from the page | not run: blocked by U1 |
| 11 | GUI update alpha.14 → alpha.15: only `oarfish` changed, components unchanged; helper phases staging to committed `ok` | works with note (U2) |
| 12 | Second confirmation and self-reload of the update page | not observed |
| 13 | Reboot from the GUI: broker `system.reboot accepted`, audit `system_reboot success`, device back in 41 s, units active | works |
| 14 | Memory: service VmHWM 32.2 MiB (MemoryPeak 26.8 MiB); helper and broker run only on demand, their peak was not measured | works, partly measured |

## Findings: assumptions that only hold on Raspberry Pi OS

**U1. Unit-file read path is too slow, and it freezes PID 1.** The services list runs `systemctl list-unit-files --type=service` with a 5 s deadline (`backend/internal/services/systemctl.go:30` and `:41`, called at `backend/internal/services/reader.go:171`). On this image the call takes 17.0 s every time (as the service user and as root), because PID 1 computes it (about 16.8 s of its own CPU, about 400 unit files in `/usr/lib/systemd/system`). During that time PID 1 answers no other request: `systemctl is-active` took 15.9 s instead of 0.06 s, and socket activation of the update helper was delayed by 17 s twice. Killing the client at the deadline does not stop the work in PID 1. Measured alternatives: the same call with the 166 unit names as patterns takes 5.3 s, `systemctl show -p UnitFileState` 3.2 s. The time on Raspberry Pi OS was not measured in this probe (G7 passed there within the deadline). **Proposal: fix now**, as a small task with an ADR 0011 amendment because the read shapes of R1 are fixed (D-C87): read the unit-file states at most once per change instead of once per page load (for example cached and invalidated after a broker action or a daemon-reload), and a test on this image.

**Fix (step UF1, D-C104):** the unit-file states have their own store and a 45 s deadline and are read at most once per 30 minutes at a list request; the list no longer waits for them or fails with them ([ADR 0011 A9.2](adr/0011-privilege-broker.md#a92-the-read-path-as-it-now-holds)). One read still occupies PID 1 once. Class C, not rechecked on Ubuntu.

**U2. `update_finished` came late.** After the restart into alpha.15 the first helper status read (timeout 10 s, `backend/internal/updater/helper.go:25`) fell into a 17 s freeze of PID 1 from U1. The entry `update_finished success` was written about five minutes later, at the next status read. Assumption: the cause is U1, not the update path. **Proposal:** no change of its own; recheck after the fix of U1.

**U3. Resolver stub.** The DNS row shows only `127.0.0.53 (lokal)` and says that the servers behind it are hidden, as ADR 0008 ("N0 as implemented") describes; this is the first observation of that sentence on a device. The upstream servers are in `/run/systemd/resolve/resolv.conf`, readable without privileges. **Proposal: N1** (network page): read that file as the fallback when `/etc/resolv.conf` lists only a loopback server, with a test fixture of this image.

**Fix (task N1, D-C104):** when every server of `/etc/resolv.conf` is a loopback address, the servers of `/run/systemd/resolve/resolv.conf` are shown as used through the resolver on the device ([ADR 0008 N1.3](adr/0008-dashboard-metrics.md#amendment-n1-and-st1-as-implemented)). Class C, not rechecked on Ubuntu.

**U4. Critical units of rule 11 name only Raspberry Pi OS units.** The lists in `backend/internal/services/classify.go:57` and `:80` and `backend/cmd/oarfish-broker/unit.go:120` name `dhcpcd`, `NetworkManager`, `networking` and `wpa_supplicant`. On Ubuntu the Wi-Fi link is held by `netplan-wpa-wlan0.service` (family `netplan-wpa-`), which is on no list, so the page would offer Stop without the network warning; stopping it cuts the device off. `systemd-networkd`, `systemd-resolved` and `systemd-timesyncd` are covered by the `systemd-` prefix of the deny list. Not observed on the page (U1 blocked the list); derived from the code and the unit names on the device. **Proposal: fix now** (small): add the family `netplan-wpa-` (a prefix, as the lists match families) to the network row and to restart-only in both lists, with tests.

**Fix (step UF1, D-C104):** the family `netplan-wpa-` is in the network row and restart only of both lists ([ADR 0011 A9.3](adr/0011-privilege-broker.md#a93-rule-11-with-the-family-netplan-wpa-)). Class C, not rechecked on Ubuntu.

**U5. No swap.** The Ubuntu image has neither a swap file nor zram; the page shows "Kein Swap eingerichtet" correctly. With 409 MiB and no swap, ORB-NFR-007 holds trivially, but the memory reserve is smaller than on Raspberry Pi OS. **Proposal: behind the MVP**; a note in [`INSTALL.md`](INSTALL.md) when Ubuntu is qualified.

**U6. OS updates (input for OSU1 in DB1).** Ubuntu installs security updates daily by itself (unattended-upgrades with the `-security` and ESM origins), `needrestart` is installed, 101 packages were pending, and the kernel package is `linux-raspi` with the firmware partition at `/boot/firmware`. Assumption, not checked in this probe: Raspberry Pi OS Lite applies no upgrades by itself by default. **Proposal: design in DB1:** OSU1 must show and respect updates that the OS applies by itself, a pending reboot (`/var/run/reboot-required` on Ubuntu; assumption: not written on Raspberry Pi OS) and a held apt lock.

**U7. Default user and sudo.** The user created by the imaging tool is in `sudo`, `adm` and `lxd` and needs a password for `sudo`. Oarfish does not depend on it; `INSTALL.md` already uses `sudo`. **No change.**

**No finding:** detection and support status (ADR 0007), the installer, the unit directives under systemd 255 (finding F-9 of the interim assessment does not apply to this version), AppArmor, netplan and systemd-networkd for the metrics (all from procfs and sysfs), `/boot/firmware`. `vcgencmd` is present on this image and `raspi-config` is absent; the core uses neither.

## Not verified

The services list, the restart of a service and its audit entry (U1); the second confirmation and the self-reload of the update page; the peak memory of the helper and the broker; Ubuntu on other boards; a fresh install of alpha.15; rollback and shutdown.
