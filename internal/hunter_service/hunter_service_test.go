package hunterservice

import (
	"context"
	"errors"
	"testing"
	"time"

	containerdchecker "github.com/drumilbhati/container-hunter/internal/containerd_checker"
	fschecker "github.com/drumilbhati/container-hunter/internal/fs_checker"
)

type mockContainerdInspector struct {
	inv *containerdchecker.RuntimeInventory
	err error
}

func (m *mockContainerdInspector) Inspect(ctx context.Context) (*containerdchecker.RuntimeInventory, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.inv, nil
}

type mockFilesystemInspector struct {
	inv *fschecker.HostInventory
	err error
}

func (m *mockFilesystemInspector) Inspect(ctx context.Context) (*fschecker.HostInventory, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.inv, nil
}

func TestHunterService_AllHealthy(t *testing.T) {
	now := time.Now()
	cMock := &mockContainerdInspector{
		inv: &containerdchecker.RuntimeInventory{
			KnownSnapshotIDs: map[string]containerdchecker.SnapshotInfo{
				"101": {Name: "101", Kind: "Active", Namespace: "default"},
			},
			ActiveContainerIDs: []string{"ctr-1"},
		},
	}
	fMock := &mockFilesystemInspector{
		inv: &fschecker.HostInventory{
			SnapshotDirs: map[string]fschecker.DiskSnapshotInfo{
				"101": {ID: "101", Path: "/snapshots/101", SizeBytes: 2048, InodeCount: 5, ModTime: now},
			},
			TotalDiskUsageBytes: 2048,
			TotalInodes:         5,
		},
	}

	service := NewService(cMock, fMock, WithGracePeriod(1*time.Minute))
	report, err := service.StartScan(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(report.Anomalies) != 0 {
		t.Errorf("expected 0 anomalies, got %d", len(report.Anomalies))
	}
	if report.WastedDiskSpaceBytes != 0 {
		t.Errorf("expected 0 wasted disk space, got %d", report.WastedDiskSpaceBytes)
	}
	if report.TotalSnapshotsOnDisk != 1 || report.TotalSnapshotsInRuntime != 1 {
		t.Errorf("expected 1 disk and 1 runtime snapshot, got disk=%d, runtime=%d", report.TotalSnapshotsOnDisk, report.TotalSnapshotsInRuntime)
	}
}

func TestHunterService_ProvenResourceLeak(t *testing.T) {
	oldTime := time.Now().Add(-10 * time.Minute)
	cMock := &mockContainerdInspector{
		inv: &containerdchecker.RuntimeInventory{
			KnownSnapshotIDs: map[string]containerdchecker.SnapshotInfo{},
		},
	}
	fMock := &mockFilesystemInspector{
		inv: &fschecker.HostInventory{
			SnapshotDirs: map[string]fschecker.DiskSnapshotInfo{
				"999": {
					ID:         "999",
					Path:       "/var/lib/containerd/snapshots/999",
					SizeBytes:  1048576, // 1 MB
					InodeCount: 42,
					ModTime:    oldTime,
				},
			},
			TotalDiskUsageBytes: 1048576,
			TotalInodes:         42,
		},
	}

	service := NewService(cMock, fMock, WithGracePeriod(2*time.Minute))
	report, err := service.StartScan(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(report.Anomalies) != 1 {
		t.Fatalf("expected 1 anomaly, got %d", len(report.Anomalies))
	}

	a := report.Anomalies[0]
	if a.Category != CategoryProvenResourceLeak {
		t.Errorf("expected %s, got %s", CategoryProvenResourceLeak, a.Category)
	}
	if a.SnapshotID != "999" {
		t.Errorf("expected snapshot ID 999, got %s", a.SnapshotID)
	}
	if report.WastedDiskSpaceBytes != 1048576 {
		t.Errorf("expected 1048576 wasted bytes, got %d", report.WastedDiskSpaceBytes)
	}
	if report.WastedInodes != 42 {
		t.Errorf("expected 42 wasted inodes, got %d", report.WastedInodes)
	}
}

func TestHunterService_SuspectedOrphan_WithinGracePeriod(t *testing.T) {
	recentTime := time.Now().Add(-10 * time.Second)
	cMock := &mockContainerdInspector{
		inv: &containerdchecker.RuntimeInventory{
			KnownSnapshotIDs: map[string]containerdchecker.SnapshotInfo{},
		},
	}
	fMock := &mockFilesystemInspector{
		inv: &fschecker.HostInventory{
			SnapshotDirs: map[string]fschecker.DiskSnapshotInfo{
				"202": {
					ID:         "202",
					Path:       "/var/lib/containerd/snapshots/202",
					SizeBytes:  500,
					InodeCount: 2,
					ModTime:    recentTime,
				},
			},
		},
	}

	service := NewService(cMock, fMock, WithGracePeriod(2*time.Minute))
	report, err := service.StartScan(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(report.Anomalies) != 1 {
		t.Fatalf("expected 1 anomaly, got %d", len(report.Anomalies))
	}
	if report.Anomalies[0].Category != CategorySuspectedOrphan {
		t.Errorf("expected %s, got %s", CategorySuspectedOrphan, report.Anomalies[0].Category)
	}
	if report.WastedDiskSpaceBytes != 0 {
		t.Errorf("suspected orphan should not be counted as wasted space yet, got %d", report.WastedDiskSpaceBytes)
	}
}

func TestHunterService_DanglingMount(t *testing.T) {
	oldTime := time.Now().Add(-10 * time.Minute)
	cMock := &mockContainerdInspector{
		inv: &containerdchecker.RuntimeInventory{
			KnownSnapshotIDs: map[string]containerdchecker.SnapshotInfo{},
		},
	}
	fMock := &mockFilesystemInspector{
		inv: &fschecker.HostInventory{
			SnapshotDirs: map[string]fschecker.DiskSnapshotInfo{
				"303": {
					ID:        "303",
					Path:      "/var/lib/containerd/snapshots/303",
					SizeBytes: 1000,
					ModTime:   oldTime,
				},
			},
			OverlayMounts: []fschecker.OverlayMountInfo{
				{
					MountPoint: "/run/containerd/m/dead-mount",
					UpperDir:   "/var/lib/containerd/snapshots/303/fs",
					WorkDir:    "/var/lib/containerd/snapshots/303/work",
				},
			},
		},
	}

	service := NewService(cMock, fMock, WithGracePeriod(2*time.Minute))
	report, err := service.StartScan(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(report.Anomalies) != 1 {
		t.Fatalf("expected 1 anomaly, got %d", len(report.Anomalies))
	}
	a := report.Anomalies[0]
	if a.Category != CategoryConfirmedInconsistency {
		t.Errorf("expected %s, got %s", CategoryConfirmedInconsistency, a.Category)
	}
	if len(a.ActiveMounts) != 1 || a.ActiveMounts[0] != "/run/containerd/m/dead-mount" {
		t.Errorf("unexpected active mounts: %v", a.ActiveMounts)
	}
}

func TestHunterService_PhantomSnapshotInRuntime(t *testing.T) {
	cMock := &mockContainerdInspector{
		inv: &containerdchecker.RuntimeInventory{
			KnownSnapshotIDs: map[string]containerdchecker.SnapshotInfo{
				"ghost-1": {Name: "ghost-1", Kind: "Committed", Namespace: "k8s.io"},
			},
		},
	}
	fMock := &mockFilesystemInspector{
		inv: &fschecker.HostInventory{
			SnapshotDirs: map[string]fschecker.DiskSnapshotInfo{},
		},
	}

	service := NewService(cMock, fMock)
	report, err := service.StartScan(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(report.Anomalies) != 1 {
		t.Fatalf("expected 1 anomaly, got %d", len(report.Anomalies))
	}
	a := report.Anomalies[0]
	if a.Category != CategoryConfirmedInconsistency {
		t.Errorf("expected %s, got %s", CategoryConfirmedInconsistency, a.Category)
	}
	if a.SnapshotID != "ghost-1" {
		t.Errorf("expected ghost-1, got %s", a.SnapshotID)
	}
}

func TestHunterService_InspectorErrors(t *testing.T) {
	cMock := &mockContainerdInspector{
		err: errors.New("cannot connect to socket"),
	}
	fMock := &mockFilesystemInspector{
		inv: &fschecker.HostInventory{},
	}

	service := NewService(cMock, fMock)
	_, err := service.StartScan(context.Background())
	if err == nil {
		t.Fatal("expected error when inspector fails, got nil")
	}
}
