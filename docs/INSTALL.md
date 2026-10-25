# Installing Oarfish

Oarfish runs on a 64-bit ARM Linux device with systemd. You download one script, read it, and run it. The script downloads the release from GitHub, checks the signature, and installs Oarfish as a service. No GitHub account, no token, no Docker, no Node.js, no Python.

**Status (Gate G3):** this installer has **not been run on a real Raspberry Pi, Raspberry Pi OS or Ubuntu Server image yet**. What was tested, and what was not, is listed at the end ([What is verified](#what-is-verified-and-what-is-not)). Oarfish today only shows a health page; there is no login and no device management yet (later gates).

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

Data point, not proof: the maintainer confirmed that OpenSSL 3 is present on one of their own devices. Nobody has confirmed it for a fresh Raspberry Pi OS Lite or Ubuntu Server image. If `openssl version` shows 1.x or the command is missing, install or upgrade the `openssl` package of your distribution first.

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

What this does and does not give you: the key is inside the script you downloaded, and the script is from the same GitHub repository as the key file, so comparing them only shows that the two copies agree. It does not protect against someone who controls the repository and replaces both. For that, compare the key id with a copy you got somewhere else, for example from a person you trust or from another machine. No second official channel exists yet. `install.sh` is also listed in the signed `checksums.txt`, so you can verify the script itself with minisign before you run it (see [RELEASING.md](RELEASING.md), "Verify a release anonymously").

## First start and opening the GUI

The service starts by itself after the installation and after every reboot (reboot behaviour is **not yet tested**, see below).

```bash
systemctl status oarfish          # should say "active (running)"
journalctl -u oarfish -e          # the log
curl http://127.0.0.1:8484/healthz   # {"status":"ok"}
```

Oarfish listens on `127.0.0.1:8484` only. That means: only programs on the device itself can reach it. This is on purpose. There is no login yet, so it must not be reachable from the network, and the installer never opens it.

**On the device itself** (with a browser, for example over a screen): open <http://127.0.0.1:8484>.

**From another computer**: use an SSH tunnel. Replace `USER` and `DEVICE` with your login name and the device's name or address:

```bash
ssh -L 8484:127.0.0.1:8484 USER@DEVICE
```

Leave that window open, then open <http://127.0.0.1:8484> in the browser on your own computer. If port 8484 is already in use there, take another local port: `ssh -L 9000:127.0.0.1:8484 USER@DEVICE` and open <http://127.0.0.1:9000>.

Do not edit `/etc/systemd/system/oarfish.service` to listen on another address. Reverse proxy, TLS and network access come in later gates, together with authentication. A re-run of the installer replaces the unit file with the one from the release. Local changes belong in a drop-in (`sudo systemctl edit oarfish`).

## Run it again

Running the installer again is safe.

- Same version again: it verifies everything again, finds that nothing differs, and changes nothing. It does not restart the service.
- Another version: the new release is installed beside the old one, the `current` link is switched, the service restarts, and the health check runs. Old release directories are kept (they are small); remove old ones by hand if you want the space back.
- A download or verification problem at any point: the existing installation stays exactly as it was.

This is the installer's job for the first installation. Updating from the GUI comes in a later gate (G6).

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
| `did not become healthy` | The files are installed but the service does not answer. Look at `journalctl -u oarfish`. If an older version was active, the installer went back to it. |

## What is verified, and what is not

**Verified automatically (CI, every change):**

- The installer functions against a fake root directory with a throwaway signing key, run with `dash` and with `bash`: wrong architecture, non-Linux, missing systemd, missing tools, LibreSSL and OpenSSL 1.1, a "verifier" that accepts everything, tampered `checksums.txt`, tampered and truncated signature, signature from another key, a valid prehashed signature, replayed signature, tampered and truncated bundle, tampered manifest, missing files, interrupted download (old installation untouched), bundles with extra files, links or an x86-64 binary, re-run without changes, upgrade, rollback after a failed health check, dry run, uninstall and purge. `scripts/test-installer.sh`.
- The release pipeline: `install.sh` is stamped with the version, listed in the signed `checksums.txt`, and a tampered or wrongly stamped `install.sh` is rejected by `scripts/verify-release.sh`. The bundle, manifest and `install.sh` are reproducible.
- End to end in plain `debian:12` and `ubuntu:24.04` **arm64** container images under qemu-user: signature verification with the image's OpenSSL, real `useradd`/`chown`, the real ARM64 binary started as the `oarfish` user, `/healthz` and the GUI answer, only `127.0.0.1:8484` listens, a second run changes nothing, uninstall and purge. Results, including which tools the minimal images already contained, are in the job summary of the "Installer in arm64 container" jobs.
- The unit file passes `systemd-analyze verify` on the CI runner.

**Not verified:**

- **Nothing has run on real hardware.** No Raspberry Pi Zero 2 W, no Raspberry Pi OS Lite image, no Ubuntu Server image.
- The containers have **no systemd**: a small stand-in starts the binary. The unit file has never been started by a real systemd. A setting in it that does not work on a given kernel would show up as a failed start; the installer would report the failed health check.
- Starting after a reboot (AT-007), the 512 MB memory budget, and write load on a microSD card are not tested.
- The presence of OpenSSL 3 and the other tools on fresh Raspberry Pi OS and Ubuntu Server images is assumed from package information, plus one data point from the maintainer. Container results are no proof for those images.
- The anonymous download of `install.sh` and the release from the real GitHub address is checked once, by hand, when the prerelease is published (see [RELEASING.md](RELEASING.md)).
- Key rotation and revocation are not designed (ADR 0005). The release key is inside the installer; if it ever changes, a new installer is needed.
- Two installers running at the same time are not guarded against.
