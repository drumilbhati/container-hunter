package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	hunterservice "github.com/drumilbhati/container-hunter/internal/hunter_service"
)

type state int

const (
	stateScanning state = iota
	stateTable
	stateDetail
	stateError
)

type filterType int

const (
	filterAll filterType = iota
	filterLeaks
	filterInconsistencies
	filterOrphans
)

// Messages
type scanCompleteMsg struct {
	report *hunterservice.ScanReport
	err    error
}

// Styling definitions
var (
	baseStyle = lipgloss.NewStyle().
			Padding(0, 1)

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#5A56E0")).
			Padding(0, 1)

	metricBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#5A56E0")).
			Padding(0, 1).
			MarginBottom(1)

	leakPill = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#E06C75")).
			Bold(true).
			Padding(0, 1)

	inconsistencyPill = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#000000")).
				Background(lipgloss.Color("#E5C07B")).
				Bold(true).
				Padding(0, 1)

	orphanPill = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#61AFEF")).
			Bold(true).
			Padding(0, 1)

	detailCardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#98C379")).
			Padding(1, 2).
			Margin(1, 0)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#5C6370")).
			MarginTop(1)
)

// Model represents the Bubble Tea TUI state.
type Model struct {
	service         *hunterservice.Service
	currentState    state
	spinner         spinner.Model
	table           table.Model
	report          *hunterservice.ScanReport
	err             error
	selectedAnomaly *hunterservice.DetectedAnomaly
	currentFilter   filterType
	width           int
	height          int
}

// NewModel instantiates the TUI model for a given HunterService.
func NewModel(service *hunterservice.Service) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#5A56E0"))

	tbl := table.New(
		table.WithColumns(getColumns()),
		table.WithFocused(true),
		table.WithHeight(10),
	)

	tblStyle := table.DefaultStyles()
	tblStyle.Header = tblStyle.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#3E4451")).
		BorderBottom(true).
		Bold(true)
	tblStyle.Selected = tblStyle.Selected.
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#4B5263")).
		Bold(true)
	tbl.SetStyles(tblStyle)

	return Model{
		service:       service,
		currentState:  stateScanning,
		spinner:       s,
		table:         tbl,
		currentFilter: filterAll,
	}
}

func getColumns() []table.Column {
	return []table.Column{
		{Title: "Category", Width: 22},
		{Title: "Snapshot ID", Width: 18},
		{Title: "Wasted Size", Width: 14},
		{Title: "Inodes", Width: 10},
		{Title: "Age", Width: 12},
		{Title: "Mounts", Width: 8},
	}
}

// Init triggers initial scanning command and spinner tick.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.startScanCmd(),
	)
}

func (m Model) startScanCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		report, err := m.service.StartScan(ctx)
		return scanCompleteMsg{report: report, err: err}
	}
}

// Update handles incoming messages and user input events.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.table.SetHeight(max(5, msg.Height-12))
		m.table.SetWidth(msg.Width - 4)

	case scanCompleteMsg:
		if msg.err != nil {
			m.err = msg.err
			m.currentState = stateError
			return m, nil
		}
		m.report = msg.report
		m.currentState = stateTable
		m.updateTableRows()
		return m, nil

	case spinner.TickMsg:
		if m.currentState == stateScanning {
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.currentState == stateDetail {
				m.currentState = stateTable
				return m, nil
			}
			return m, tea.Quit

		case "esc":
			if m.currentState == stateDetail {
				m.currentState = stateTable
				return m, nil
			}

		case "r":
			if m.currentState != stateScanning {
				m.currentState = stateScanning
				return m, tea.Batch(m.spinner.Tick, m.startScanCmd())
			}

		case "tab":
			if m.currentState == stateTable {
				m.currentFilter = (m.currentFilter + 1) % 4
				m.updateTableRows()
				return m, nil
			}

		case "1":
			if m.currentState == stateTable {
				m.currentFilter = filterAll
				m.updateTableRows()
				return m, nil
			}
		case "2":
			if m.currentState == stateTable {
				m.currentFilter = filterLeaks
				m.updateTableRows()
				return m, nil
			}
		case "3":
			if m.currentState == stateTable {
				m.currentFilter = filterInconsistencies
				m.updateTableRows()
				return m, nil
			}
		case "4":
			if m.currentState == stateTable {
				m.currentFilter = filterOrphans
				m.updateTableRows()
				return m, nil
			}

		case "enter":
			if m.currentState == stateTable && len(m.report.Anomalies) > 0 {
				selectedIdx := m.table.Cursor()
				filtered := m.getFilteredAnomalies()
				if selectedIdx >= 0 && selectedIdx < len(filtered) {
					m.selectedAnomaly = &filtered[selectedIdx]
					m.currentState = stateDetail
					return m, nil
				}
			} else if m.currentState == stateDetail {
				m.currentState = stateTable
				return m, nil
			}
		}
	}

	if m.currentState == stateTable {
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *Model) getFilteredAnomalies() []hunterservice.DetectedAnomaly {
	if m.report == nil {
		return nil
	}
	var filtered []hunterservice.DetectedAnomaly
	for _, a := range m.report.Anomalies {
		switch m.currentFilter {
		case filterLeaks:
			if a.Category == hunterservice.CategoryProvenResourceLeak {
				filtered = append(filtered, a)
			}
		case filterInconsistencies:
			if a.Category == hunterservice.CategoryConfirmedInconsistency {
				filtered = append(filtered, a)
			}
		case filterOrphans:
			if a.Category == hunterservice.CategorySuspectedOrphan {
				filtered = append(filtered, a)
			}
		default:
			filtered = append(filtered, a)
		}
	}
	return filtered
}

func (m *Model) updateTableRows() {
	filtered := m.getFilteredAnomalies()
	rows := make([]table.Row, 0, len(filtered))

	for _, a := range filtered {
		cat := string(a.Category)
		size := FormatBytes(a.SizeBytes)
		inodes := fmt.Sprintf("%d", a.InodeCount)
		age := a.Age.Round(time.Second).String()
		if a.Age <= 0 {
			age = "N/A"
		}
		mounts := fmt.Sprintf("%d", len(a.ActiveMounts))

		rows = append(rows, table.Row{
			cat,
			a.SnapshotID,
			size,
			inodes,
			age,
			mounts,
		})
	}
	m.table.SetRows(rows)
}

// View renders the terminal view according to currentState.
func (m Model) View() string {
	switch m.currentState {
	case stateScanning:
		return m.viewScanning()
	case stateTable:
		return m.viewTable()
	case stateDetail:
		return m.viewDetail()
	case stateError:
		return m.viewError()
	default:
		return ""
	}
}

func (m Model) viewScanning() string {
	return baseStyle.Render(fmt.Sprintf(
		"\n  %s %s\n\n  %s\n",
		titleStyle.Render("CONTAINER HUNTER"),
		m.spinner.View(),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#ABB2BF")).Render("Inspecting containerd runtime metadata and Linux host filesystems..."),
	))
}

func (m Model) viewTable() string {
	var b strings.Builder

	// Header banner
	b.WriteString(titleStyle.Render("CONTAINER HUNTER v1.0.0") + "  " +
		lipgloss.NewStyle().Foreground(lipgloss.Color("#98C379")).Bold(true).Render("● READ-ONLY DIAGNOSTIC MODE") + "\n\n")

	// Metric summary dashboard
	if m.report != nil {
		metricContent := fmt.Sprintf(
			"Disk Snapshots: %s  │  Runtime Keys: %s  │  Overlay Mounts: %s\n"+
				"Wasted Storage: %s  │  Wasted Inodes: %s  │  Scan Duration: %s",
			lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("%d", m.report.TotalSnapshotsOnDisk)),
			lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("%d", m.report.TotalSnapshotsInRuntime)),
			lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("%d", m.report.TotalActiveMounts)),
			lipgloss.NewStyle().Foreground(lipgloss.Color("#E06C75")).Bold(true).Render(FormatBytes(m.report.WastedDiskSpaceBytes)),
			lipgloss.NewStyle().Foreground(lipgloss.Color("#E06C75")).Bold(true).Render(fmt.Sprintf("%d", m.report.WastedInodes)),
			lipgloss.NewStyle().Foreground(lipgloss.Color("#61AFEF")).Render(m.report.Duration.Round(time.Millisecond).String()),
		)
		b.WriteString(metricBoxStyle.Render(metricContent) + "\n")
	}

	// Filter Tabs
	tabs := []string{
		renderTab("[1] All", m.currentFilter == filterAll),
		renderTab("[2] Proven Leaks", m.currentFilter == filterLeaks),
		renderTab("[3] Inconsistencies", m.currentFilter == filterInconsistencies),
		renderTab("[4] Suspected Orphans", m.currentFilter == filterOrphans),
	}
	b.WriteString(strings.Join(tabs, " ") + "\n\n")

	// Table or Empty Notice
	if len(m.table.Rows()) == 0 {
		b.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("#98C379")).
			Bold(true).
			Padding(2, 4).
			Render("✔ Clean! No leftover container files or leaked mounts detected."))
		b.WriteString("\n")
	} else {
		b.WriteString(m.table.View() + "\n")
	}

	// Help keys footer
	b.WriteString(helpStyle.Render("Keys: [↑/↓/j/k] Navigate  │  [Enter] Detailed Evidence  │  [Tab/1-4] Filter  │  [r] Re-scan  │  [q] Quit"))

	return baseStyle.Render(b.String())
}

func renderTab(title string, active bool) string {
	if active {
		return lipgloss.NewStyle().
			Background(lipgloss.Color("#5A56E0")).
			Foreground(lipgloss.Color("#FFFFFF")).
			Bold(true).
			Padding(0, 1).
			Render(title)
	}
	return lipgloss.NewStyle().
		Background(lipgloss.Color("#2C313C")).
		Foreground(lipgloss.Color("#ABB2BF")).
		Padding(0, 1).
		Render(title)
}

func (m Model) viewDetail() string {
	if m.selectedAnomaly == nil {
		return m.viewTable()
	}

	a := m.selectedAnomaly
	var catPill string
	switch a.Category {
	case hunterservice.CategoryProvenResourceLeak:
		catPill = leakPill.Render(string(a.Category))
	case hunterservice.CategoryConfirmedInconsistency:
		catPill = inconsistencyPill.Render(string(a.Category))
	default:
		catPill = orphanPill.Render(string(a.Category))
	}

	mountsDesc := "None"
	if len(a.ActiveMounts) > 0 {
		mountsDesc = strings.Join(a.ActiveMounts, "\n    - ")
	}

	content := fmt.Sprintf(
		"ANOMALY FORENSIC DOSSIER\n\n"+
			"Category:        %s\n"+
			"Snapshot ID:     %s\n"+
			"Physical Path:   %s\n"+
			"Disk Space:      %s (%d bytes)\n"+
			"Inodes:          %d\n"+
			"Age on Disk:     %s\n"+
			"Checked At:      %s\n"+
			"Active Leases:   %t\n"+
			"Container Lock:  %t\n"+
			"Active Mounts:   %s\n\n"+
			"Forensic Explanation:\n  %s\n",
		catPill,
		lipgloss.NewStyle().Bold(true).Render(a.SnapshotID),
		a.Path,
		FormatBytes(a.SizeBytes), a.SizeBytes,
		a.InodeCount,
		a.Age.Round(time.Second),
		a.CheckedAt.Format(time.RFC3339),
		a.HasLease,
		a.HasContainer,
		mountsDesc,
		lipgloss.NewStyle().Foreground(lipgloss.Color("#E5C07B")).Render(a.Reason),
	)

	card := detailCardStyle.Render(content)
	footer := helpStyle.Render("Press [Esc] or [Enter] to return to table  │  [q] Quit")

	return baseStyle.Render(card + "\n" + footer)
}

func (m Model) viewError() string {
	return baseStyle.Render(fmt.Sprintf(
		"\n  %s\n\n  Error: %s\n\n  %s\n",
		titleStyle.Render("CONTAINER HUNTER — SCAN ERROR"),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#E06C75")).Render(m.err.Error()),
		helpStyle.Render("Keys: [r] Retry Scan  │  [q] Quit"),
	))
}

// FormatBytes formats byte counts into human-readable strings (e.g. 1.25 MB).
func FormatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
