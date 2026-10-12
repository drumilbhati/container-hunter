# Container Hunter

Container Hunter is a read-only diagnostic tool for Linux servers that uncovers leftover container files and dangling mounts.

When container runtimes like `containerd` crash, experience abrupt server reboots, or encounter mount failures midway through cleanup, they can leave behind untracked snapshot directories and active kernel mounts that silently consume gigabytes of disk space. Container Hunter reconciles what `containerd` *thinks* is running against what is *actually* present on the Linux filesystem to find, categorize, and report these resource leaks with verifiable forensic evidence.

---

## Why Is This Needed?

Container tools manage state across distinct layers: internal metadata databases (e.g. `meta.db`), image snapshot folders, and Linux kernel mounts (OverlayFS). Built-in cleanup commands rely exclusively on internal database records to decide what to prune. When disruptions occur:

- Database records can be removed while physical folders or mounts remain locked on disk.
- Snapshot folders can lose their tracking links or leases while continuing to eat up storage.
- Leftover mounts in `/proc/mountinfo` can lock filesystem resources and prevent normal garbage collection.

Because standard tools like `ctr` or `crictl` only query internal databases, they miss these leaks. Container Hunter bridges this gap by directly cross-referencing runtime metadata against physical host directories and kernel mount tables.

---

## Main Question

> **Can we reliably and quickly find leftover container files by comparing system mounts and disk folders with runtime metadata, without accidentally flagging files that are still being used?**

---

## Key Features

- **Concurrent Inspection:** Inspects `containerd` runtime metadata and host mount tables simultaneously using Go goroutines.
- **Three-Tier Taxonomy:** Classifies findings into **Proven Resource Leaks**, **Confirmed Inconsistencies**, and **Suspected Orphans** (with configurable safety grace periods to eliminate false positives).
- **Interactive Terminal UI (TUI):** Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) for navigating anomalies, filtering categories, and inspecting detailed forensic dossiers.
- **Headless & JSON Modes:** Non-interactive ANSI tables for scripts/cron jobs, and structured JSON for observability and CI/CD pipelines.
- **Strictly Read-Only:** **Never** modifies files, deletes directories, or unmounts drives.

---

## Quickstart

### 1. Build from Source

Ensure Go 1.22+ is installed:

```bash
git clone https://github.com/drumilbhati/container-hunter.git
cd container-hunter
go build -o container-hunter ./cmd/container-hunter
```

### 2. Interactive TUI Mode (Default)

Launch the interactive interface on a Linux server running `containerd`:

```bash
sudo ./container-hunter
```

*(Elevated privileges / `sudo` are required to read `/proc/self/mountinfo` and inspect `/var/lib/containerd/...`).*

#### TUI Keyboard Shortcuts

| Key | Action |
| :--- | :--- |
| `↑` / `↓` or `j` / `k` | Navigate anomalies in the table |
| `Enter` | Open the **Forensic Dossier** for the selected anomaly |
| `Esc` | Return from the Forensic Dossier back to the table |
| `Tab` | Cycle through anomaly category filters |
| `1` | Show **All** anomalies |
| `2` | Filter to **Proven Resource Leaks** only (🔴) |
| `3` | Filter to **Confirmed Inconsistencies** only (🟡) |
| `4` | Filter to **Suspected Orphans** only (🔵) |
| `r` | Trigger an immediate **Re-scan** |
| `q` or `Ctrl+C` | Safely exit the application |

---

### 3. Headless Table Output (`--no-tui`)

For automated logging, remote SSH runs, or CI/CD jobs:

```bash
sudo ./container-hunter --no-tui
```

```text
Running Container Hunter scan (read-only)...

=== Scan Summary ===
Duration:         120ms
Disk Snapshots:   142
Runtime Keys:     138
Overlay Mounts:   12
Wasted Storage:   3.42 GB (3670016000 bytes)
Wasted Inodes:    48210
Anomalies Found:  4

CATEGORY               SNAPSHOT ID   SIZE       INODES   AGE    REASON
--------               -----------   ----       ------   ---    ------
Proven Resource Leak   1048          2.10 GB    24500    4d6h   Unreferenced snapshot directory on disk...
Proven Resource Leak   1052          1.32 GB    23710    2d1h   Unreferenced snapshot directory on disk...
Confirmed Inconsistency ghost-8      0 B        0        N/A    containerd database tracks snapshot key...
Suspected Orphan       1090          45.20 MB   812      1m20s  Directory exists on disk but is absent from...
```

---

### 4. JSON Output for Scripts & Pipelines (`--json`)

Dump findings as structured JSON for parsing with `jq` or external monitoring tools:

```bash
# Full report
sudo ./container-hunter --json | jq .

# Total wasted bytes
sudo ./container-hunter --json | jq '.wasted_disk_space_bytes'

# Filter snapshot IDs for proven leaks
sudo ./container-hunter --json | jq '.anomalies[] | select(.category == "Proven Resource Leak") | .snapshot_id'
```

---

## CLI Flags

| Flag | Shorthand | Default | Description |
| :--- | :--- | :--- | :--- |
| `--socket` | `-s` | `/run/containerd/containerd.sock` | Path to containerd UNIX domain socket |
| `--snapshots-dir` | `-d` | `/var/lib/containerd/io.containerd.snapshotter.v1.overlayfs/snapshots` | Path to host OverlayFS snapshot directory |
| `--mountinfo` | `-m` | `/proc/self/mountinfo` | Path to host mount table |
| `--grace-period` | `-g` | `5m` | Safety grace period before classifying orphans as proven leaks |
| `--no-tui` | — | `false` | Disable interactive TUI and print summary table to stdout |
| `--json` | — | `false` | Output results as structured JSON |
| `--version` | `-v` | `false` | Print version and exit |

---

## Documentation Index

- [Problem Statement](docs/problem-statement.md) — Why resource leaks occur, how standard tools miss them, and anomaly taxonomy.
- [Scope and Requirements](docs/scope-and-requirements.md) — Supported environments, features, performance goals, and safety rules.
- [Architecture & Design](docs/architecture.md) — Component responsibilities, request lifecycle, and orchestration flow.

---

## Safety Statement

Container Hunter is strictly a **diagnostic tool**. It runs in **read-only mode** and will **never** modify files, delete folders, or unmount drives. It provides clear, verifiable evidence so system administrators and operators can make safe remediation decisions.
