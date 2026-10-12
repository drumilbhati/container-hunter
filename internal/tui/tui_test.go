package tui

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	containerdchecker "github.com/drumilbhati/container-hunter/internal/containerd_checker"
	fschecker "github.com/drumilbhati/container-hunter/internal/fs_checker"
	hunterservice "github.com/drumilbhati/container-hunter/internal/hunter_service"
)

type mockContainerd struct{}

func (m *mockContainerd) Inspect(ctx context.Context) (*containerdchecker.RuntimeInventory, error) {
	return &containerdchecker.RuntimeInventory{
		KnownSnapshotIDs: map[string]containerdchecker.SnapshotInfo{},
	}, nil
}

type mockFS struct{}

func (m *mockFS) Inspect(ctx context.Context) (*fschecker.HostInventory, error) {
	return &fschecker.HostInventory{
		SnapshotDirs: map[string]fschecker.DiskSnapshotInfo{
			"999": {
				ID:        "999",
				Path:      "/var/lib/containerd/snapshots/999",
				SizeBytes: 1048576,
				ModTime:   time.Now().Add(-1 * time.Hour),
			},
		},
	}, nil
}

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		bytes    int64
		expected string
	}{
		{500, "500 B"},
		{1024, "1.00 KB"},
		{1048576, "1.00 MB"},
		{1073741824, "1.00 GB"},
	}

	for _, c := range cases {
		out := FormatBytes(c.bytes)
		if out != c.expected {
			t.Errorf("FormatBytes(%d): expected %s, got %s", c.bytes, c.expected, out)
		}
	}
}

func TestTUI_Lifecycle(t *testing.T) {
	service := hunterservice.NewService(&mockContainerd{}, &mockFS{})
	m := NewModel(service)

	if m.currentState != stateScanning {
		t.Errorf("expected initial state stateScanning, got %v", m.currentState)
	}

	view := m.View()
	if view == "" {
		t.Error("expected non-empty scanning view")
	}

	// Simulate scan completion message
	report, err := service.StartScan(context.Background())
	if err != nil {
		t.Fatalf("unexpected scan error: %v", err)
	}

	newM, _ := m.Update(scanCompleteMsg{report: report})
	model := newM.(Model)

	if model.currentState != stateTable {
		t.Errorf("expected stateTable after scan complete, got %v", model.currentState)
	}

	tableView := model.View()
	if tableView == "" {
		t.Error("expected non-empty table view")
	}

	// Test Filter switching via Tab
	newM, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	newM, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = newM.(Model)
	if model.currentFilter != filterLeaks {
		t.Errorf("expected filterLeaks after tab, got %v", model.currentFilter)
	}

	// Test Detail view opening via Enter
	newM, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = newM.(Model)
	if model.currentState != stateDetail {
		t.Errorf("expected stateDetail after enter, got %v", model.currentState)
	}

	detailView := model.View()
	if detailView == "" {
		t.Error("expected non-empty detail view")
	}

	// Test returning via Esc
	newM, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = newM.(Model)
	if model.currentState != stateTable {
		t.Errorf("expected return to stateTable after esc, got %v", model.currentState)
	}
}
