# Oarfish delivery roadmap

| Gate | Result |
|---|---|
| G0 | Pflichtenheft and technical baseline |
| G1 | Repository, skeleton, documentation, tests, and CI |
| G2 | Signed GitHub Release pipeline for Linux ARM64 |
| G3 | Pull-based installation script for Raspberry Pi OS |
| G4 | Oarfish Core, hardware, OS, and capability detection |
| G5 | Embedded dashboard with real metrics |
| (task) | Between G5 and G5.1, in this order, each in its own session and pull request and not a gate (D-C42): alert tuning (swap threshold, fewer irrelevant alerts), then a UI foundation task (appearance and structure only, no new functions) |
| G5.1 | Local access control and audit foundation. Scope per [ADR 0009](adr/0009-local-access-control-and-audit.md) (D-C45 to D-C48): Argon2id password hash, in-memory sessions, strict cookie and CSRF protection, setup token for the first account, rate limiting, own audit file, every `/api/v1` route behind a session; loopback only. Exit: state-changing endpoints reject requests without a valid session and without CSRF protection; audit entries contain no secrets; login verified on the Raspberry Pi. Also measured on the Pi: the password hash benchmark and the audit write load. Status: implemented and tested ([`AUTH.md`](AUTH.md)); the Raspberry Pi evidence (reference device, `v0.1.0-alpha.6`) was reviewed and G5.1 passed on 2026-11-02, with one open observation (an unexplained audit entry, see [`AUTH.md`](AUTH.md#open-observations-from-the-device-run)) |
| G6 | GUI self-update, verification, health check, and rollback; Update Helper as a separate privileged process. Design per [ADR 0010](adr/0010-update-helper.md) (accepted 2026-11-02, D-C53 to D-C63), delivered in the sub-steps G6.0 to G6.10 below, each with its own session, pull request and stop point. G6 closes only after G6.10 is accepted |
| G6.0 | Design stop: ADR 0010 accepted with the owner decisions, decisions recorded, specification 0.3.5, ADR 0009 amendment A6. Documentation only. Stop point: the owner says "merge" for the documentation pull request |
| G6.1 | Spike on the device, no product code: V1 to V4 (Raspberry Pi OS part), V6, V8, V10 to V13 on the device; V5, V7 and V9 are checked in G6.5, G6.9 and G6.2 (systemd behaviour of the hardened helper units, linked units, restart interplay, GitHub limits and redirects, clock). Status: the spike ran on the reference device with throwaway units, and the owner amended ADR 0010 from the findings (A1 to A8, D-C64). Stop point: the owner confirms or changes the hardening and the unit layout from the findings |
| G6.2 | Release tooling: bundle contract (helper binary and unit files), additive manifest fields, `bundlegen` checks and tests. Stop point: green dry-run release |
| G6.3 | Installer: installs and removes the helper, its units and the link layout, shared lock, tests with the fake root, `systemd-analyze verify` in CI. Delivered in two pull requests (D-C66): G6.3a the helper stub and the units (merged), G6.3b the installer, the lock and the helper in the default release bundle. Stop point: the owner runs the installer on the device, after G6.3b |
| G6.4 | Shared verification package (signature, checksums, manifest, SemVer, tar rules) with tamper, fuzz and replay tests. Stop point: green CI |
| G6.5 | Helper core: protocol, peer check, `status`, `stage`, `discard`, staging, state file, free space, cache file-type tests. Stop point: review of the privileged code |
| G6.6 | Helper activation: `activate`, `ack`, health check, rollback, boot recovery, retention, cooldown, expiry, fault hooks, crash tests. Stop point: review of the privileged code and its tests |
| G6.7 | Service side: GitHub client, check, download, advisory verification, helper client, routes (including the notice route, which the UI does not use yet, D-C68), audit amendment A6 in code, `/healthz` version and `pid`, `/readyz`. Stop point: review |
| G6.8 | Update page (Svelte). Stop point: the owner reviews the UI |
| G6.9 | Hardware evidence for the base update on the reference device, and documentation. Stop point: the owner reviews the evidence; G6 stays open |
| G6.10 | Replacement of the helper and its units by a GUI update (ADR 0010, U15): unit lint, component transaction, known-good copy, key rule, second confirmation. Until it is accepted, a release that changes the helper or a unit needs an installer run. Stop point: the owner decides whether G6 passes |
| (spike) S1 | Device spike, extended, no product code: wireless data (SSID, signal), storage type, write baseline, throttling files, cpufreq, the source of the DNS servers, feasibility of the operating system update display (D-C72, D-C74). Done: the owner ran it on 2026-11-06 on one device (one run, nothing Verified); the findings are in [ADR 0008](adr/0008-dashboard-metrics.md), "Amendment after S1", and D-C78 |
| (task) U1 | New UI shell (navigation of specification 7.2, "Dashboard" as the first entry, language choice, design tokens, colour baseline, logo mark). Done: PR #34 (merged); `docs/UI.md` was changed in that step. Open rest: the installed-memory text on the Gerät page, see N0 |
| (task) N0 | Default route and DNS display (ORB-NET-001, D-C72). Known sources (device observation, ADR 0008 amendment after S1): the default rows of `/proc/net/route` and `/proc/net/ipv6_route`, and `/etc/resolv.conf`; adding them to the read allow-list is a proposal of the amendment and is done in this task, with a test on the device under the unit hardening; images with a resolver stub need a fallback design (not tested). Also the installed-memory size field: `/api/v1/device` gives only the memory total today, so a field for the installed size must come from the detected board data or the profile; this lets the Gerät page say "Nur 512 MiB Arbeitsspeicher" instead of "etwa 512 MiB" (the open rest of U1). The implementation is not specified here |
| (task) N1 | Network page with signal strength (from `/proc/net/wireless`, device observation) and IP details (prefix, gateway, IPv6); after U1 and G6.8 (D-C72). The SSID only after the open decision: it needs netlink and `AF_NETLINK` in the unit, and the owner has not decided the timing (recommendation: together with D-D10) |
| (design stop) H1 | Dashboard history and settings store: intervals, write numbers, medium-dependent rhythm, file format, language setting (D-C73). Input from S1 (device observation, one run): the medium is an SD card, the idle write baseline is about 392 KB per hour; the cost of an atomic replace of a history file (data, journal, directory) is not measured and is part of H1. (task) H2 is the implementation, after H1 and U1 |
| G7 | systemd, journal, processes, reboot, and shutdown; Privilege Broker with a fixed allowlist. Journal and processes are reserve behind the MVP acceptance (ORB-LOG-001 is SOLL, D-C69). Does not start before G6.10 is accepted (D-C63) |
| (design stop) F1 | Files, read-only: the fixed roots (open), block list, size limit and acceptance criteria, with an ADR; after G7, container roots also after G10 (D-C75). (task) F2 is the implementation |
| G8 | Plugin runtime and permission model; broker capability check |
| G9 | Tailscale plugin; installation through the GUI by a typed broker action, with its own design stop (D-C75) |
| G10 | Docker and Podman plugin; the access of the unprivileged service to the engine is decided in a design stop before G10 (D-C75) |
| G11 | Oarfish Assist, read-only MVP |
| G12 | Ubuntu Server LTS ARM64 qualification |
| (design stop) D-D10 | LAN access and HTTPS, after G6.10 and before the MVP release and before any test with outside users. Direction decided (D-C70): self-signed certificate, installer option, never without TLS outside loopback; the design is open (certificate, host and origin checks, setup token handover, installer size budget). Not a gate; until then remote access is an SSH tunnel and the listen guard stays (D-C48) |
| (design stop) D-D12 (rest) | Key rotation: revocation of a leaked signing key and the key embedded in the helper. Alpha devices are installed again (D-C76). Touches the installer and the embedded key, so it runs next to D-D10; before G13 |
| G13 | Hardening and MVP acceptance |
| Post-MVP | Armbian, Rockchip, additional boards, and external plugins |

Implementation order after G8: G10, then G9, then G11; the identifiers stay unchanged (D-C75).

Reserve, which may follow the MVP acceptance (D-C69): the journal view and the process view (ORB-LOG-001 is SOLL, ORB-PRC-001). Later and not part of this plan: throttling and undervoltage warnings (ORB-DASH-004, task T1). Device observation from S1: undervoltage is readable from the `rpi_volt` hwmon alarm file (whether it is live or latched is not checked); a frequency cap is not available as a flag.

Each gate receives its own prompt for the implementation agent. A later gate must not be implemented early unless the specification is formally changed.
