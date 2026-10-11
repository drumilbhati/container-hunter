# System Architecture & Design

Container Hunter is compiled as a **single, lightweight Go binary** running entirely in-process on the local host. It avoids all internal IPC, gRPC, or socket-based service overhead, coordinating components via direct Go interfaces, method calls, and goroutines.

---

## High-Level Design (HLD)

```mermaid
flowchart TD
    %% Styling Classes
    classDef client fill:#1f6feb,stroke:#58a6ff,stroke-width:2px,color:#ffffff;
    classDef orch fill:#8957e5,stroke:#bc8cff,stroke-width:2px,color:#ffffff;
    classDef checker fill:#238636,stroke:#3fb950,stroke-width:2px,color:#ffffff;
    classDef target fill:#d29922,stroke:#f0883e,stroke-width:2px,color:#000000;

    subgraph App["Container Hunter (Single In-Process Binary)"]
        subgraph Presentation["Presentation"]
            TUI["TUI Client\n(Interactive Terminal UI)"]:::client
        end

        subgraph Orchestration["Orchestration"]
            HS["HunterService\n(Dispatcher & Reconciliation Engine)"]:::orch
        end

        subgraph Inspection["Concurrent Inspection (Goroutines)"]
            CC["containerd Checker\n(In-process Go client)"]:::checker
            FC["Linux Filesystem Checker\n(In-process Go scanner)"]:::checker
        end
    end

    subgraph Host["Host Operating System"]
        CD["containerd Runtime\n(Local Runtime Metadata)"]:::target
        PROC["Linux Mount Tables\n(/proc/*/mountinfo)"]:::target
        FS["OverlayFS Snapshots\n(/var/lib/containerd/...)"]:::target
    end

    %% Interactions
    TUI -->|"Go function call"| HS
    HS -->|"Return scan results"| TUI

    HS -->|"goroutine"| CC
    HS -->|"goroutine"| FC

    CC -->|"RuntimeInventory"| HS
    FC -->|"HostInventory"| HS

    CC -.->|"Inspect metadata"| CD
    FC -.->|"Read mount tables"| PROC
    FC -.->|"Scan snapshot directories"| FS

    style App fill:#161b22,stroke:#30363d,stroke-width:2px,color:#c9d1d9
    style Host fill:#161b22,stroke:#30363d,stroke-width:2px,color:#c9d1d9
    style Presentation fill:#0d1117,stroke:#21262d,stroke-width:1px,color:#c9d1d9
    style Orchestration fill:#0d1117,stroke:#21262d,stroke-width:1px,color:#c9d1d9
    style Inspection fill:#0d1117,stroke:#21262d,stroke-width:1px,color:#c9d1d9
```

---

## Sequence / Interaction Flow

```mermaid
sequenceDiagram
    autonumber
    actor User as User / Admin
    participant TUI as TUI (Client)
    participant HS as HunterService
    participant CC as containerd Checker
    participant FC as Linux Filesystem Checker

    User->>TUI: Trigger Scan
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
    TUI-->>User: Render formatted leak table
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
