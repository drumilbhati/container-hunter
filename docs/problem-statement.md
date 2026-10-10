# Problem Statement: Leftover Container Resources and Filesystem Leaks

## 1. Background and Problem Overview

Modern container engines on Linux (like `containerd`) coordinate two main things to run containers:
1. **Internal tracking data** (database records of images, containers, active locks, and snapshots).
2. **Actual system resources** (folders on disk, OverlayFS file mounts, network setups, and process limits).

When a container runs normally, everything happens in order:
1. Container image layers are unpacked into snapshot folders.
2. A working folder is created for the active container.
3. Linux mounts the layers together using OverlayFS.
4. Container processes start inside isolated namespaces.
5. When the container stops and gets deleted, processes stop, mounts are removed, snapshot folders are deleted, and internal database entries are cleaned up.

However, things don't always go smoothly. Container engines can crash unexpectedly, servers can reboot mid-cleanup, out-of-memory errors can kill helper programs, or file mounts can get stuck. When these interruptions happen midway through cleanup, the internal database and the actual files on disk get out of sync.

---

## 2. Why Built-In Container Tools Miss Leftover Files

Container tools rely on their internal database (like `meta.db` in `containerd`) as their single source of truth. Commands like `ctr`, `crictl`, or `nerdctl` ask this database what containers and images exist.

This design creates a major blind spot:

- **Uncleaned Disk Folders:** If `containerd` deletes a database entry but gets killed before it actually deletes the folder on disk (`/var/lib/containerd/...`), the tool reports zero used space while gigabytes of leftover files sit on your server.
- **Leftover Mounts:** If an unmount operation (`umount`) fails or gets interrupted, the Linux kernel still keeps the mount active in `/proc/self/mountinfo`. Built-in cleanup commands don't check Linux mount tables, so these mounts stay active indefinitely.
- **Lost Ownership Links:** Aborted container setups or database glitches can leave snapshot folders on disk without any parent link or lease, making them completely invisible to standard cleanup routines.

Because of this, relying only on container CLI tools or API queries isn't enough to catch hidden disk leaks.

---

## 3. How We Categorize Anomlies (Resource Taxonomy)

To avoid false alarms, Container Hunter categorizes disk and mount issues into three clear levels:

| Status Term | Simple Definition | What We Check |
| :--- | :--- | :--- |
| **Suspected Orphan** | A folder or mount on disk that isn't listed in the container runtime's active metadata. | Needs a second check to make sure it isn't part of a container that is currently being created or deleted. |
| **Confirmed Inconsistency** | A verified mismatch between runtime metadata and actual filesystem or mount table observations. | For example: a snapshot folder exists on disk but its ID isn't in `containerd`, or `containerd` lists a key whose folder is missing. |
| **Proven Resource Leak** | A confirmed mismatch that is clearly unused by any running process or image, and cannot be cleaned up by normal runtime tools. | A leftover folder or dangling mount with no active processes, no lock leases, and an age that exceeds our safety threshold. |

---

## 4. Main Goal

> **Can we reliably and efficiently detect leftover container files and mounts by cross-checking Linux system state against container metadata, without accidentally flagging files that are still legitimately in use?**

---

## 5. System Assumptions and Limits

1. **Operating System:** Linux with kernel 5.x or 6.x.
2. **Container Engine:** `containerd` (v1.6+) using the standard `overlayfs` snapshot plugin.
3. **Required Permissions:** Root or system admin privileges (`CAP_SYS_ADMIN` / `CAP_DAC_OVERRIDE`) to read `/proc/mountinfo` and check `/var/lib/containerd`.
4. **Safety Rule:** Operates strictly in **read-only** mode. It never deletes files, kills processes, or modifies system settings.
5. **Single Server Scope:** Checks one Linux server at a time.

---

## 6. What Is Included (and What Is Not)

### In Scope
- Comparing `containerd` metadata with snapshot folders on disk (`/var/lib/containerd/io.containerd.snapshotter.v1.overlayfs`).
- Reading and checking Linux mount tables (`/proc/self/mountinfo` and `/proc/*/mountinfo`).
- Estimating total wasted disk space from leftover files.
- Reporting findings clearly with supporting evidence.

### Out of Scope
- Automatic file deletion or unmounting.
- Kubernetes cluster-level checks (like Kubelet vs API server).
- Multi-server network scanning.
- Other container engines (Docker Engine, CRI-O, Podman) for the initial version.
- Leaked network interfaces or firewall rules.

---

## 7. Expected Outcome

A safe, read-only Go tool and clear diagnostic framework that proves whether cross-referencing system mounts and disk folders can reliably uncover hidden container leaks on Linux servers.

---

## Related Documentation

- [README](../README.md)
- [Scope and Requirements](scope-and-requirements.md)

