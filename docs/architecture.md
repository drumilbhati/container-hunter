# System Architecture & Design

Container Hunter is compiled as a **single, lightweight Go binary** running entirely in-process on the local host. It avoids all internal IPC, gRPC, or socket-based service overhead, coordinating components via direct Go interfaces, method calls, and goroutines.

---

## High-Level Design (HLD)

```mermaid
flowchart TD
    subgraph App["Container Hunter (Single In-Process Go Binary)"]
        subgraph Presentation["Presentation"]
            TUI["TUI Client\n(Interactive Terminal UI)"]
        end

        subgraph Orchestration["Orchestration"]
            HS["HunterService\n(Dispatcher & Reconciliation Engine)"]
        end

        subgraph Inspection["Concurrent Inspection (Goroutines)"]
            CC["containerd Checker\n(In-process Go client)"]
            FC["Linux Filesystem Checker\n(In-process Go scanner)"]
        end
    end

    subgraph Host["Host Operating System"]
        CD["containerd Runtime\n(Local Runtime Metadata)"]
        PROC["Linux Mount Tables\n(/proc/*/mountinfo)"]
        FS["OverlayFS Snapshots\n(/var/lib/containerd/...)"]
    end

    TUI -->|"Go function call"| HS
    HS -->|"Return scan results"| TUI

    HS -->|"goroutine"| CC
    HS -->|"goroutine"| FC

    CC -->|"Return RuntimeInventory"| HS
    FC -->|"Return HostInventory"| HS

    CC -->|"Inspect metadata"| CD
    FC -->|"Read mount tables"| PROC
    FC -->|"Scan snapshot directories"| FS
```

---

## Sequence / Interaction Flow

```mermaid
sequenceDiagram
    participant TUI as TUI (Client)
    participant HS as HunterService
    participant CC as containerd Checker
    participant FC as Linux Filesystem Checker

    TUI->>HS: StartScan(ctx) [Method Call]
    par Concurrent In-Process Execution (goroutines)
        HS->>CC: Inspect(ctx)
        CC-->>HS: Runtime inventory
    and
        HS->>FC: Inspect(ctx)
        FC-->>HS: Host filesystem inventory
    end
    Note over HS: Reconcile, diff, and classify leaks
    HS-->>TUI: ScanReport [Return Value]
```

---

## Component Responsibilities

| Component | Responsibility |
| :--- | :--- |
| **TUI (Client)** | Renders terminal views, captures user input, and triggers scans. Invokes `HunterService` via direct Go method calls. |
| **HunterService** | Single orchestration layer. Dispatches both checkers concurrently across goroutines, reconciles runtime metadata against filesystem state, calculates wasted space, and returns the combined report. |
| **containerd Checker** | Local inspection package. Gathers active containers, image layers, snapshot keys, and leases directly using the containerd Go library. |
| **Linux Filesystem Checker** | Local inspection package. Reads host mount tables (`/proc/*/mountinfo`) and scans physical snapshot directories (`/var/lib/containerd/...`) using standard Go filesystem libraries. |

---

## Key Design Principles

1. **Single In-Process Binary:** No separate background daemons, microservices, gRPC servers, or IPC sockets. Everything runs in a single Go process.
2. **Concurrent & Non-Blocking:** `HunterService` spins up lightweight goroutines (using `sync.WaitGroup` or `errgroup`) to run both checks simultaneously.
3. **Decoupled via Interfaces:** Checkers implement simple Go interfaces, ensuring zero coupling between the TUI and inspection internals.
