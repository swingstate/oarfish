# ADR 0001: SBC and embedded Linux product focus

- Status: Accepted
- Date: 2026-10-22

## Context

A generic Linux management platform would compete on a broad feature surface and dilute the product. Orbit is intended to be small, device-aware, and useful on constrained ARM systems.

## Decision

Orbit targets ARM SBCs, edge devices, and small embedded Linux appliances. Raspberry Pi Zero 2 W is the MVP resource baseline and initial reference device. Generic enterprise server management is not the primary product category.

## Consequences

Resource budgets, thermal state, boot media, storage wear, Wi-Fi stability, and board support status are first-class concerns. Additional board families are qualified after the Raspberry Pi MVP.
