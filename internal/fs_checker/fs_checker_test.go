package fschecker

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Ensure Checker implements FilesystemInspector interface at compile time.
var _ FilesystemInspector = (*Checker)(nil)

func TestNewChecker(t *testing.T) {
	snapDir := "/tmp/test-snapshots"
	mountPath := "/tmp/test-mountinfo"

	checker := NewChecker(snapDir, mountPath, WithCalculateSizes(false))
	if checker == nil {
		t.Fatal("expected non-nil checker")
	}
	if checker.snapshotsRootDir != snapDir {
		t.Errorf("expected snapshotsRootDir %q, got %q", snapDir, checker.snapshotsRootDir)
	}
	if checker.mountinfoPath != mountPath {
		t.Errorf("expected mountinfoPath %q, got %q", mountPath, checker.mountinfoPath)
	}
	if checker.calculateSizes {
		t.Error("expected calculateSizes to be false with option")
	}
}

func TestDefaultChecker(t *testing.T) {
	checker := DefaultChecker()
	if checker == nil {
		t.Fatal("expected non-nil default checker")
	}
	if checker.snapshotsRootDir != DefaultSnapshotsDir {
		t.Errorf("expected DefaultSnapshotsDir, got %q", checker.snapshotsRootDir)
	}
	if checker.mountinfoPath != DefaultMountinfoPath {
		t.Errorf("expected DefaultMountinfoPath, got %q", checker.mountinfoPath)
	}
	if !checker.calculateSizes {
		t.Error("expected calculateSizes to default to true")
	}
}

func TestDecodeProcEscapes(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/normal/path", "/normal/path"},
		{"/path\\040with\\040spaces", "/path with spaces"},
		{"/path\\011tab", "/path\ttab"},
		{"invalid\\999path", "invalid\\999path"},
		{"short\\01", "short\\01"},
	}

	for _, tc := range tests {
		actual := decodeProcEscapes(tc.input)
		if actual != tc.expected {
			t.Errorf("decodeProcEscapes(%q): expected %q, got %q", tc.input, tc.expected, actual)
		}
	}
}

func TestParseMountInfoLine_Overlay(t *testing.T) {
	line := "125 32 0:45 / /run/containerd/io.containerd.runtime.v2.task/k8s.io/pod1/rootfs rw,relatime shared:75 - overlay overlay rw,lowerdir=/var/lib/containerd/snapshots/1/fs:/var/lib/containerd/snapshots/2/fs,upperdir=/var/lib/containerd/snapshots/3/fs,workdir=/var/lib/containerd/snapshots/3/work"

	overlay, isOverlay, err := parseMountInfoLine(line)
	if err != nil {
		t.Fatalf("unexpected error parsing overlay line: %v", err)
	}
	if !isOverlay {
		t.Fatal("expected isOverlay to be true")
	}

	if overlay.MountID != 125 {
		t.Errorf("expected MountID 125, got %d", overlay.MountID)
	}
	if overlay.ParentID != 32 {
		t.Errorf("expected ParentID 32, got %d", overlay.ParentID)
	}
	if overlay.MountPoint != "/run/containerd/io.containerd.runtime.v2.task/k8s.io/pod1/rootfs" {
		t.Errorf("unexpected MountPoint: %s", overlay.MountPoint)
	}
	if overlay.FSType != "overlay" {
		t.Errorf("expected FSType 'overlay', got %q", overlay.FSType)
	}
	if len(overlay.LowerDirs) != 2 {
		t.Fatalf("expected 2 lowerdirs, got %d", len(overlay.LowerDirs))
	}
	if overlay.LowerDirs[0] != "/var/lib/containerd/snapshots/1/fs" {
		t.Errorf("expected lowerdir 0 to be /var/lib/containerd/snapshots/1/fs, got %s", overlay.LowerDirs[0])
	}
	if overlay.UpperDir != "/var/lib/containerd/snapshots/3/fs" {
		t.Errorf("unexpected UpperDir: %s", overlay.UpperDir)
	}
	if overlay.WorkDir != "/var/lib/containerd/snapshots/3/work" {
		t.Errorf("unexpected WorkDir: %s", overlay.WorkDir)
	}
}

func TestParseMountInfoLine_NonOverlay(t *testing.T) {
	line := "36 35 98:0 / /var/log rw,noatime master:1 - ext4 /dev/root rw,errors=continue"

	_, isOverlay, err := parseMountInfoLine(line)
	if err != nil {
		t.Fatalf("unexpected error parsing non-overlay line: %v", err)
	}
	if isOverlay {
		t.Fatal("expected isOverlay to be false for ext4")
	}
}

func TestParseMountInfoLine_Malformed(t *testing.T) {
	_, _, err := parseMountInfoLine("too few fields")
	if err == nil {
		t.Error("expected error for line with too few fields")
	}

	_, _, err = parseMountInfoLine("invalid parent 98:0 / / rw - ext4 /dev/root rw")
	if err == nil {
		t.Error("expected error for invalid int field")
	}

	_, _, err = parseMountInfoLine("36 35 98:0 / / rw missing-separator-token")
	if err == nil {
		t.Error("expected error for missing hyphen separator")
	}
}

func TestInspect_FullMock(t *testing.T) {
	tempDir := t.TempDir()
	snapRoot := filepath.Join(tempDir, "snapshots")
	mountinfoPath := filepath.Join(tempDir, "mountinfo")

	// 1. Create simulated snapshots directory structure
	snap1 := filepath.Join(snapRoot, "101")
	if err := os.MkdirAll(filepath.Join(snap1, "fs"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(snap1, "work"), 0755); err != nil {
		t.Fatal(err)
	}
	testFile := filepath.Join(snap1, "fs", "test.txt")
	if err := os.WriteFile(testFile, []byte("hello containerd"), 0644); err != nil {
		t.Fatal(err)
	}

	snap2 := filepath.Join(snapRoot, "102")
	if err := os.MkdirAll(snap2, 0755); err != nil {
		t.Fatal(err)
	}

	// 2. Create simulated mountinfo file
	mountinfoContent := "50 1 0:30 / /var/lib/overlay-test rw,relatime - overlay overlay rw,lowerdir=" + filepath.Join(snap1, "fs") + "\n" +
		"51 1 8:1 / /mnt rw,relatime - ext4 /dev/sda1 rw\n"
	if err := os.WriteFile(mountinfoPath, []byte(mountinfoContent), 0644); err != nil {
		t.Fatal(err)
	}

	checker := NewChecker(snapRoot, mountinfoPath)
	inv, err := checker.Inspect(context.Background())
	if err != nil {
		t.Fatalf("unexpected error during Inspect: %v", err)
	}

	if len(inv.SnapshotDirs) != 2 {
		t.Fatalf("expected 2 snapshot dirs, got %d", len(inv.SnapshotDirs))
	}

	s1, exists := inv.SnapshotDirs["101"]
	if !exists {
		t.Fatal("expected snapshot 101 to exist")
	}
	if !s1.HasFsDir || !s1.HasWorkDir {
		t.Errorf("expected snapshot 101 to have fs and work dirs, got fs=%v, work=%v", s1.HasFsDir, s1.HasWorkDir)
	}
	if s1.SizeBytes != int64(len("hello containerd")) {
		t.Errorf("expected size %d, got %d", len("hello containerd"), s1.SizeBytes)
	}
	if s1.InodeCount < 3 { // snapshot dir, fs dir, work dir, test.txt
		t.Errorf("expected at least 3 inodes, got %d", s1.InodeCount)
	}

	s2, exists := inv.SnapshotDirs["102"]
	if !exists {
		t.Fatal("expected snapshot 102 to exist")
	}
	if s2.HasFsDir || s2.HasWorkDir {
		t.Errorf("expected snapshot 102 not to have fs/work dirs, got fs=%v, work=%v", s2.HasFsDir, s2.HasWorkDir)
	}

	if len(inv.OverlayMounts) != 1 {
		t.Fatalf("expected 1 overlay mount, got %d", len(inv.OverlayMounts))
	}
	if inv.OverlayMounts[0].MountPoint != "/var/lib/overlay-test" {
		t.Errorf("unexpected mount point: %s", inv.OverlayMounts[0].MountPoint)
	}
}

func TestInspect_ContextCancellation(t *testing.T) {
	tempDir := t.TempDir()
	checker := NewChecker(tempDir, filepath.Join(tempDir, "mountinfo"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := checker.Inspect(ctx)
	if err == nil {
		t.Fatal("expected context error on cancelled context, got nil")
	}
}

// MockFilesystemInspector implements FilesystemInspector for testing consumers.
type MockFilesystemInspector struct {
	Inventory *HostInventory
	Err       error
}

func (m *MockFilesystemInspector) Inspect(ctx context.Context) (*HostInventory, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Inventory, nil
}

func TestMockFilesystemInspector_Compliance(t *testing.T) {
	mock := &MockFilesystemInspector{
		Inventory: &HostInventory{
			SnapshotDirs: map[string]DiskSnapshotInfo{
				"1": {ID: "1", SizeBytes: 1024},
			},
			OverlayMounts: []OverlayMountInfo{
				{MountID: 10, MountPoint: "/target"},
			},
		},
	}

	var inspector FilesystemInspector = mock
	inv, err := inspector.Inspect(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(inv.SnapshotDirs) != 1 || len(inv.OverlayMounts) != 1 {
		t.Errorf("unexpected inventory result: %+v", inv)
	}
}
