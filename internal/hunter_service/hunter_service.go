package hunterservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	containerdchecker "github.com/drumilbhati/container-hunter/internal/containerd_checker"
	fschecker "github.com/drumilbhati/container-hunter/internal/fs_checker"
	"golang.org/x/sync/errgroup"
)

// AnomalyCategory defines the classification level of a detected discrepancy.
type AnomalyCategory string

const (
	// CategorySuspectedOrphan indicates a disk resource not tracked by runtime metadata,
	// but within the grace period or potentially in-flight.
	CategorySuspectedOrphan AnomalyCategory = "Suspected Orphan"

	// CategoryConfirmedInconsistency indicates an unequivocal state mismatch between runtime and host.
	CategoryConfirmedInconsistency AnomalyCategory = "Confirmed Inconsistency"

	// CategoryProvenResourceLeak indicates an unlinked, aged resource with zero runtime tracking and zero active mounts.
	CategoryProvenResourceLeak AnomalyCategory = "Proven Resource Leak"
)

// DetectedAnomaly holds complete forensic evidence for an identified resource mismatch.
type DetectedAnomaly struct {
	SnapshotID   string          `json:"snapshot_id"`
	Path         string          `json:"path"`
	Category     AnomalyCategory `json:"category"`
	SizeBytes    int64           `json:"size_bytes"`
	InodeCount   int64           `json:"inode_count"`
	ActiveMounts []string        `json:"active_mounts"`
	CheckedAt    time.Time       `json:"checked_at"`
	Age          time.Duration   `json:"age"`
	Reason       string          `json:"reason"`
	HasLease     bool            `json:"has_lease"`
	HasContainer bool            `json:"has_container"`
}

// ScanReport aggregates metrics, inventories, and detected anomalies produced by a scan run.
type ScanReport struct {
	ScanTime                time.Time                           `json:"scan_time"`
	Duration                time.Duration                       `json:"duration"`
	TotalSnapshotsOnDisk    int                                 `json:"total_snapshots_on_disk"`
	TotalSnapshotsInRuntime int                                 `json:"total_snapshots_in_runtime"`
	TotalActiveMounts       int                                 `json:"total_active_mounts"`
	TotalDiskUsageBytes     int64                               `json:"total_disk_usage_bytes"`
	WastedDiskSpaceBytes    int64                               `json:"wasted_disk_space_bytes"`
	WastedInodes            int64                               `json:"wasted_inodes"`
	Anomalies               []DetectedAnomaly                   `json:"anomalies"`
	RuntimeInventory        *containerdchecker.RuntimeInventory `json:"runtime_inventory"`
	HostInventory           *fschecker.HostInventory            `json:"host_inventory"`
}

// Service orchestrates concurrent host and runtime inspection, reconciling states to classify leaks.
type Service struct {
	containerdInspector containerdchecker.ContainerdInspector
	fsInspector         fschecker.FilesystemInspector
	gracePeriod         time.Duration
}

// Option provides configuration options for Service.
type Option func(*Service)

// WithGracePeriod configures the minimum age required before classifying an orphan as a proven leak.
func WithGracePeriod(d time.Duration) Option {
	return func(s *Service) {
		s.gracePeriod = d
	}
}

// NewService creates a new HunterService orchestration instance.
func NewService(cInspector containerdchecker.ContainerdInspector, fInspector fschecker.FilesystemInspector, opts ...Option) *Service {
	s := &Service{
		containerdInspector: cInspector,
		fsInspector:         fInspector,
		gracePeriod:         5 * time.Minute,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// StartScan triggers concurrent runtime and filesystem inspection, reconciling and diffing inventories.
func (s *Service) StartScan(ctx context.Context) (*ScanReport, error) {
	startTime := time.Now()

	g, gCtx := errgroup.WithContext(ctx)

	var runtimeInv *containerdchecker.RuntimeInventory
	var hostInv *fschecker.HostInventory

	// Concurrently query containerd runtime metadata
	g.Go(func() error {
		var err error
		runtimeInv, err = s.containerdInspector.Inspect(gCtx)
		if err != nil {
			return fmt.Errorf("containerd inspection failed: %w", err)
		}
		return nil
	})

	// Concurrently inspect host filesystem and mount tables
	g.Go(func() error {
		var err error
		hostInv, err = s.fsInspector.Inspect(gCtx)
		if err != nil {
			return fmt.Errorf("filesystem inspection failed: %w", err)
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	report := s.reconcile(startTime, runtimeInv, hostInv)
	return report, nil
}

// reconcile performs diffing and anomaly classification between runtime and filesystem inventories.
func (s *Service) reconcile(startTime time.Time, rInv *containerdchecker.RuntimeInventory, hInv *fschecker.HostInventory) *ScanReport {
	now := time.Now()

	report := &ScanReport{
		ScanTime:                startTime,
		Duration:                time.Since(startTime),
		TotalSnapshotsOnDisk:    len(hInv.SnapshotDirs),
		TotalSnapshotsInRuntime: len(rInv.KnownSnapshotIDs),
		TotalActiveMounts:       len(hInv.OverlayMounts),
		TotalDiskUsageBytes:     hInv.TotalDiskUsageBytes,
		Anomalies:               make([]DetectedAnomaly, 0),
		RuntimeInventory:        rInv,
		HostInventory:           hInv,
	}

	// Index active mounts by snapshot path/ID
	mountsBySnapID := make(map[string][]string)
	for _, mount := range hInv.OverlayMounts {
		referencedIDs := extractSnapshotIDsFromMount(mount)
		for _, id := range referencedIDs {
			mountsBySnapID[id] = append(mountsBySnapID[id], mount.MountPoint)
		}
	}

	// Index runtime leases and containers as sets
	activeLeases := make(map[string]bool, len(rInv.ActiveLeaseIDs))
	for _, l := range rInv.ActiveLeaseIDs {
		activeLeases[l] = true
	}

	activeContainers := make(map[string]bool, len(rInv.ActiveContainerIDs))
	for _, c := range rInv.ActiveContainerIDs {
		activeContainers[c] = true
	}

	// 1. Check every snapshot folder physically present on disk
	for snapID, diskSnap := range hInv.SnapshotDirs {
		// Resolve metadata key from metadata.db if mapped, or direct ID
		runtimeKey := snapID
		if mappedKey, ok := hInv.DirIDToSnapshotKey[snapID]; ok && mappedKey != "" {
			runtimeKey = mappedKey
		}

		_, knownInRuntime := rInv.KnownSnapshotIDs[runtimeKey]
		if !knownInRuntime {
			_, knownInRuntime = rInv.KnownSnapshotIDs[snapID]
		}

		activeMounts := mountsBySnapID[snapID]
		hasLease := activeLeases[runtimeKey] || activeLeases[snapID]

		// Check container association (direct container name, or task mount rootfs)
		hasContainer := activeContainers[runtimeKey] || activeContainers[snapID]
		if !hasContainer && hInv.ActiveContainerMounts != nil {
			for _, cID := range hInv.ActiveContainerMounts[snapID] {
				if activeContainers[cID] {
					hasContainer = true
					break
				}
			}
		}

		age := now.Sub(diskSnap.ModTime)

		// A snapshot is legitimate if known in runtime OR attached to an active container
		if !knownInRuntime && !hasContainer {
			anomaly := DetectedAnomaly{
				SnapshotID:   snapID,
				Path:         diskSnap.Path,
				SizeBytes:    diskSnap.SizeBytes,
				InodeCount:   diskSnap.InodeCount,
				ActiveMounts: activeMounts,
				CheckedAt:    now,
				Age:          age,
				HasLease:     hasLease,
				HasContainer: hasContainer,
			}

			if hasLease || age < s.gracePeriod {
				// Recently created or holds an active lease
				anomaly.Category = CategorySuspectedOrphan
				anomaly.Reason = fmt.Sprintf("Directory exists on disk but is absent from containerd snapshot database (Age: %s, GracePeriod: %s)", age.Round(time.Second), s.gracePeriod)
			} else if len(activeMounts) > 0 {
				// Dangling active mount with missing metadata
				anomaly.Category = CategoryConfirmedInconsistency
				anomaly.Reason = fmt.Sprintf("Dangling OverlayFS mount actively attached (%s) without corresponding containerd snapshot metadata", strings.Join(activeMounts, ", "))
			} else {
				// Proven resource leak: untracked, unmounted, expired grace period
				anomaly.Category = CategoryProvenResourceLeak
				anomaly.Reason = "Unreferenced snapshot directory on disk: absent from containerd metadata, zero active leases, zero active mounts, and exceeds safety grace period"
				report.WastedDiskSpaceBytes += diskSnap.SizeBytes
				report.WastedInodes += diskSnap.InodeCount
			}

			report.Anomalies = append(report.Anomalies, anomaly)
		}
	}

	// 2. Check for "phantom" snapshots: present in containerd metadata but missing on disk
	for snapKey, meta := range rInv.KnownSnapshotIDs {
		expectedDirID := snapKey
		if mappedDir, ok := hInv.SnapshotKeyToDirID[snapKey]; ok && mappedDir != "" {
			expectedDirID = mappedDir
		}

		_, existsDirect := hInv.SnapshotDirs[snapKey]
		_, existsMapped := hInv.SnapshotDirs[expectedDirID]

		if !existsDirect && !existsMapped {
			report.Anomalies = append(report.Anomalies, DetectedAnomaly{
				SnapshotID: snapKey,
				Path:       "",
				Category:   CategoryConfirmedInconsistency,
				CheckedAt:  now,
				Reason:     fmt.Sprintf("containerd database tracks snapshot key (Namespace: %s, Kind: %s), but physical folder is missing on disk", meta.Namespace, meta.Kind),
			})
		}
	}

	return report
}

// extractSnapshotIDsFromMount extracts snapshot directory identifiers referenced in lowerdir, upperdir, or workdir.
func extractSnapshotIDsFromMount(m fschecker.OverlayMountInfo) []string {
	idsMap := make(map[string]bool)

	allDirs := append([]string{}, m.LowerDirs...)
	if m.UpperDir != "" {
		allDirs = append(allDirs, m.UpperDir)
	}
	if m.WorkDir != "" {
		allDirs = append(allDirs, m.WorkDir)
	}

	for _, dir := range allDirs {
		parts := strings.Split(dir, "/")
		for i, part := range parts {
			if part == "snapshots" && i+1 < len(parts) {
				id := parts[i+1]
				if id != "" {
					idsMap[id] = true
				}
			}
		}
	}

	result := make([]string, 0, len(idsMap))
	for id := range idsMap {
		result = append(result, id)
	}
	return result
}
