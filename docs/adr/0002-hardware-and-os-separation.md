# ADR 0002: Separate hardware and operating-system detection

- Status: Accepted
- Date: 2026-10-22

## Context

A Raspberry Pi can run Raspberry Pi OS, Ubuntu Server, or another distribution. Tying hardware knowledge to one distribution would prevent clean Ubuntu support and produce brittle path assumptions.

## Decision

Hardware profiles and OS adapters are independent. `/etc/os-release` identifies the OS. Device Tree, procfs, sysfs, hwmon, and other hardware sources identify the device. Features are enabled through capability detection.

Raspberry Pi OS Lite 64-bit is the initial verified OS. Ubuntu Server LTS ARM64 is a mandatory future target. Ubuntu Core is outside the MVP.

## Consequences

The code must not hardcode the `pi` user, `/home/pi`, one boot path, or one network backend. Raspberry Pi-specific tools are optional enhancement providers only.
