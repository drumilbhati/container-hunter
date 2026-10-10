# Container Hunter

Container Hunter is a forensic state-reconciliation tool and systems engineering investigation designed to detect orphaned container resources on Linux hosts. When container runtimes experience interrupted lifecycle operations—such as daemon crashes, abrupt host reboots, or unmount failures—inconsistencies can develop between the runtime's internal metadata and actual Linux kernel or host filesystem allocations. Container Hunter correlates runtime metadata with host mount tables and filesystem snapshots in a strictly read-only manner to identify, classify, and report leaked resources along with supporting evidence.

---

## Problem Overview and Motivation

Modern container engines manage resources across distinct abstractions: higher-level metadata databases, OCI runtime state, snapshotter directory structures, and Linux kernel mounts (such as OverlayFS). Standard garbage collection mechanisms rely on intact metadata chains to track resource ownership. However, when lifecycle routines fail midway:

- Metadata records may be purged while host directories or active mounts persist.
- Host snapshot directories may lose parent-child metadata pointers while retaining allocated disk blocks.
- Dangling mounts may keep underlying filesystems locked, preventing cleanup and consuming system resources.

Because runtime inspection commands (e.g., `containerd` API queries) depend on internal metadata stores, they are often blind to host-level leaks created by these structural disconnects. Container Hunter bridges this gap by cross-referencing host-level observations against runtime state.

---

## Research Question

> **How accurately and efficiently can cross-layer state reconciliation detect orphaned container resources following lifecycle failures, without misclassifying legitimately retained or shared resources?**

---

## Core Objectives and Scope

- **Primary Objective:** Provide forensic detection, classification, and clear evidence for orphaned container resources.
- **Target Runtime:** `containerd` (v1.6+).
- **Target OS:** Linux (Kernel 5.x / 6.x).
- **Inspection Scope:** `overlayfs` snapshotter state directories and host mount tables (`/proc/self/mountinfo`, `/proc/*/mountinfo`).
- **Operating Mode:** Strictly **read-only**. Automated cleanup or deletion is explicitly out of scope.
- **Implementation Language:** Go.

---

## Project Status

> [!IMPORTANT]
> **Current Status: Documentation & Problem Definition Phase.**
> This repository contains formal problem definitions, scope specifications, and requirements. No implementation code exists yet.

---

## Documentation Index

- [Problem Statement](docs/problem-statement.md) — Formal problem definition, metadata limitations, taxonomy, and research question.
- [Scope and Requirements](docs/scope-and-requirements.md) — Environment specs, functional/non-functional requirements, and explicit scope boundaries.

---

## Safety Statement

Container Hunter is designed as a **diagnostic tool only**. It executes in a non-mutating, read-only mode and will **never** perform automatic file deletion, unmounting, or resource cleanup. The tool provides human-verifiable evidence so that operators can make informed decisions.