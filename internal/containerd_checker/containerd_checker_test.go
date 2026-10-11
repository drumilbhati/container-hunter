package containerdchecker

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Ensure Checker implements ContainerdInspector interface at compile time.
var _ ContainerdInspector = (*Checker)(nil)

func TestNewChecker(t *testing.T) {
	socket := "/tmp/test-containerd.sock"
	checker := NewChecker(socket)

	if checker == nil {
		t.Fatal("expected non-nil checker")
	}
	if checker.socketPath != socket {
		t.Errorf("expected socketPath %q, got %q", socket, checker.socketPath)
	}
	if checker.snapshotterName != "overlayfs" {
		t.Errorf("expected default snapshotterName 'overlayfs', got %q", checker.snapshotterName)
	}
	if checker.timeout != 2*time.Second {
		t.Errorf("expected default timeout 2s, got %v", checker.timeout)
	}
}

func TestInspect_InvalidSocket(t *testing.T) {
	checker := NewChecker("/tmp/nonexistent-containerd.sock")
	checker.timeout = 500 * time.Millisecond // Fast failure for unit testing

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	inv, err := checker.Inspect(ctx)
	if err == nil {
		t.Fatal("expected error connecting to non-existent socket, got nil")
	}
	if inv != nil {
		t.Errorf("expected nil inventory on error, got %+v", inv)
	}
	if !strings.Contains(err.Error(), "failed to") {
		t.Errorf("expected error message to indicate failure, got %v", err)
	}
}

func TestSnapshotInfo_Structure(t *testing.T) {
	now := time.Now()
	info := SnapshotInfo{
		Name:      "test-snap-1",
		Parent:    "base-snap-0",
		Kind:      "Active",
		Namespace: "k8s.io",
		CreatedAt: now,
	}

	if info.Name != "test-snap-1" || info.Parent != "base-snap-0" || info.Kind != "Active" || info.Namespace != "k8s.io" || !info.CreatedAt.Equal(now) {
		t.Errorf("snapshot info struct fields mismatch: %+v", info)
	}
}

func TestRuntimeInventory_Structure(t *testing.T) {
	inv := RuntimeInventory{
		KnownSnapshotIDs:   make(map[string]SnapshotInfo),
		ActiveContainerIDs: []string{"ctr-1", "ctr-2"},
		ActiveLeaseIDs:     []string{"lease-1"},
	}

	inv.KnownSnapshotIDs["snap-1"] = SnapshotInfo{Name: "snap-1"}

	if len(inv.KnownSnapshotIDs) != 1 {
		t.Errorf("expected 1 snapshot, got %d", len(inv.KnownSnapshotIDs))
	}
	if len(inv.ActiveContainerIDs) != 2 {
		t.Errorf("expected 2 containers, got %d", len(inv.ActiveContainerIDs))
	}
	if len(inv.ActiveLeaseIDs) != 1 {
		t.Errorf("expected 1 lease, got %d", len(inv.ActiveLeaseIDs))
	}
}

// MockChecker implements ContainerdInspector for testing consumers like HunterService.
type MockChecker struct {
	Inventory *RuntimeInventory
	Err       error
}

func (m *MockChecker) Inspect(ctx context.Context) (*RuntimeInventory, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Inventory, nil
}

func TestMockChecker_InterfaceCompliance(t *testing.T) {
	mock := &MockChecker{
		Inventory: &RuntimeInventory{
			KnownSnapshotIDs: map[string]SnapshotInfo{
				"1": {Name: "1", Kind: "Active"},
			},
			ActiveContainerIDs: []string{"ctr-1"},
			ActiveLeaseIDs:     []string{"lease-1"},
		},
	}

	var inspector ContainerdInspector = mock
	inv, err := inspector.Inspect(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(inv.KnownSnapshotIDs) != 1 {
		t.Errorf("expected 1 snapshot, got %d", len(inv.KnownSnapshotIDs))
	}
}
