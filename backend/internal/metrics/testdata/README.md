# Metrics fixtures

Every directory here is a **synthetic** file tree that stands for the root file system of a device: `proc/stat` is `/proc/stat`, `sys/class/net/wlan0/operstate` is `/sys/class/net/wlan0/operstate`. The tests give a fixture to the metrics collector as its file source (an `fs.FS`) instead of `/`.

**These are not captures of real devices.** They were written by hand to look like what the Linux kernel provides. Their numbers are invented. Nothing here was copied from a Raspberry Pi or any other machine.

What the fixtures may contain, and nothing else (enforced by `TestFixturesContainNoPrivateData`, `backend/internal/platform/privacy_test.go`):

- the host name `oarfish-test`, in `proc/sys/kernel/hostname` (the live API shows the real host name; fixtures never hold a real one);
- IP addresses from the documentation ranges only: `192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24` and `2001:db8::/32`;
- no MAC addresses (`sys/class/net/*/address` does not exist here and the collector never reads it), serial numbers, machine ids, user names, environment, process lists or SSH data. `if_inet6` holds documentation-range addresses only. One of them has an interface identifier in EUI-64 form (a made-up one) so the tests can prove that such addresses, which would contain a MAC address, are dropped. Link-local and loopback IPv6 lines are not stored here; the tests add them in memory.

`phy80211` files stand in for a symlink (git stores them as regular files); only their existence is checked. `.keep` files exist so git keeps otherwise empty directories.

| Fixture | Stands for |
|---|---|
| `pi-zero-2w` | Raspberry Pi Zero 2 W style: 4 cores, 415 MiB, swap file, ext4 root plus vfat firmware partition, `cpu-thermal` zone (also visible as hwmon), `wlan0` with an IPv4 and a global IPv6 address, cpufreq with `ondemand` |
| `generic-arm64` | unknown ARM64 board: 8 cores, 8 GB, no swap, btrfs subvolumes and bind mounts of one NVMe, an exFAT stick under `/run/media/<user>/`, a mount that cannot be read, hwmon sensors only (NVMe with two labelled inputs, `cpu`), `eth0` with 1000 Mb/s and two kinds of derived IPv6 addresses, a down `wlan0`, a container bridge |
| `minimal` | procfs only: no cpufreq, no sensors, no mount table, no network directory, no `MemAvailable` |
| `hostile` | NUL bytes, escape sequences, invalid UTF-8, absurd and negative numbers, a space in an interface name |

Oversized files and thousands of mounts or sensors are not stored here; the tests generate them in memory.
