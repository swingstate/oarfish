# Detection fixtures

Every directory here is a **synthetic** file tree that stands for the root file system of a device: `proc/cpuinfo` is `/proc/cpuinfo`, `etc/os-release` is `/etc/os-release`, and so on. The tests give a fixture to the detectors as their file source (an `fs.FS`) instead of `/`.

**These are not captures of real devices.** They were written by hand to look like what the Linux kernel and the distributions provide. In particular:

- Device-tree `compatible` strings, `Model` lines, revision codes and the `/proc/sys/kernel/osrelease` values are written from knowledge of the kernel sources and of the G3 test report, not copied from a Raspberry Pi. They have **not been compared with a real Raspberry Pi Zero 2 W** yet (that comparison is a hardware step, see `docs/DETECTION.md`).
- `run/systemd/journal/socket` is an empty regular file standing in for a Unix socket (git cannot store sockets). Detection only checks that the name exists.
- `.gitkeep` files exist only so git keeps otherwise empty directories.

The fixtures contain **no** serial numbers, MAC addresses, machine ids, host names, user names or IP addresses. `TestFixturesContainNoPrivateData` (`backend/internal/platform/privacy_test.go`) enforces that. Tests that need such values (to prove they never leak) create them in memory, not as files here.

| Fixture | Stands for |
|---|---|
| `pi-zero-2w-raspios` | Raspberry Pi Zero 2 W Rev 1.0 (revision code 902120) with Raspberry Pi OS (Debian 13 base, `/etc/rpi-issue` present) |
| `pi-zero-2w-ubuntu` | the same hardware files, byte for byte, with Ubuntu 24.04 |
| `pi4-raspios` | Raspberry Pi 4 Model B, Raspberry Pi OS: a Raspberry Pi for which no board profile ships |
| `generic-arm64-debian` | unknown ARM64 board with a device tree, Debian 12, NVMe root, a sensor under hwmon only |
| `generic-arm64-ubuntu` | unknown ARM64 server without a device tree (DMI only), Ubuntu 24.04, SCSI/SATA root |
| `unknown-arm64-minimal` | unknown ARM64 with nothing but procfs, sysfs, systemd and `/usr/lib/os-release` |
| `non-arm` | x86_64 development machine (tests set `GOARCH=amd64`) |
| `broken-hostile` | NUL bytes, escape sequences, invalid UTF-8, unterminated quotes, absurd numbers |

Oversized files are not stored here; the tests generate them in memory.
