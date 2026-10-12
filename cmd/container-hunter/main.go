package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	containerdchecker "github.com/drumilbhati/container-hunter/internal/containerd_checker"
	fschecker "github.com/drumilbhati/container-hunter/internal/fs_checker"
	hunterservice "github.com/drumilbhati/container-hunter/internal/hunter_service"
	"github.com/drumilbhati/container-hunter/internal/tui"
	"golang.org/x/term"
)

const version = "1.0.0"

func main() {
	var (
		socketPath   string
		snapshotsDir string
		mountinfo    string
		gracePeriod  time.Duration
		noTUI        bool
		jsonOutput   bool
		showVersion  bool
	)

	flag.StringVar(&socketPath, "socket", "/run/containerd/containerd.sock", "Path to containerd UNIX domain socket")
	flag.StringVar(&socketPath, "s", "/run/containerd/containerd.sock", "Path to containerd UNIX domain socket (shorthand)")

	flag.StringVar(&snapshotsDir, "snapshots-dir", fschecker.DefaultSnapshotsDir, "Path to OverlayFS snapshots directory")
	flag.StringVar(&snapshotsDir, "d", fschecker.DefaultSnapshotsDir, "Path to OverlayFS snapshots directory (shorthand)")

	flag.StringVar(&mountinfo, "mountinfo", fschecker.DefaultMountinfoPath, "Path to Linux mountinfo file")
	flag.StringVar(&mountinfo, "m", fschecker.DefaultMountinfoPath, "Path to Linux mountinfo file (shorthand)")

	flag.DurationVar(&gracePeriod, "grace-period", 5*time.Minute, "Safety grace period before classifying orphans as proven leaks")
	flag.DurationVar(&gracePeriod, "g", 5*time.Minute, "Safety grace period before classifying orphans as proven leaks (shorthand)")

	flag.BoolVar(&noTUI, "no-tui", false, "Disable interactive TUI and print summary table to stdout")
	flag.BoolVar(&jsonOutput, "json", false, "Output results as JSON for automation")
	flag.BoolVar(&showVersion, "version", false, "Print version and exit")
	flag.BoolVar(&showVersion, "v", false, "Print version and exit (shorthand)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Container Hunter v%s — Read-only container leak diagnostic tool\n\n", version)
		fmt.Fprintf(os.Stderr, "Usage: %s [flags]\n\nFlags:\n", os.Args[0])
		flag.PrintDefaults()
	}

	flag.Parse()

	if showVersion {
		fmt.Printf("Container Hunter v%s\n", version)
		return
	}

	// Initialize inspection components
	cChecker := containerdchecker.NewChecker(socketPath)
	fChecker := fschecker.NewChecker(snapshotsDir, mountinfo)
	service := hunterservice.NewService(cChecker, fChecker, hunterservice.WithGracePeriod(gracePeriod))

	// 1. JSON output mode
	if jsonOutput {
		runHeadlessJSON(service)
		return
	}

	// 2. Non-interactive table mode
	isTerminal := term.IsTerminal(int(os.Stdout.Fd()))
	if noTUI || !isTerminal {
		runHeadlessTable(service)
		return
	}

	// 3. Interactive TUI mode (default when in a TTY)
	p := tea.NewProgram(tui.NewModel(service), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}
}

func runHeadlessJSON(service *hunterservice.Service) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	report, err := service.StartScan(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error running scan: %v\n", err)
		os.Exit(1)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
		os.Exit(1)
	}
}

func runHeadlessTable(service *hunterservice.Service) {
	fmt.Println("Running Container Hunter scan (read-only)...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	report, err := service.StartScan(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error during scan: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n=== Scan Summary ===\n")
	fmt.Printf("Duration:         %s\n", report.Duration.Round(time.Millisecond))
	fmt.Printf("Disk Snapshots:   %d\n", report.TotalSnapshotsOnDisk)
	fmt.Printf("Runtime Keys:     %d\n", report.TotalSnapshotsInRuntime)
	fmt.Printf("Overlay Mounts:   %d\n", report.TotalActiveMounts)
	fmt.Printf("Wasted Storage:   %s (%d bytes)\n", tui.FormatBytes(report.WastedDiskSpaceBytes), report.WastedDiskSpaceBytes)
	fmt.Printf("Wasted Inodes:    %d\n", report.WastedInodes)
	fmt.Printf("Anomalies Found:  %d\n\n", len(report.Anomalies))

	if len(report.Anomalies) == 0 {
		fmt.Println("✔ No leftover container files or mounts found.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "CATEGORY\tSNAPSHOT ID\tSIZE\tINODES\tAGE\tREASON")
	fmt.Fprintln(w, "--------\t-----------\t----\t------\t---\t------")

	for _, a := range report.Anomalies {
		age := a.Age.Round(time.Second).String()
		if a.Age <= 0 {
			age = "N/A"
		}
		reason := a.Reason
		if len(reason) > 60 {
			reason = reason[:57] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\n",
			a.Category,
			a.SnapshotID,
			tui.FormatBytes(a.SizeBytes),
			a.InodeCount,
			age,
			strings.ReplaceAll(reason, "\t", " "),
		)
	}
	w.Flush()
}
