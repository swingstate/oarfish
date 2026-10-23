# ADR 0003: Pull-based installation and updates

- Status: Accepted
- Date: 2026-10-22

## Context

Direct deployment from GitHub Actions to a private development device requires network reachability, secrets, and a self-hosted or gateway runner. This adds complexity that is not part of the eventual product experience.

## Decision

GitHub Actions performs CI, cross-builds, signing, and GitHub Release publication. The target device pulls releases over HTTPS. First installation uses a small installer script. Later updates are initiated through the Oarfish GUI.

There is no self-hosted runner and no direct GitHub-to-device SSH deployment.

## Consequences

The product and development flow share the same release path. Release verification, atomic activation, health checks, and rollback are core features.
