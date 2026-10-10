# Problem Statement: Container Resource Inconsistencies and Leaks

## 1. Context and Problem Definition

Container runtimes on modern Linux systems manage multi-layered abstractions to provide isolated execution environments. Runtimes such as `containerd` coordinate higher-level metadata (containers, tasks, image manifests, content stores, snapshotter keys) with lower-level host primitives (OverlayFS mounts, block storage directories, network namespaces, and cgroups).

During normal container lifecycles, resource allocations follow strict transactional workflows:
1. An image layer is unpacked into a snapshotter directory.
2. An active container snapshot (upper/work directory) is prepared.
3. OverlayFS mounts are established in host mount namespaces.
4. Processes are spawned within target namespaces.
5. Upon container removal, processes are terminated, OverlayFS targets are unmounted, snapshot directories are deleted, and metadata records are removed.

However, container lifecycle routines are vulnerable to abrupt failure modes, including daemon crashes (e.g., `SIGKILL` or panic during `containerd`), kernel panics, abrupt host restarts, out-of-memory (OOM) kills of runtime helpers, and host mount lock contention. When an operation is interrupted between host state mutation and metadata commit (or vice versa), state drift occurs.

---

## 2. Why Runtime Metadata Alone Is Insufficient

Container runtimes treat their internal database (e.g., `meta.db` in BoltDB for `containerd`) as their primary source of truth. Diagnostic tools (`ctr`, `crictl`, `nerdctl`, or Docker CLI) query this metadata layer over gRPC APIs to report active containers, images, and disk usage.

This architectural decoupling introduces a fundamental blind spot:

- **Unlinked Host Artifacts:** If `containerd` deletes a metadata entry for a snapshot but is terminated before `rmdir` removes `/var/lib/containerd/io.containerd.snapshotter.v1.overlayfs/snapshots/<ID>`, the metadata store reports zero resources, while gigabytes of data remain on the host disk.
- **Orphaned Mounts:** If an unmount operation (`umount`) fails or is interrupted, the kernel mount table (`/proc/self/mountinfo`) retains the OverlayFS mount point. Standard garbage collection routines relying on API queries will skip host mount table inspection, leaving mounts dangling indefinitely.
- **Lost Lease Pointers:** Metadata corruption or aborted container creation can leave anonymous snapshots on disk with missing parent or lease records, rendering them invisible to standard `containerd garbage collection` routines.

Therefore, relying exclusively on runtime API metadata is insufficient for detecting host-level resource exhaustion.

---

## 3. Resource Lifecycle Taxonomy

To prevent imprecise claims, Container Hunter establishes a three-tier taxonomy for host-level resource anomalies:

| Term | Definition | Diagnostic Characteristics |
| :--- | :--- | :--- |
| **Suspected Orphan** | A host resource (directory or mount) that lacks a direct reference in the current active runtime metadata query. | Requires further validation to ensure it is not part of a concurrent, in-flight container lifecycle or held by an active lease. |
| **Confirmed Inconsistency** | A structural discrepancy verified between runtime metadata and host kernel/filesystem observations across multiple correlation checks. | A host snapshot directory exists whose recorded key is absent from metadata, or a metadata key points to a non-existent host path. |
| **Proven Resource Leak** | A confirmed inconsistency that is demonstrably unreferenced by any active task, image layer, or process, and cannot be reclaimed by standard runtime garbage collection. | An unreferenced snapshot directory or dangling mount with no active process attachments, no lease locks, and age exceeding execution thresholds. |

---

## 4. Primary Research Question

> **How accurately and efficiently can cross-layer state reconciliation detect orphaned container resources following lifecycle failures, without misclassifying legitimately retained or shared resources?**

---

## 5. Explicit Assumptions and Constraints

1. **Host Environment:** Linux operating system with kernel version 5.x or 6.x.
2. **Container Runtime Target:** `containerd` (v1.6+) utilizing the default `overlayfs` snapshotter plugin.
3. **Privilege Boundary:** Execution requires elevated privileges (`root` or `CAP_SYS_ADMIN` / `CAP_DAC_OVERRIDE`) to access `/proc/mountinfo` across host process namespaces and inspect `/var/lib/containerd`.
4. **Read-Only Invariant:** Analysis must occur without mutating host filesystems, sending signal kills, or invoking runtime delete APIs.
5. **Single-Node Scope:** State reconciliation is performed locally on a single Linux host node.

---

## 6. Scope Boundaries

### In Scope
- Reconciliation between `containerd` metadata (API/BoltDB) and host snapshot directories (`/var/lib/containerd/io.containerd.snapshotter.v1.overlayfs`).
- Parsing and correlation of Linux mount tables (`/proc/self/mountinfo` and `/proc/*/mountinfo`).
- Disk space impact estimation for confirmed leaks.
- Classification of findings with explicit evidence chains.

### Out of Scope
- Automated remediation, resource deletion, or unmounting.
- Kubernetes CRI cluster-level reconciliation (e.g., Kubelet state vs APIServer state).
- Multi-node distributed state inspection.
- Non-containerd runtimes (Docker Engine/dockerd, CRI-O, Podman) in the initial MVP phase.
- Network namespace, veth pair, or iptables rule leak detection.

---

## 7. Intended Outcome

The outcome of this project is a defensible forensic framework, open research data, and a read-only Go diagnostic tool that demonstrates whether cross-layer state correlation can reliably detect hidden container resource leaks on Linux host systems.

---

## Related Documentation

- [README](../README.md)
- [Scope and Requirements](scope-and-requirements.md)
