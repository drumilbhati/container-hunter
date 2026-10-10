# Container Hunter

Container Hunter is a read-only tool for Linux servers that finds leftover container files and mounts. 

When container runtimes like `containerd` crash, experience sudden server reboots, or encounter mount errors, they can leave behind hidden files, folders, and active mounts that eat up disk space. Container Hunter compares what `containerd` *thinks* is running against what is *actually* on your Linux filesystem to find, categorize, and report these resource leaks with clear evidence.

---

## Why Is This Needed?

Container tools manage resources across a few different layers: internal databases, image snapshot folders, and Linux kernel mounts (like OverlayFS). Built-in cleanup tools rely on their internal databases to decide what to delete. However, when things go wrong midway:

- Database records might get deleted while the actual folders or mounts stay on disk.
- Snapshot folders can lose their tracking links while still taking up gigabytes of storage.
- Leftover mounts can lock up filesystems and prevent normal cleanup.

Because standard tools like `ctr` or `crictl` only check internal databases, they miss these leftover files. Container Hunter bridges this gap by directly inspecting host filesystems and mount tables.

---

## Main Question

> **Can we reliably and quickly find leftover container files by comparing system mounts and disk folders with runtime metadata, without accidentally flagging files that are still being used?**

---

## Key Features and Scope

- **Primary Goal:** Detect, categorize, and provide evidence for leftover container files and mounts.
- **Supported Runtime:** `containerd` (v1.6+).
- **Supported OS:** Linux (Kernel 5.x / 6.x).
- **What It Checks:** OverlayFS snapshot folders (`/var/lib/containerd/...`) and Linux mount tables (`/proc/.../mountinfo`).
- **Safety First:** Strictly **read-only**. It will **never** delete files or unmount folders automatically.
- **Built With:** Go.

---

## Project Status

> [!IMPORTANT]
> **Current Status: Documentation & Planning Phase.**
> This repository currently contains problem definitions, requirements, and design goals. Code implementation is coming soon.

---

## Documentation Index

- [Problem Statement](docs/problem-statement.md) — Why resource leaks happen, how standard tools miss them, and how we define leak types.
- [Scope and Requirements](docs/scope-and-requirements.md) — Supported environments, features, performance goals, and safety rules.

---

## Safety Statement

Container Hunter is strictly a **diagnostic tool**. It runs in **read-only mode** and will **never** modify files, delete folders, or unmount drives. It provides clear, verifiable evidence so system administrators and operators can make safe decisions.