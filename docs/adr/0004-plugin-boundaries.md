# ADR 0004: First-Party plugins and core boundaries

- Status: Accepted
- Date: 2026-10-22

## Context

Container engines, remote connectivity, and LLM integration are valuable but optional and introduce distinct security and resource profiles.

## Decision

Tailscale, Containers, and Oarfish Assist are optional First-Party plugins. The core remains operational without them. Installation, authentication, audit, hardware and OS detection, and self-update remain core capabilities.

## Consequences

Tailscale cannot be required for installation, updates, or recovery. Plugins receive explicit capabilities and no generic privileged shell access.
