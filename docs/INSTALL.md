# Installing Oarfish

Oarfish runs on a 64-bit ARM Linux device with systemd. You download one script, read it, and run it. The script downloads the release from GitHub, checks the signature, and installs Oarfish as a service. No GitHub account, no token, no Docker, no Node.js, no Python.

**Status (Gates G3, G4 and G5):** this installer, the device detection built into the service and the dashboard have run on **one** real Raspberry Pi Zero 2 W at a time (see [Hardware test results](#hardware-test-results)); neither has been run on Ubuntu Server or on any other board. What was tested, and what was not, is listed at the end ([What is verified](#what-is-verified-and-what-is-not)). Oarfish today shows Home (live values, alerts) and Gerät, a read-only page about what it detected about the board and the system; there is no login and no device management yet (later gates).

**Kurzfassung (Deutsch):** `install.sh` von der Release-Seite laden, lesen, mit `sudo sh install.sh --dry-run` prüfen, dann mit `sudo sh install.sh` installieren. Die Oberfläche läuft nur auf `127.0.0.1:8484` (nur lokal). Von einem anderen Rechner erreichst du sie über einen SSH-Tunnel: `ssh -L 8484:127.0.0.1:8484 BENUTZER@GERAET`, dann im Browser <http://127.0.0.1:8484> öffnen. Deinstallieren: `sudo sh install.sh --uninstall` (Daten bleiben), mit `--purge` werden auch Daten und Benutzer gelöscht.

## What you need

| Need | Why |
|---|---|
| 64-bit ARM Linux (`uname -m` shows `aarch64`) | The release is built for ARM64 only. 32-bit systems are refused. |
| systemd as the running init system | Oarfish is installed as a systemd service. |
| `openssl` **3.0 or newer** | Checks the release signature. OpenSSL 1.x and LibreSSL are refused. Check with `openssl version`. |
| `curl`, `tar`, `gzip`, `sha256sum` and the usual base tools | The installer lists any that are missing and stops. |
| `useradd` or `adduser` | Creates the service user. |
| About 64 MB free on `/opt` and in `/tmp` | The release is about 3 MB; the installer wants room to spare. |
| root (use `sudo`) | The installer creates a user, directories and the service. |

Hardware and operating system are not checked against a list. Any ARM64 Linux with systemd and OpenSSL 3 is accepted ("generic mode"). The installer prints what it finds in `/etc/os-release`. It does not need Raspberry Pi tools, and it does not assume a user called `pi`.

Data point, not proof: OpenSSL 3.5.7 was present on one freshly flashed and updated Raspberry Pi Zero 2 W (image reported as Raspberry Pi OS Lite 64-bit) (see [Hardware test results](#hardware-test-results)). Nobody has confirmed it for an Ubuntu Server image or for other versions of either system. If `openssl version` shows 1.x or the command is missing, install or upgrade the `openssl` package of your distribution first.

## Install

Prereleases are installed from their own tag. This example uses `v0.1.0-alpha.2`; use the tag you want.

```bash
# 1. Download the installer of that release (HTTPS, no login).
curl -fsSLO https://github.com/swingstate/oarfish/releases/download/v0.1.0-alpha.2/install.sh

# 2. Read it. It is a plain shell script of about 600 lines.
less install.sh

# 3. See what it would do. Changes nothing, downloads nothing.
sudo sh install.sh --dry-run

# 4. Install.
sudo sh install.sh
```

Do not pipe the download into a shell (`curl ... | sudo sh`). The two-step way lets you read what you are about to run as root.

The installer you download from a release belongs to that release and installs exactly that version. To install another version, pass it: `sudo sh install.sh --version v0.1.0-alpha.3`. A copy of `install.sh` taken from the source repository is not tied to a release; it stops and asks for `--version`.

Releases before `v0.1.0-alpha.2` (that is, `v0.1.0-alpha.1`) have no installer and a different bundle layout; this installer refuses them.

For later stable releases the address `https://github.com/swingstate/oarfish/releases/latest/download/install.sh` is expected to work (GitHub's "latest" skips prereleases). That is **expected, to be verified with the first stable release**; no stable release exists yet, so do not rely on it.

### What the installer does, in order

1. Checks the system (Linux, ARM64, systemd, tools, OpenSSL 3, free space). Anything wrong: it stops, with nothing changed.
2. Downloads `checksums.txt` and `checksums.txt.sig` from the release.
3. **Verifies the signature** with the release key built into the script. First it proves that its `openssl` really rejects a modified file (a check on the checker); then it checks the real signature. A bad signature stops everything.
4. Downloads `manifest.json` and the bundle and compares their SHA-256 with the signed `checksums.txt`. Checks that the bundle holds only the expected files and that the program is an ARM64 executable.
5. Only now it changes the system: creates the user `oarfish` (no login shell), the directories, installs the release, switches the `current` link, installs and starts the service.
6. Calls `http://127.0.0.1:8484/healthz`. If the new service does not become healthy and an older version was installed, it switches back to the older version.
7. Prints the version and the address.

Nothing is sent anywhere except the download requests to `github.com`. The script has no telemetry.

### The release key

The installer trusts exactly one signing key:

```
key id:  11F1C52777090BFC
key:     RWT8Cwl3J8XxETRSvWsyNWIiVFVSOYnaL9IiJk0ylKyklmYUsRZ+1w6p
```

The installer prints this key id when it starts. Compare it with the file `release/oarfish-release.pub` of the repository at the tag you install:

```bash
curl -fsSL https://raw.githubusercontent.com/swingstate/oarfish/v0.1.0-alpha.2/release/oarfish-release.pub
```

The second line of that file must be the key above. The first line contains the key id. You can also compute the id from the key line yourself; minisign prints the eight bytes in reverse order, so read them from the right:

```bash
sed -n 2p oarfish-release.pub | base64 -d | od -An -tx1 -j2 -N8
#  fc 0b 09 77 27 c5 f1 11     ->  11 F1 C5 27 77 09 0B FC
```

What this does and does not give you: the key is inside the script you downloaded, and the script is from the same GitHub repository as the key file, so comparing them only shows that the two copies agree. It does not protect against someone who controls the repository and replaces both. For that, compare the key id with a copy you got somewhere else, for example from a person you trust or from another machine. No second official channel exists yet.

### Verify `install.sh` itself (optional)

`install.sh` is listed in the signed `checksums.txt`, so you can check that the script you downloaded is the one the release signed, before you run it. This needs `minisign`, which is not installed by default (`apt install minisign` on current Debian and Ubuntu, `brew install minisign` on macOS); you do it on any computer, not necessarily on the device.

```bash
TAG=v0.1.0-alpha.2
base=https://github.com/swingstate/oarfish/releases/download/$TAG
mkdir verify && cd verify
curl -fsSLO $base/install.sh
curl -fsSLO $base/checksums.txt
curl -fsSLO $base/checksums.txt.sig
curl -fsSL -o oarfish-release.pub https://raw.githubusercontent.com/swingstate/oarfish/$TAG/release/oarfish-release.pub

# 1. The signature of checksums.txt, made with the release key.
minisign -Vm checksums.txt -x checksums.txt.sig -p oarfish-release.pub
# 2. The SHA-256 of install.sh must be the one listed in checksums.txt.
grep ' install.sh$' checksums.txt                 # the signed value
sha256sum install.sh                              # yours (macOS: shasum -a 256 install.sh); the two must be equal
grep ' install.sh$' checksums.txt | sha256sum -c -   # or let sha256sum compare (macOS: shasum -a 256 -c -)
```

Step 1 must print `Signature and comment signature verified`. Do step 2 only after step 1 succeeded, because the hashes in `checksums.txt` are only worth something once its signature is checked. This proves the script is the signed one; it does not remove the need to compare the key (the public key file comes from the same repository, see above). The installer itself repeats the signature check for everything it downloads.

## First start and opening the GUI

The service starts by itself after the installation and after every reboot (the reboot was tested on one device, see [Hardware test results](#hardware-test-results)).

```bash
systemctl status oarfish          # should say "active (running)"
journalctl -u oarfish -e          # the log
curl http://127.0.0.1:8484/healthz   # {"status":"ok"}
```

Oarfish listens on `127.0.0.1:8484` only. That means: only programs on the device itself can reach it. This is on purpose. There is no login yet, so it must not be reachable from the network, and the installer never opens it.

Until the login exists (Gate G5.1), the program also enforces this: it refuses to start when `-listen` is anything but a loopback IP address with a port (`127.0.0.1:8484`, another `127.x.x.x` address or `[::1]:8484`). An empty host (`:8484`), the unspecified addresses, any other IP address and host names such as `localhost` are refused with a message and exit status 1; there is no option or environment variable to override it. In the log: `oarfish refuses to start`.

**On the device itself** (with a browser, for example over a screen): open <http://127.0.0.1:8484>.

**From another computer**: use an SSH tunnel. Replace `USER` and `DEVICE` with your login name and the device's name or address:

```bash
ssh -L 8484:127.0.0.1:8484 USER@DEVICE
```

Leave that window open, then open <http://127.0.0.1:8484> in the browser on your own computer. If port 8484 is already in use there, take another local port: `ssh -L 9000:127.0.0.1:8484 USER@DEVICE` and open <http://127.0.0.1:9000>.

Do not change the service to listen on another address; since Gate G5 the program would not start. Reverse proxy, TLS and network access come in later gates, together with authentication.

The first page is **Home**: live values of the device (CPU, memory, temperatures, storage, network) and alerts. The device is only read while a browser has Home open and visible (at most four windows at the same time). Opening Home shows the host name and the IP addresses of the device; do not share screenshots of it publicly.

**The installer overwrites `/etc/systemd/system/oarfish.service`.** If the file exists and differs from the one in the release, it is replaced (the installer says so), also when you re-run it. Do not edit that file. Local changes belong in a drop-in directory, which the installer never touches:

```bash
sudo systemctl edit oarfish        # creates /etc/systemd/system/oarfish.service.d/override.conf
```

## Run it again

Running the installer again is safe.

- Same version again: it verifies everything again, finds that nothing differs, and changes nothing. It does not restart the service.
- Another version: the new release is installed beside the old one, the `current` link is switched, the service restarts, and the health check runs. Old release directories are kept (they are small); remove old ones by hand if you want the space back.
- A download or verification problem at any point: the existing installation stays exactly as it was.

This is the installer's job for the first installation. Updating from the GUI comes in a later gate (G6).

## Hardware test results

Three tests, one per gate (G3 and G4 on the same device; G5 on a Raspberry Pi Zero 2 W Rev 1.0 with the same revision code). Each is a single data point, not general proof: it does not show that Oarfish works on other boards, other images or other versions of the same image.

### Gate G3: installer

| Item | Result |
|---|---|
| Device | Raspberry Pi Zero 2 W Rev 1.0, revision code 902120 (the "test device") |
| Image | "Raspberry Pi reference 2026-10-06", pi-gen stage2, pi-gen commit `b2cd98a9ee08a31ef7d0110ed09f2492bbd972ec` |
| Tested release | `v0.1.0-alpha.2`, commit `2d150d6e59b55edb18592b95b26d6fa1bfe7320b` |
| Support status | Compatible, not Verified. |
| System | Freshly flashed and updated Debian GNU/Linux 13 (trixie), `aarch64`, kernel `6.18.50+rpt-rpi-v8`. The tester reports that this is the Raspberry Pi OS Lite 64-bit image. |
| OpenSSL | 3.5.7 |
| systemd | 257 |
| Installer | `install.sh` of release `v0.1.0-alpha.2`; the file size on the device (27956 bytes) matches the release asset. |
| Dry run and real install | Both succeeded. |
| Service | `active` and `enabled`. |
| Reboot | Passed: after a reboot the service started by itself and `/healthz` returned `ok`. |
| Memory | Process `VmRSS` 7020 kB. |
| GUI | Reached from another computer through an SSH tunnel (see [First start and opening the GUI](#first-start-and-opening-the-gui)). |

Memory note: `systemctl status` shows no `Memory:` line on this image, because cgroup memory accounting is not enabled by default. The memory figure was therefore read from `/proc/<pid>/status` (`VmRSS`). It is the resident size of the Oarfish process at one moment, not a measurement under load and not a test of the 512 MB budget.

Anonymous download and verification of the release: checked on 2026-10-26 for `v0.1.0-alpha.2` (`curl` without token, signature, SHA-256 of the files, version stamp in `install.sh`). The `releases/latest/download/install.sh` address is a different matter and is still to be verified with the first stable release (see [Install](#install)).

The test setup is deliberately not identified here: no host or user names, no addresses.

**Not verified on hardware:**

- Ubuntu Server (any version) and any other board or image.
- Uninstall on the device, and running the installer again for the *same* version. (The G4 test below upgraded in place from `v0.1.0-alpha.2` to `v0.1.0-alpha.3`; rollback after a failed health check has not been tried on the device.)
- A tampered or damaged download on the device.
- Key rotation and revocation (not designed, ADR 0005).

### Gate G4: detection

Measured on 2026-10-27. Same device and same image as in the G3 block, so only the differences are repeated.

| Item | Result |
|---|---|
| Device | Raspberry Pi Zero 2 W Rev 1.0, revision code 902120 (the same device as in the G3 block) |
| Image | Same as in the G3 block |
| OS | Debian GNU/Linux 13 (trixie); detected as OS adapter `raspberry_pi_os` (status `initial-target`) |
| Kernel | `6.18.50+rpt-rpi-v8` |
| Tested release | `v0.1.0-alpha.3`, commit `b793b1069659f9ed85412b01523cc1f87e61df4f` |
| Method | In-place upgrade from `v0.1.0-alpha.2` with the `install.sh` of the alpha.3 release (the stamped installer); the SHA-256 of `install.sh` matched the signed `checksums.txt`. Then `GET /api/v1/device` on the device, the journal line `platform detected`, the "Gerät" page viewed in a browser through an SSH tunnel, and `VmRSS` of the `oarfish` process read from `/proc/<pid>/status`. |
| Profile | `raspberry-pi-zero-2-w` 1.0.0, matched by the device-tree compatible string (`matched_by` is `compatible`); SoC family `BCM2837` |
| Support status | Tier `compatible`, reason `profile_not_verified`. Limitations `memory_512mb` and `evidence_install_only`. `board_actions_allowed` is true. Not Verified. |
| Capabilities | All seven available: `procfs`, `sysfs`, `systemd`, `journald`, `cgroup_v2`, `cpu_temperature`, `boot_medium`. Boot medium `mmc`. CPU temperature was read and plausible. |
| Memory | `memory_total_bytes` 435318784 (415 MiB) |
| Log | The journal line `platform detected` showed `arch=arm64`, `os_adapter=raspberry_pi_os`, `profile=raspberry-pi-zero-2-w`, `tier=compatible`, `reason=profile_not_verified`. |
| GUI | The "Gerät" page showed support status, explanation, limitations, hardware, OS, kernel, sensor, boot medium and capabilities; the technical details were collapsed. |
| Idle memory | Process `VmRSS` 9212 kB, read about 6 minutes after the service restart, after the pages had been opened a few times. `v0.1.0-alpha.2` measured 7020 kB (G3). The target of ORB-NFR-001 is 35 MB, tolerated 45 MB. |

What this confirms: the device-tree compatible string `raspberrypi,model-zero-2-w` and a `/etc/rpi-issue` starting with `Raspberry Pi ` (the Raspberry Pi OS adapter matched), and that the unchanged service unit, with its sandboxing, lets the service read everything detection needs.

The same rules as above apply: one device, one image, one moment. The memory figure is the resident size of the process while idle, a single reading and not a measurement under load. No host or user names, addresses, serial numbers or MAC addresses are recorded here.

**Not verified on hardware (G4):**

- Ubuntu Server: detection is tested with fixtures and the architecture test only.
- Other boards and other Raspberry Pi models, and other images or versions of Raspberry Pi OS.
- USB boot (the classification is tested with a synthetic sysfs link only).
- Long-term memory use, and the service under load.

### Gate G5: dashboard

User-run on 2026-10-27 (the owner ran the commands and reported the results).

| Item | Result |
|---|---|
| Device | Raspberry Pi Zero 2 W Rev 1.0, revision code 902120 |
| OS | Raspberry Pi OS (Debian 13 trixie, arm64) |
| Kernel | `6.18.50+rpt-rpi-v8` |
| Tested release | `v0.1.0-alpha.4`, commit `5a9644edc551c0dc964ed9ab96e6fea1e51d80dc` |
| Method | In-place upgrade from `v0.1.0-alpha.3` with the `install.sh` of the alpha.4 release (signature OK, SHA-256 OK). The service unit was unchanged. Then the steps of [`DASHBOARD.md`](DASHBOARD.md#checking-a-device-acceptance-by-hand): the device's own tools (`free -b`, `uptime`, `df -B1`, the sysfs files), CPU ticks and `VmRSS` of the `oarfish` process from `/proc/<pid>/stat` and `/proc/<pid>/status`, `/proc/<pid>/io`, `vmstat 2 6`, and Home in Safari through an SSH tunnel. |
| Support status | Compatible, not Verified. |
| Sandbox | The unchanged unit allowed every file the dashboard reads: cpufreq, thermal, `/proc/net/*` and `statfs` on both mounts. No blocked read was seen. |
| AT-008 | Values compared with the device's own tools: `free -b` (total, used, swap total, swap used) equal to the API, apart from a few MB of normal drift in available memory; `uptime` matched; `df -B1` numbers equal (the percentage differs only because `df` rounds up and the API shows one decimal); CPU temperature 38.09 °C against 37.6 °C read two minutes earlier; `scaling_cur_freq` 600000 kHz equal to 600 MHz. |
| Idle, no browser (ORB-NFR-001, -003) | Five minutes: 0 of 30000 CPU ticks. `VmRSS` 8480 kB. |
| One Safari tab on Home (ORB-NFR-002, one client only) | 60 s: 17 of 6000 ticks (0.28 percent of one core). `VmRSS` 12680 kB, `VmHWM` 12924 kB. |
| Tab hidden | 60 s: 0 of 6000 ticks. Safari closed the stream and the sampler was idle. |
| Write load (AT-022) | `write_bytes` 0 in `/proc/<pid>/io` for the process, including the browser session. |
| Memory pressure (ORB-NFR-007) | `vmstat 2 6` with the tab open: `si` and `so` 0, `bi` and `bo` 0, CPU idle 100 percent. 1.5 MiB parked in swap, no swap activity. |
| Restart with the tab open (ORB-NFR-014) | The service was restarted while the tab was open. The owner reported that the page behaved as expected. The lost-connection banner was not recorded separately. |
| GUI | Home viewed in Safari through an SSH tunnel with live values in all cards and a transfer rate in the network card. |

The same rules as above apply: one device, one image, one moment, readings of minutes and not of hours. Numbers are single readings, not a budget test under load. No host or user names, addresses, serial numbers or MAC addresses are recorded here.

**Not verified on hardware (G5):**

- Chromium and Firefox (only Safari was used, once).
- Keyboard-only use and screen-reader use (AT-025 was checked by structure tests only).
- Four simultaneous clients: the ORB-NFR-002 reading was taken with one.
- Ubuntu Server, other boards and other Raspberry Pi models.
- Long-running behaviour.
- CPU usage percent and the network counters against a second tool.
- The lost-connection display of Home after a restart (only the owner's statement above exists).

## Uninstall

```bash
sudo sh install.sh --uninstall           # stop and remove the service and the program; keeps your data
sudo sh install.sh --uninstall --purge   # also remove /etc/oarfish, /var/lib/oarfish and the user "oarfish"
sudo sh install.sh --uninstall --dry-run # show what would be removed
```

The first form removes the service, the unit file, `/opt/oarfish` and `/var/cache/oarfish`. It keeps `/etc/oarfish`, `/var/lib/oarfish` and the user, so a later installation finds your configuration and data. `--purge` is the explicit flag that removes them. Any copy of `install.sh` can do this; the uninstall downloads nothing.

## Where things go

| Path | Content |
|---|---|
| `/opt/oarfish/releases/<version>/` | One directory per installed release: `oarfish`, `oarfish.service`, `LICENSE`, `THIRD_PARTY_NOTICES.txt`. Owned by root, never modified after installation. |
| `/opt/oarfish/current` | Symlink to the active release, switched atomically. |
| `/etc/systemd/system/oarfish.service` | The service unit (copied from the release). |
| `/etc/oarfish/` | Configuration (`root:oarfish`, 0750); `secrets/` inside is `oarfish`, 0700. Empty for now. |
| `/var/lib/oarfish/` | State, owned by `oarfish`. Empty for now. |
| `/var/cache/oarfish/` | Cache, owned by `oarfish`. |
| `/run/oarfish/` | Created by systemd when the service starts. |
| user and group `oarfish` | System user without login shell and without home directory. |

The service runs as `oarfish`, not as root, with the sandboxing options of the unit file (no extra privileges, read-only system, private `/tmp`, no devices, restricted system calls). `systemd-analyze security oarfish` shows the exposure rating on your device.

## If something goes wrong

The installer stops with a message that starts with `oarfish-install: error:`.

| Message (shortened) | Meaning and what to do |
|---|---|
| `unsupported CPU architecture` | Not a 64-bit ARM system. Check `uname -m`. |
| `systemd is not the running init system` | No systemd, or inside a container. |
| `required tools are missing: ...` | Install them, for example `sudo apt install curl openssl`. |
| `... is not OpenSSL` / `OpenSSL ... is too old` | Needs OpenSSL 3.0+. Upgrade the system or its `openssl` package. |
| `SIGNATURE VERIFICATION FAILED` | The release was not signed with the Oarfish release key, or the files were changed on the way. Do not install. Nothing was changed. |
| `does not match the SHA-256 in the signed checksums.txt` | The download is damaged or has been changed. Try again; if it repeats, do not install. |
| `download of ... failed` | The tag does not exist, there is no network, or GitHub is unreachable. |
| `not tied to a release` | You used the `install.sh` from the repository. Pass `--version`, or use the one from the release page. |
| `did not become healthy` | The files are installed but the service does not answer. Look at `journalctl -u oarfish`. If an older version was active, the installer went back to it and tells you whether that one is healthy. On a first installation the installer stops and disables the service (the files stay); after fixing the cause start it with `sudo systemctl enable --now oarfish`. |
| `rollback ... did not become healthy either` | Neither the new nor the previous version runs. Check `systemctl status oarfish` and `journalctl -u oarfish`. |
| `userdel was not found` | `--uninstall --purge` has to remove the service user and needs `userdel`. Nothing was changed. |

## What is verified, and what is not

**Verified automatically (CI, every change):**

- The installer functions against a fake root directory with a throwaway signing key, run with `dash` and with `bash`: wrong architecture, non-Linux, missing systemd, missing tools, LibreSSL and OpenSSL 1.1, a "verifier" that accepts everything, tampered `checksums.txt`, tampered and truncated signature, signature from another key, a valid prehashed signature, replayed signature, tampered and truncated bundle, tampered manifest, missing files, interrupted download (old installation untouched), bundles with extra files, links or an x86-64 binary, re-run without changes, upgrade, rollback after a failed health check (with a healthy and with an unhealthy rollback, and `reset-failed` before the restart), a first installation that never becomes healthy (service stopped and disabled, files kept), `useradd`, `chown` on every path and `userdel` with stub tools, dry run, uninstall and purge. `scripts/test-installer.sh`. The `systemctl` used there is a stub, so these tests prove the installer's logic, not systemd behaviour.
- The release pipeline: `install.sh` is stamped with the version, listed in the signed `checksums.txt`, and a tampered or wrongly stamped `install.sh` is rejected by `scripts/verify-release.sh`. The bundle, manifest and `install.sh` are reproducible.
- End to end in plain `debian:12` and `ubuntu:24.04` **arm64** container images under qemu-user: signature verification with the image's OpenSSL, real `useradd`/`chown`, the real ARM64 binary started as the `oarfish` user, `/healthz` and the GUI answer, only `127.0.0.1:8484` listens, a second run changes nothing, uninstall and purge. Result of the first CI run: both passed. The minimal images contained **neither `curl` nor `openssl`** (the test installed both with `apt-get`, giving OpenSSL 3.0.22 on Debian 12 and 3.0.13 on Ubuntu 24.04); `tar`, `gzip`, `sha256sum`, `od`, `base64`, `awk`, `useradd` and `getent` were present. On a minimal system run `sudo apt install curl openssl` first. The per-job tool report is in the job summary of the "Installer in arm64 container" jobs.
- The unit file passes `systemd-analyze verify` on the CI runner.

**Verified on hardware (one device, see [Hardware test results](#hardware-test-results)):**

- Gate G3: dry run and install on a Raspberry Pi Zero 2 W (image reported as Raspberry Pi OS Lite 64-bit), the unit file started by a real systemd (257), service active and enabled, start after a reboot, `/healthz`, the GUI through an SSH tunnel, and one process memory reading.
- Gate G4 (`v0.1.0-alpha.3`): in-place upgrade from `v0.1.0-alpha.2` with the stamped installer; detection of the board, SoC family, OS adapter, kernel, memory, all seven capabilities, CPU temperature and boot medium (`mmc`); support status Compatible with the profile `raspberry-pi-zero-2-w`; the "Gerät" page in a browser through an SSH tunnel; and a second idle `VmRSS` reading (9212 kB). The unchanged unit file's sandbox did not block detection.
- Gate G5 (`v0.1.0-alpha.4`): in-place upgrade from `v0.1.0-alpha.3` with the stamped installer; the unchanged unit file's sandbox did not block any file the dashboard reads; dashboard values against the device's own tools (AT-008); idle CPU and `VmRSS` with no browser, with one Safari tab and with the tab hidden; no writes by the process (AT-022); no swap activity (ORB-NFR-007); a service restart with the tab open (the owner reported expected behaviour); Home in Safari through an SSH tunnel. Details in [Gate G5: dashboard](#gate-g5-dashboard).

**Not verified:**

- **Gate G5 (dashboard), what the hardware run did not cover**: Chromium and Firefox, keyboard-only and screen-reader use, four simultaneous clients, CPU usage percent and network counters against a second tool, and long-running behaviour. See [`DASHBOARD.md`](DASHBOARD.md#verification-status).
- **Ubuntu Server and any other board or image** have not run on real hardware. Container results are no proof for them. That includes device detection on Ubuntu (fixtures and the architecture test only), other Raspberry Pi models, and USB boot.
- Uninstall, and running the installer again for the same version, have not been tried on the device (only in the containers and the CI tests). An upgrade from one version to the next was done once on the device (G4 test); a rollback was not.
- A tampered or damaged download has not been tried on the device (only in the CI tests).
- The containers have **no systemd**: a small stand-in starts the binary. Only the one device above has run the unit file under a real systemd; other kernels and images may differ.
- The 512 MB memory budget under load, long-term memory use, and write load on a microSD card are not tested. The idle `VmRSS` readings (7020 kB, 9212 kB and 8480 kB, three different releases) are not a budget test.
- The presence of OpenSSL 3 and the other tools on Ubuntu Server images, and on other versions of Raspberry Pi OS, is assumed from package information. One device had OpenSSL 3.5.7.
- `releases/latest/download/install.sh` is checked with the first stable release; the release addressed by tag was downloaded anonymously and verified on 2026-10-26 for `v0.1.0-alpha.2` (see [RELEASING.md](RELEASING.md)).
- Key rotation and revocation are not designed (ADR 0005). The release key is inside the installer; if it ever changes, a new installer is needed.
- Two installers running at the same time are not guarded against.
