# Installing Oarfish

Oarfish runs on a 64-bit ARM Linux device with systemd. You download one script, read it, and run it. The script downloads the release from GitHub, checks the signature, and installs Oarfish as a service. No GitHub account, no token, no Docker, no Node.js, no Python.

**Status (Gates G3, G4 and G5):** this installer, the device detection built into the service and the dashboard have run on **one** real Raspberry Pi Zero 2 W at a time (see [Hardware test results](#hardware-test-results)); neither has been run on Ubuntu Server or on any other board. What was tested, and what was not, is listed at the end ([What is verified](#what-is-verified-and-what-is-not)). Oarfish today shows Home (live values, alerts) and Gerät, a read-only page about what it detected about the board and the system, behind a local administrator login (Gate G5.1); there is no device management yet (later gates).

**Kurzfassung (Deutsch):** `install.sh` von der Release-Seite laden, lesen, mit `sudo sh install.sh --dry-run` prüfen, dann mit `sudo sh install.sh` installieren. Die Oberfläche läuft nur auf `127.0.0.1:8484` (nur lokal). Von einem anderen Rechner erreichst du sie über einen SSH-Tunnel: `ssh -L 8484:127.0.0.1:8484 BENUTZER@GERAET`, dann im Browser <http://127.0.0.1:8484> öffnen. Deinstallieren: `sudo sh install.sh --uninstall` (Daten bleiben), mit `--purge` werden auch Daten und Benutzer gelöscht.

## What you need

| Need | Why |
|---|---|
| 64-bit ARM Linux (`uname -m` shows `aarch64`) | The release is built for ARM64 only. 32-bit systems are refused. |
| systemd as the running init system | Oarfish is installed as a systemd service. |
| `openssl` **3.0 or newer** | Checks the release signature. OpenSSL 1.x and LibreSSL are refused. Check with `openssl version`. |
| `curl`, `tar`, `gzip`, `sha256sum`, `flock` (util-linux) and the usual base tools | The installer lists any that are missing and stops. `flock` takes the lock that keeps two installer runs, and later an update, apart. |
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

# 2. Read it. It is a plain shell script of about 1,000 lines.
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

A release can carry the update helper (the "components" bundle shape, below). Then the installer also takes the lock, creates the helper layout and checks the helper socket; step 5 and 6 differ as described in the next section. A release without it (the "legacy" shape) is installed exactly as before.

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
curl http://127.0.0.1:8484/healthz   # {"status":"ok","version":"...","pid":...}
```

Oarfish listens on `127.0.0.1:8484` only. That means: only programs on the device itself can reach it. This is on purpose: the login of Gate G5.1 protects the GUI, but LAN access and HTTPS are not designed yet (D-D10), so it must not be reachable from the network, and the installer never opens it.

The program also enforces this, and Gate G5.1 does not lift it (D-C48): it refuses to start when `-listen` is anything but a loopback IP address with a port (`127.0.0.1:8484`, another `127.x.x.x` address or `[::1]:8484`). An empty host (`:8484`), the unspecified addresses, any other IP address and host names such as `localhost` are refused with a message and exit status 1; there is no option or environment variable to override it. In the log: `oarfish refuses to start`.

**On the device itself** (with a browser, for example over a screen): open <http://127.0.0.1:8484>.

**From another computer**: use an SSH tunnel. Replace `USER` and `DEVICE` with your login name and the device's name or address:

```bash
ssh -L 8484:127.0.0.1:8484 USER@DEVICE
```

Leave that window open, then open <http://127.0.0.1:8484> in the browser on your own computer. If port 8484 is already in use there, take another local port: `ssh -L 9000:127.0.0.1:8484 USER@DEVICE` and open <http://127.0.0.1:9000>.

Do not change the service to listen on another address; since Gate G5 the program would not start. Reverse proxy, TLS and network access need their own design before the MVP release (open item D-D10); the SSH tunnel is the way until then.

**The first account (since Gate G5.1).** A fresh installation, and a device updated from `v0.1.0-alpha.5` or earlier, has no administrator account. The GUI then shows the setup page and asks for the setup token; nothing else is shown until the account exists (D-C46, ADR 0009). The installer's final message says so. Read the token on the device:

```bash
sudo cat /var/lib/oarfish/setup-token
```

Enter it in the GUI with a user name (1 to 32 characters: `a-z`, `0-9`, `.`, `_`, `-`) and a password (12 to 128 characters). The token is a new one after every service start while no account exists, and the file is deleted after the setup. Afterwards the GUI asks for the user name and password; a session ends after one hour without use, after twelve hours at the latest, and at every service restart. Details, the API and the audit file: [`AUTH.md`](AUTH.md).

**Lost password.** Stop the service, delete the account file as root, start the service; it is in setup mode again (the audit file stays):

```bash
sudo systemctl stop oarfish
```

```bash
sudo rm /var/lib/oarfish/auth/account.json
```

```bash
sudo systemctl start oarfish
```

The first page is **Home**: live values of the device (CPU, memory, temperatures, storage, network) and alerts. The device is only read while a browser has Home open and visible (at most four windows at the same time). Opening Home shows the host name and the IP addresses of the device; do not share screenshots of it publicly.

### The update helper layout (G6.3b)

Since step G6.3b of ADR 0010 the release bundle carries the update helper by default (`oarfish-update-helper` and the units `oarfish-update-helper.socket`, `oarfish-update-helper.service` and `oarfish-update-recover.service`, [`RELEASING.md`](RELEASING.md#bundle-shapes-adr-0010-steps-g62-and-g63b)). Since `v0.1.0-alpha.7` the helper does the real work of a GUI update (stage, activate, health check, rollback, recovery; [`UPDATE.md`](UPDATE.md)). It was observed on one device in the G6.9 run ([`UPDATE.md`](UPDATE.md#results-on-the-reference-device), class D and T). The layout was also observed on one device at the owner stop of G6.3 (a test release with a throwaway key, never published); what was seen and what was not is in [Results on the reference device (G6.3)](#results-on-the-reference-device-g63). It is not Verified. The installer accepts exactly two bundle shapes, **legacy** (the five files of earlier releases) and **components** (those plus the four helper files), and the shape found in the bundle must agree with the manifest.

For the components shape the installer, under the lock described below, creates:

| Path | Content |
|---|---|
| `/opt/oarfish/components/<id>/` | The helper and its three units (root:root, directory 0755, helper 0755, units 0644). `<id>` is the version of the release that first delivered exactly these bytes; a later release with identical bytes creates nothing new. Never modified after installation. |
| `/opt/oarfish/components/active` | Relative link to `<id>`; serves the helper, its socket and its service. Moves at once when the helper bytes change. |
| `/opt/oarfish/components/known-good` | Relative link to `<id>`; serves the recover unit. On the first installation it equals `active`; later it moves only **after** the health check of the run has passed. |
| `/usr/libexec/oarfish-update-helper` | Link to `/opt/oarfish/components/active/oarfish-update-helper`. |
| `/etc/systemd/system/oarfish.service` | Link to `/opt/oarfish/current/oarfish.service` (it was a copy before). A regular file there (an install of an earlier release) is replaced atomically by the link; the drop-in directory `oarfish.service.d` is never touched. |
| `/etc/systemd/system/oarfish-update-helper.socket` and `.service` | Links to the files in `components/active`. |
| `/etc/systemd/system/oarfish-update-recover.service` | Link to the file in `components/known-good`. |
| `multi-user.target.wants/oarfish.service`, `sockets.target.wants/oarfish-update-helper.socket`, `multi-user.target.wants/oarfish-update-recover.service` | **Relative** links to the unit in `/etc/systemd/system` (for example `../oarfish.service`). The installer creates them itself and does not use `systemctl enable`, which would link the resolved release directory (ADR 0010, A5); they therefore keep working when an old release directory is removed. |
| `/var/lib/oarfish-update/` | Root-owned, mode 0700. Created by the installer because it takes the lock there before the helper has ever run. An existing directory with another owner or mode is a reason to stop. |

The order of a run: take the lock; create the directories; install the release and the component set; create the links; one `systemctl daemon-reload`; if the helper bytes or the socket unit changed, or the socket is not active, `systemctl stop oarfish-update-helper.service` and then `systemctl restart oarfish-update-helper.socket` (systemd keeps the text a socket was started with, and a socket cannot be started while its service is active, ADR 0010 A9); restart `oarfish.service` when something changed. The recover unit is enabled by its link and is **not** started by the installer. The health check additionally needs `oarfish-update-helper.socket` to be active (not the service, which is socket activated and exits when idle). Running the same release again changes nothing and restarts nothing.

**The lock.** The installer takes an exclusive `flock` on the directory `/var/lib/oarfish-update`, the object the helper will lock too (ADR 0010 U7, U8; the helper side came with G6.5 (`stage`, `discard`, expiry cleanup; tested in CI against temporary directories, not on a device), so on a device it has so far only been observed to guard installer runs against each other; two installer runs at the same time were tested only against the fake root, not on a device, and a helper holding the lock has not been tried on a device). It waits at most 30 seconds and then stops with the message that an update or another installer run is in progress and that nothing was changed. The lock is held to the end of the run, also for `--uninstall`, and is released when the installer exits, however it exits. A legacy-shape release on a device without that directory takes no lock and does not create the directory.

**If the health check fails** and an earlier release was active, the installer switches `current` back, switches `components/active` back if it moved in this run, leaves `known-good` where it was, reloads systemd, stops the helper service, restarts the socket and checks again. The layout created in the run stays. On a first installation it disables `oarfish.service`, the helper socket and the recover unit and keeps all files; run the installer again after fixing the cause.

**What changes on an existing device.** An alpha-style installation (`/etc/systemd/system/oarfish.service` is a regular file, no helper) is migrated by running the installer of a release that carries the helper: the unit file becomes a link, the layout above is created, `current` is switched, and the service restarts if the version changed. The one-time step from a release without the helper is an installer run (ADR 0010 U11, the "bootstrap release"). After that, updates come from the GUI ([`UPDATE.md`](UPDATE.md)), observed on one device, not Verified.

**Going back to a release without the helper is not supported.** A legacy-shape release is refused with a clear message on a device that already has the helper layout; use `--uninstall` first (this is the installer's reading of ADR 0010, which does not describe a downgrade to a release without a helper). The installer keeps old release directories and old component sets. Since G6.10 step b the update helper removes old component sets itself after a component update from the GUI: it keeps the sets that `active` and `known-good` name and the one they replaced (ADR 0010 A16, P16; class C tests, not run on a device).

**The installer overwrites `/etc/systemd/system/oarfish.service`** (a copy for a legacy release, a link into the active release for a components release). If the file exists and differs from the one in the release, it is replaced (the installer says so), also when you re-run it. Do not edit that file. Local changes belong in a drop-in directory, which the installer never touches:

```bash
sudo systemctl edit oarfish        # creates /etc/systemd/system/oarfish.service.d/override.conf
```

## Run it again

Running the installer again is safe.

- Same version again: it verifies everything again, finds that nothing differs, and changes nothing. It does not restart the service.
- Another version: the new release is installed beside the old one, the `current` link is switched, the service restarts, and the health check runs. The installer keeps old release directories; remove old ones by hand if you want the space back. (After a successful GUI update the update helper keeps only the active and the previous release, ADR 0010 U5.)
- A download or verification problem at any point: the existing installation stays exactly as it was.

**Updating from the GUI** exists since `v0.1.0-alpha.7` (System > Update, [`UPDATE.md`](UPDATE.md)). It was observed as expected on one device in the G6.9 run (class D, [`UPDATE.md`](UPDATE.md#results-on-the-reference-device)); it is **not Verified**. Since G6.10 (implemented in the code, class C tests, **not yet in a release and not run on a device**) the update helper can also replace itself, its units and `oarfish.service` from the GUI, after a second confirmation ([`UPDATE.md`](UPDATE.md#the-steps-of-an-update-with-components)). The installer is still required:

- **Once, to reach the first release that carries the helper of G6.10.** Every helper released before it (`v0.1.0-alpha.7` to `v0.1.0-alpha.10`) cannot replace itself and answers `installer_required` for the new helper; the update page shows the installer instruction. Up to `v0.1.0-alpha.6` there is no update helper at all (the bootstrap, ADR 0010 U11).
- **After that, only in these cases**, each shown on the update page as "Installer nötig" with its reason ([`UPDATE.md`](UPDATE.md#when-the-installer-is-required)): the release changes the trust key of the helper (`key_changed`; key rotation is not available from the GUI); a unit file of the release does not match the rules of the installed helper (`unit_rejected`; the installer does not lint, so it installs such a release); the release has requirements that the device does not meet with the installed helper (`requirements_not_met`, for example a helper protocol two steps newer).

Use the `install.sh` of the release you want, as in [Install](#install).

**A changed socket unit takes effect after a reboot.** When an update from the GUI replaces the helper's socket unit, systemd keeps the socket it started with until the next boot; the update page shows a notice until then (ADR 0010 A15 D4). An installer run stops the helper service and restarts the socket, so the new socket text is in use after it; the notice of the page stays until the next boot. Nothing else is needed.

### When the update helper needs a person

The update page says "Wiederherstellung fehlgeschlagen" (rollback failed) and the helper reports that it needs attention. Updates from the GUI are blocked until a person acts. The cases differ:

- **A release rollback failed** (`rollback_target_damaged`, `rollback_unhealthy`, `restart_failed`, `switch_failed`, `current_unknown`): the page says to run the installer again. Use the `install.sh` of the release that should run, as in [Install](#install). The G6.9 device run did this once from `rollback_target_damaged` (class T, [`UPDATE.md`](UPDATE.md#phase-3-class-t-v010-alpha10-throwaway-key)).
- **The saved component set is damaged** (`known_good_damaged`, ADR 0010 E5): a component update failed, and the helper did not switch back, because the copy it would switch back to (`/opt/oarfish/components/known-good`) does not have the bytes it recorded, or cannot be read. Nothing else was changed: `current` still names the release that ran before, and the service keeps running. The helper keeps this state at every boot and start until a person acts; no helper process clears it, and the installer does not write the helper's state.

**The steps for `known_good_damaged`. They are derived from the code of the helper and the installer, were not run on a device or anywhere else, and are for the owner's confirmation (ADR 0010 A17, S8).** Look first, then repair:

```bash
# 1. What the helper wrote (the journal lines name the phase and the reason, no secrets).
sudo journalctl -u oarfish-update-helper -u oarfish-update-recover --since "-2 days" | grep -E 'phase=|known_good'
# 2. Which component sets the links name, and their hashes.
readlink /opt/oarfish/components/active /opt/oarfish/components/known-good /opt/oarfish/current
sudo sha256sum /opt/oarfish/components/*/oarfish-update-helper /opt/oarfish/components/*/*.service /opt/oarfish/components/*/*.socket
# Compare them with the "contents" of manifest.json of the release that delivered each set
# (the directory name is that version). A file that differs from its release was changed on this device:
# find out why before you go on.
# 3. Repair: remove the program files (keeps your account, your settings and /var/lib/oarfish-update),
#    remove the helper's state while no helper runs, and install the release that ran before.
sudo sh install.sh --uninstall
sudo rm /var/lib/oarfish-update/state.json
sudo sh install.sh            # the install.sh of the installed release, as in "Install"
```

After step 3 the helper starts with a fresh state; the update page may show once that the helper rebuilt its state. Then try the update again from the GUI.


## Hardware test results

Six tests (G3 and G4 on the same device; G5, the UI foundation and the access control of G5.1 on a Raspberry Pi Zero 2 W Rev 1.0 with the same revision code; the update helper layout of G6.3 on a Raspberry Pi Zero 2 W whose revision code the evidence does not record). Each is a single data point, not general proof: it does not show that Oarfish works on other boards, other images or other versions of the same image.

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

Anonymous download and verification of the release: checked on 2026-10-26 for `v0.1.0-alpha.2` (`curl` without token, signature, SHA-256 of the files, version stamp in `install.sh`); later checked for `v0.1.0-alpha.6` (see [RELEASING.md](RELEASING.md#verify-a-release-anonymously)). The `releases/latest/download/install.sh` address is a different matter and is still to be verified with the first stable release (see [Install](#install)).

The test setup is deliberately not identified here: no host or user names, no addresses.

**Not verified on hardware:**

- Ubuntu Server (any version) and any other board or image.
- At the time of the G3 test: uninstall on the device, and running the installer again for the *same* version. Both were later observed once at the G6.3 owner stop (see [Results on the reference device (G6.3)](#results-on-the-reference-device-g63)). (The G4 test below upgraded in place from `v0.1.0-alpha.2` to `v0.1.0-alpha.3`; rollback after a failed health check has still not been tried on the device.)
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

### UI foundation (`v0.1.0-alpha.5`)

User-run on 2026-10-28 (the owner ran the steps and reported the results). Not a gate: this checks the redesigned interface ([`UI.md`](UI.md)) on the device.

| Item | Result |
|---|---|
| Device | Raspberry Pi Zero 2 W Rev 1.0, revision code 902120 (the same device model and revision code as in the G5 block) |
| OS | Raspberry Pi OS (Debian 13 trixie, arm64) |
| Kernel | `6.18.50+rpt-rpi-v8` |
| Tested release | `v0.1.0-alpha.5`, commit `c86d15929ce0431b43a19dffe1c447844cac5451` |
| Method | In-place upgrade from `v0.1.0-alpha.4` with the `install.sh` of the alpha.5 release: dry run, then installation. Signature OK, SHA-256 OK, service healthy. Then Home and "Gerät" viewed in Safari (light and dark theme), Chrome and Firefox through an SSH tunnel, and keyboard-only use in Chrome or Firefox (the owner did not name which). |
| Support status | Compatible, not Verified. |
| Layout | In all three browsers the layout looked as intended: nothing was cut off and no value list broke into single characters. The connection indicator read "Live". |
| Alerts (D-C44, swap from 25 percent) | On the idle device the alert list was empty. 1.5 MiB of 415 MiB parked in swap raised no alert, and there was no alert for the Compatible tier. The support status was shown as a badge in the header. This confirms the alert tuning on the device. |
| Network card | The tunnel interface with operstate `unknown` was shown in neutral grey, not red. |
| Keyboard-only use | Worked in Chrome or Firefox, as reported by the owner: skip link, tab order, opening and closing the technical details, navigation to "Gerät" and the "Erneut laden" button. The focus indicator was visible. |

The same rules as above apply: one device, one image, one moment. No host or user names, addresses, serial numbers or MAC addresses are recorded here. Idle CPU and `VmRSS` of `v0.1.0-alpha.5` were not measured again.

**Not verified on hardware (UI foundation):**

- The dark theme in Chrome and Firefox (not reported).
- Screen-reader use.
- Keyboard use in Safari.
- The lost-connection display of the new UI on the device.
- Idle CPU and `VmRSS` of `v0.1.0-alpha.5` (not re-measured).
- Four simultaneous clients.
- Ubuntu Server, other boards and other Raspberry Pi models.
- Long-running behaviour.
- CPU usage percent and the network counters against a second tool.

Known gaps of the interface itself are listed in [`UI.md`](UI.md#known-gaps).

### Access control and audit (`v0.1.0-alpha.6`)

User-run on 2026-10-31 to 2026-11-02 (the owner ran the steps and reported the results); the 24-hour readings span 24 h 41 min. Gate G5.1 closeout evidence: this block records what was seen. Gate G5.1 passed on 2026-11-02, on the owner's decision after reviewing this evidence. The measurements are in [`AUTH.md`](AUTH.md#results-on-the-reference-device-v010-alpha6); this block gives the install-side result.

| Item | Result |
|---|---|
| Device | Raspberry Pi Zero 2 W Rev 1.0 (the reference device; the same device model and revision code as in the blocks above) |
| OS | Debian GNU/Linux 13 (trixie), arm64 |
| Kernel | `6.18.50+rpt-rpi-v8` |
| Tested release | `v0.1.0-alpha.6`, commit `d4300bd2d0e4db7d7051f09f0aa8eb2171acda38` |
| Method | In-place upgrade from `v0.1.0-alpha.5` with the `install.sh` of the alpha.6 release (the stamped installer). Then the steps of [`AUTH.md`](AUTH.md#measurements-and-checks-on-the-device). |
| Support status | Compatible (the tier is unchanged). |
| Upgrade into setup mode | The service started in setup mode after the update: session state `setup_required`, `GET /api/v1/device` answered `401`, `/healthz` answered `ok`. |
| State directory (D-D11) | `/var/lib/oarfish`, `auth/` and `audit/` were 0700 after the update, so the installer needs no change. `account.json`, `audit.jsonl` and `setup-token` were 0600; the owner was the service user. |
| Setup and login | Setup with the token worked and the token file was removed afterwards. Login, logout, a wrong password, the rate limit, a password change and a restart behaved as documented in [`AUTH.md`](AUTH.md#results-on-the-reference-device-v010-alpha6). Lost-password recovery as documented worked. |
| Idle memory | `VmRSS` 8784 kB after a fresh start; 10960 kB after 24 h 41 min without a restart (`VmHWM` 30004 kB, which includes the password hash). Single readings. |
| Hash cost | 442.9 ms per verification (Go benchmark on the device, 10 iterations). |

The same rules as above apply: one device, one image, one moment. No host or user names, addresses, serial numbers or MAC addresses are recorded here.

**Not verified on hardware (G5.1 closeout):**

- Screen-reader use and keyboard use of the new pages (Einrichten, Anmelden, Konto).
- Ubuntu Server, other boards and other Raspberry Pi models.
- The interface changes of the later UI task (the known-gaps task) in a real browser or on the device.
- Four simultaneous clients.
- Behaviour over many days (the longest reading is 24 h 41 min).
- One audit entry that went missing in one run is unexplained ([`AUTH.md`](AUTH.md#open-observations-from-the-device-run)).

### Results on the reference device (G6.3)

Label for every row below: **device observation (G6.3 owner stop, 2026-11-03): one Raspberry Pi Zero 2 W, Debian 13 (trixie) aarch64, kernel 6.18.50, systemd 257, one image, one moment.** The owner ran the steps and reported the results. Not a release test and not Verified.

What was installed: a test release `0.1.0-alpha.6.1` (components shape, update helper stub) built from the merge commit of pull request 25, signed with a throwaway key and installed with a test copy of `install.sh` that differs from the production installer only in the key, the allowed download protocol and the base-URL override. It was never published. **The production key path with the real installer was not exercised there.** Support status stays Compatible; nothing is Verified.

| Item | Result (device observation, G6.3 owner stop, 2026-11-03) |
|---|---|
| Fresh components install over a `v0.1.0-alpha.6` device (regular unit file) | The layout came out as designed: `components/<id>` with the relative links `active` and `known-good`, the `/usr/libexec` link, four unit links, `oarfish.service` turned from a regular file into a link into `current`, three relative wants links, `/var/lib/oarfish-update` `root:root` 0700, modes and owners as specified. No `enable` call. The hash of the account file was unchanged; the GUI login with the old password worked after the install and after a reboot. |
| Socket activation | The socket was active (listening) with mode 0660 `root:oarfish` in a root-owned 0755 directory. The helper service was inactive until the first query. A `status` query as the user `oarfish` was answered with `key_id` `11f1c52777090bfc` and the installed version. The helper was active right after, inactive after 35 s (idle exit) and started again by the next query. The same query as an ordinary user got `Permission denied`; as root the connection was closed without an answer. |
| `systemd-analyze security` | Both helper units 1.7 OK, equal to the offline value in CI; `oarfish.service` 1.7 OK as well. |
| Wants links (ADR 0010 A5) | After `current` moved from `0.1.0-alpha.6` to `0.1.0-alpha.6.1` and the old release directory was removed, `daemon-reload` and a reboot left all three units enabled, loaded and started. The release directories `alpha.2` to `alpha.5` were still there: the installer keeps old releases. |
| Boot order after that reboot | The recover unit exited about 42 ms before `oarfish.service` started and about 54 ms before the socket became active; it needed about 650 ms; result `success`. Total boot 40.9 s against 41.3 s before, no noticeable delay. This row predates the finding of the G6.9 evidence run, recorded in ADR 0010 A14: on a cold boot with the unit text of `v0.1.0-alpha.8` and `v0.1.0-alpha.9`, systemd deleted the socket job to break an ordering cycle; read the row with that. |
| Same installer again | Nothing changed and nothing was restarted (same PID, same activation timestamps). |
| `SIGKILL` of the running helper (extra observation) | The service failed with result `signal`; `OnFailure=` started the recover unit, which finished within the same second; one second later `Restart=on-failure` started the helper again without a connection (restart counter 1). The socket stayed active and the next query was answered. |
| Uninstall without `--purge` | Removed the `/opt/oarfish` tree, the cache, the helper link, the units and the wants links; kept `/etc/oarfish`, `/var/lib/oarfish`, `/var/lib/oarfish-update` and the user; no failed units; the account hash was unchanged. The official `v0.1.0-alpha.6` installer then restored the original state. |

The same rules as above apply: one device, one image, one moment. No host or user names, addresses, serial numbers or MAC addresses are recorded here.

**Not exercised on the device (G6.3):**

- `--uninstall --purge`.
- A first installation without any previous release.
- The installer rollback after a failed health check.
- The production-key path with the real installer.
- Ubuntu Server, another Raspberry Pi model, a cold power cycle.
- A crash loop up to the start limit, and a helper that does not start.
- Two installer runs at the same time (tested only against the fake root) and a helper that holds the lock (the helper core of G6.5 takes it for `stage` and `discard`; never run on a device).

## Uninstall

```bash
sudo sh install.sh --uninstall           # stop and remove the service and the program; keeps your data
sudo sh install.sh --uninstall --purge   # also remove /etc/oarfish, /var/lib/oarfish, /var/lib/oarfish-update and the user "oarfish"
sudo sh install.sh --uninstall --dry-run # show what would be removed
```

The first form removes the service, the unit file, `/opt/oarfish` and `/var/cache/oarfish`. It keeps `/etc/oarfish`, `/var/lib/oarfish` and the user, so a later installation finds your configuration and data, including the administrator account and the audit file (`/var/lib/oarfish/auth/`, `/var/lib/oarfish/audit/`): after a reinstall you sign in with the old password. `--purge` is the explicit flag that removes them. Any copy of `install.sh` can do this; the uninstall downloads nothing.

If the update helper layout exists, the uninstall takes the lock on `/var/lib/oarfish-update` first, stops and disables `oarfish.service`, the helper socket, the helper service and the recover unit, and removes the three wants links, the unit links and `/usr/libexec/oarfish-update-helper` **only where they are links into `/opt/oarfish`** (the wants links where they are links named for the unit). A regular file or a link to something else at one of these paths is left alone and reported. The drop-in directory `oarfish.service.d` is never touched. `/var/lib/oarfish-update` is kept without `--purge` (the message says so) and removed by `--purge`. Uninstall without `--purge` was observed on one device at the G6.3 owner stop (see [Results on the reference device (G6.3)](#results-on-the-reference-device-g63)); `--purge` was not tried on a device.

## Where things go

| Path | Content |
|---|---|
| `/opt/oarfish/releases/<version>/` | One directory per installed release: `oarfish`, `oarfish.service`, `LICENSE`, `THIRD_PARTY_NOTICES.txt`. Owned by root, never modified after installation. |
| `/opt/oarfish/current` | Symlink to the active release, switched atomically. |
| `/etc/systemd/system/oarfish.service` | The service unit (copied from the release for a legacy release; a link to `/opt/oarfish/current/oarfish.service` for a components release). |
| `/opt/oarfish/components/`, `/usr/libexec/oarfish-update-helper`, the helper units in `/etc/systemd/system`, the wants links | The update helper layout of a components release, see [The update helper layout](#the-update-helper-layout-g63b). |
| `/var/lib/oarfish-update/` | Root-owned, mode 0700; the lock directory (and, later, the helper's state). |
| `/etc/oarfish/` | Configuration (`root:oarfish`, 0750); `secrets/` inside is `oarfish`, 0700. Empty for now. |
| `/var/lib/oarfish/` | State, owned by `oarfish`, mode 0700 (the installer creates it as 0750; the unit's `StateDirectoryMode=0700` applies when the service starts). Holds `auth/account.json` (the administrator account, 0600), `setup-token` (0600, only while there is no account) and `audit/audit.jsonl` with up to three rotated files (0600, at most 4 MiB in all). See [`AUTH.md`](AUTH.md#files-and-modes). |
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
| `required tools are missing: ...` | Install them, for example `sudo apt install curl openssl`; `flock` is part of `util-linux`. |
| `another update or installer run is in progress` | Another installer (or, later, an update) holds the lock on `/var/lib/oarfish-update`. Nothing was changed; run the installer again when the other run has finished. |
| `... must be owned by root:root with mode 0700` | `/var/lib/oarfish-update` exists with another owner or mode. It is security relevant: look at who created it, fix or remove it, run again. Nothing was changed. |
| `... is a link to ..., not to ...` / `... is not a link to ... (a foreign file)` | A file or link of your own sits where the helper layout needs a link. The installer does not touch it. Move it away, or decide the layout is not wanted. Nothing was changed. |
| `going back to a release without the helper is not supported` | A legacy release on a device that has the helper layout. Install a release with the helper, or `--uninstall` first. |
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
- The unit file passes `systemd-analyze verify` on the CI runner, and so do `oarfish.service` and the three helper units **as the installer links them** (`scripts/verify-units.sh --installed`, run on a fake root after a components installation; because `systemd-analyze --root` opens paths on the host, the absolute link targets are rewritten to equivalent relative links in a copy first).
- The components shape (G6.3b), against the fake root with the stub `systemctl`: both bundle shapes are accepted and anything else is rejected before a change (extra, missing or wrong-type entries, a manifest that disagrees with the shape, a helper that is not an ARM64 ELF); exact link targets, modes and relative wants links; wants and unit links that still resolve after old release and component directories are removed; the order of the `systemctl` calls (`daemon-reload` before any start, the helper service stopped before the socket is restarted, no `enable`, the recover unit never started, nothing restarted on an identical re-run) for a fresh install, an upgrade with new helper bytes, an upgrade with identical bytes and a rollback; `known-good` moving only after the health check; the lock (a held lock, a second installer, free after success and after failure, the same object for uninstall); migration of an alpha-style installation with a drop-in; refusals for foreign files and links, an immutable component set and a state directory with a wrong owner or mode; a legacy release on a device with the helper layout; uninstall, purge and dry run for the layout. The stub refuses to restart the socket while the helper service is active, as ADR 0010 A9 describes systemd, so the order is exercised; it is still a stub.

**Verified on hardware (one device, see [Hardware test results](#hardware-test-results)):**

- Gate G3: dry run and install on a Raspberry Pi Zero 2 W (image reported as Raspberry Pi OS Lite 64-bit), the unit file started by a real systemd (257), service active and enabled, start after a reboot, `/healthz`, the GUI through an SSH tunnel, and one process memory reading.
- Gate G4 (`v0.1.0-alpha.3`): in-place upgrade from `v0.1.0-alpha.2` with the stamped installer; detection of the board, SoC family, OS adapter, kernel, memory, all seven capabilities, CPU temperature and boot medium (`mmc`); support status Compatible with the profile `raspberry-pi-zero-2-w`; the "Gerät" page in a browser through an SSH tunnel; and a second idle `VmRSS` reading (9212 kB). The unchanged unit file's sandbox did not block detection.
- Gate G5 (`v0.1.0-alpha.4`): in-place upgrade from `v0.1.0-alpha.3` with the stamped installer; the unchanged unit file's sandbox did not block any file the dashboard reads; dashboard values against the device's own tools (AT-008); idle CPU and `VmRSS` with no browser, with one Safari tab and with the tab hidden; no writes by the process (AT-022); no swap activity (ORB-NFR-007); a service restart with the tab open (the owner reported expected behaviour); Home in Safari through an SSH tunnel. Details in [Gate G5: dashboard](#gate-g5-dashboard).
- UI foundation (`v0.1.0-alpha.5`): in-place upgrade from `v0.1.0-alpha.4` with the stamped installer (dry run, installation, signature OK, SHA-256 OK, service healthy); Home and "Gerät" in Safari (light and dark), Chrome and Firefox through an SSH tunnel with the layout as intended and the indicator on "Live"; an empty alert list on the idle device (1.5 MiB of 415 MiB in swap raised no alert, none for the Compatible tier); the tunnel interface with operstate `unknown` in neutral grey; keyboard-only use in Chrome or Firefox, as reported by the owner. Details in [UI foundation](#ui-foundation-v010-alpha5).

**Measured on hardware, G5.1 closeout (one device; the evidence the owner reviewed before Gate G5.1 passed on 2026-11-02):** Access control and audit (`v0.1.0-alpha.6`): in-place upgrade from `v0.1.0-alpha.5` with the stamped installer into setup mode; the state directory 0700 after the update; setup, login, logout, the rate limit, a password change, restart behaviour and lost-password recovery; the session cookie in Safari, Chrome and Firefox over `http://127.0.0.1`; the Argon2id cost; memory readings including one after 24 h 41 min; idle CPU; no test password or token in the journal or the audit file; the audit write load over 24 h 41 min. Details in [Access control and audit](#access-control-and-audit-v010-alpha6) and [`AUTH.md`](AUTH.md#results-on-the-reference-device-v010-alpha6).

**Observed on hardware, G6.3 owner stop (one device, a test release with a throwaway key, never published; not Verified):** the components install over a `v0.1.0-alpha.6` device with the layout as designed, socket activation with the idle exit and the refusal of other users, the `systemd-analyze security` rating of the helper units, the relative wants links after the old release directory was removed and a reboot, the boot order of the recover unit, an installer re-run that changed nothing, the `SIGKILL` recovery path, and uninstall without `--purge`. Details and the list of what was not exercised in [Results on the reference device (G6.3)](#results-on-the-reference-device-g63).

**Not verified:**

- **Gate G5 (dashboard), what the hardware runs did not cover**: screen-reader use, keyboard use in Safari, the dark theme in Chrome and Firefox, the lost-connection display of the new UI on the device, four simultaneous clients, CPU usage percent and network counters against a second tool, and long-running behaviour. (Idle CPU and `VmRSS` were read again with `v0.1.0-alpha.6`.) See [`DASHBOARD.md`](DASHBOARD.md#verification-status) and [`UI.md`](UI.md#verification-status).
- **Ubuntu Server and any other board or image** have not run on real hardware. Container results are no proof for them. That includes device detection on Ubuntu (fixtures and the architecture test only), other Raspberry Pi models, and USB boot.
- `--uninstall --purge` has not been tried on the device (only in the containers and the CI tests); uninstall without `--purge` and running the installer again for the same version were observed once at the G6.3 owner stop. An upgrade from one version to the next was done on the device (G4 test); a rollback after a failed health check was not.
- A tampered or damaged download has not been tried on the device (only in the CI tests).
- The containers have **no systemd**: a small stand-in starts the binary. Only the one device above has run the unit file under a real systemd; other kernels and images may differ.
- The 512 MB memory budget under load and long-term memory use (beyond 24 h 41 min) are not tested. The write load was read over 24 h 41 min only, with a few events (a rotation of the audit file was never reached). The idle `VmRSS` readings (7020 kB, 9212 kB and 8480 kB for three releases, none for `v0.1.0-alpha.5`, and 8784 kB for `v0.1.0-alpha.6`) are not a budget test.
- The presence of OpenSSL 3 and the other tools on Ubuntu Server images, and on other versions of Raspberry Pi OS, is assumed from package information. One device had OpenSSL 3.5.7.
- `releases/latest/download/install.sh` is checked with the first stable release; the release addressed by tag was downloaded anonymously and verified for `v0.1.0-alpha.2` (2026-10-26) and for `v0.1.0-alpha.6` (see [RELEASING.md](RELEASING.md)).
- Key rotation and revocation are not designed (ADR 0005). The release key is inside the installer; if it ever changes, a new installer is needed.
- **The update helper layout (G6.3b) was observed on one device only, with a test release.** At the G6.3 owner stop (device observation, 2026-11-03: one Raspberry Pi Zero 2 W, Debian 13 aarch64, systemd 257, one image, one moment) a test release `0.1.0-alpha.6.1` with a throwaway key and a test copy of `install.sh` was installed; it was never published. The links, the wants links after a removed release directory and a reboot, socket activation, the idle exit, the boot order of the recover unit, a re-run that changed nothing and uninstall without `--purge` came out as designed (details in [Results on the reference device (G6.3)](#results-on-the-reference-device-g63)). Not exercised there: the production key path with the real installer, `--purge`, a first installation without a previous release, the rollback after a failed health check, Ubuntu, other Pi models, a cold power cycle and a crash loop up to the start limit. Everything else about the layout is verified only against a fake root and a stub `systemctl`, in the arm64 containers (no systemd) and, for the unit text, by `systemd-analyze verify` of the installed link layout on the CI runner.
- **The GUI update** (`v0.1.0-alpha.7` and later) was observed on one device in the G6.9 run (class D and T, rows F1 to F23 of the failure matrix in [`UPDATE.md`](UPDATE.md#failure-matrix-f1-to-f32) except F13; one image, one moment; not Verified). Not run: F13, the parts of rows marked "C only", F24 to F32 (G6.10: implemented with class C tests, not in a release, not run on a device; the device step follows), the steps of [When the update helper needs a person](#when-the-update-helper-needs-a-person) for `known_good_damaged`, screen reader, other boards and models, Ubuntu, behaviour over many days, four simultaneous clients.
- Two installer runs are guarded against each other by the lock on `/var/lib/oarfish-update` for the components shape (and for the legacy shape when that directory exists); a legacy-shape installer on a device without the directory takes no lock. The helper takes the same lock for `stage`, `discard`, `activate` and its recovery (G6.5, G6.6); that side is tested in CI only. Two installer runs at the same time were tested only against the fake root, not on a device, and a helper holding the lock against an installer run has not been tried on a device.
