package fschecker

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultSnapshotsDir is the standard host directory where containerd overlayfs snapshots reside.
	DefaultSnapshotsDir = "/var/lib/containerd/io.containerd.snapshotter.v1.overlayfs/snapshots"

	// DefaultMountinfoPath is the standard Linux procfs mountinfo path for the current process.
	DefaultMountinfoPath = "/proc/self/mountinfo"
)

// FilesystemInspector defines the interface for collecting host filesystem and mount table inventory.
type FilesystemInspector interface {
	Inspect(ctx context.Context) (*HostInventory, error)
}

// DiskSnapshotInfo captures metadata for a single snapshot folder physically present on disk.
type DiskSnapshotInfo struct {
	ID         string    `json:"id"`
	Path       string    `json:"path"`
	SizeBytes  int64     `json:"size_bytes"`
	InodeCount int64     `json:"inode_count"`
	ModTime    time.Time `json:"mod_time"`
	HasWorkDir bool      `json:"has_work_dir"`
	HasFsDir   bool      `json:"has_fs_dir"`
}

// OverlayMountInfo captures details of an active OverlayFS mount from /proc/*/mountinfo.
type OverlayMountInfo struct {
	MountID      int      `json:"mount_id"`
	ParentID     int      `json:"parent_id"`
	MajorMinor   string   `json:"major_minor"`
	Root         string   `json:"root"`
	MountPoint   string   `json:"mount_point"`
	MountOptions []string `json:"mount_options"`
	FSType       string   `json:"fs_type"`
	MountSource  string   `json:"mount_source"`
	SuperOptions []string `json:"super_options"`
	LowerDirs    []string `json:"lower_dirs"`
	UpperDir     string   `json:"upper_dir"`
	WorkDir      string   `json:"work_dir"`
}

// HostInventory aggregates physical snapshot folders and active Linux OverlayFS mounts.
type HostInventory struct {
	SnapshotDirs        map[string]DiskSnapshotInfo `json:"snapshot_dirs"`
	OverlayMounts       []OverlayMountInfo          `json:"overlay_mounts"`
	TotalDiskUsageBytes int64                       `json:"total_disk_usage_bytes"`
	TotalInodes         int64                       `json:"total_inodes"`
}

// Checker performs read-only inspection of host snapshot directories and kernel mount tables.
type Checker struct {
	snapshotsRootDir string
	mountinfoPath    string
	calculateSizes   bool
}

// Option configures Checker parameters.
type Option func(*Checker)

// WithCalculateSizes toggles whether snapshot directory byte sizes and inodes are calculated.
func WithCalculateSizes(enabled bool) Option {
	return func(c *Checker) {
		c.calculateSizes = enabled
	}
}

// NewChecker creates a Checker instance with explicit snapshot directory and mountinfo paths.
func NewChecker(snapshotsRootDir, mountinfoPath string, opts ...Option) *Checker {
	c := &Checker{
		snapshotsRootDir: snapshotsRootDir,
		mountinfoPath:    mountinfoPath,
		calculateSizes:   true,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// DefaultChecker initializes a Checker with standard Linux production paths.
func DefaultChecker(opts ...Option) *Checker {
	return NewChecker(DefaultSnapshotsDir, DefaultMountinfoPath, opts...)
}

// Inspect collects snapshot directory metadata and active OverlayFS mounts across the host system.
func (c *Checker) Inspect(ctx context.Context) (*HostInventory, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	inventory := &HostInventory{
		SnapshotDirs:  make(map[string]DiskSnapshotInfo),
		OverlayMounts: make([]OverlayMountInfo, 0),
	}

	// 1. Scan physical snapshot directories on disk
	snapDirs, totalBytes, totalInodes, err := c.scanSnapshots(ctx)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to scan snapshots directory at %s: %w", c.snapshotsRootDir, err)
	}
	if snapDirs != nil {
		inventory.SnapshotDirs = snapDirs
		inventory.TotalDiskUsageBytes = totalBytes
		inventory.TotalInodes = totalInodes
	}

	// 2. Parse Linux mount tables from mountinfo
	mounts, err := c.parseMountInfo(ctx)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to parse mountinfo at %s: %w", c.mountinfoPath, err)
	}
	if mounts != nil {
		inventory.OverlayMounts = mounts
	}

	return inventory, nil
}

// scanSnapshots reads the snapshot directory and calculates size and inode counts.
func (c *Checker) scanSnapshots(ctx context.Context) (map[string]DiskSnapshotInfo, int64, int64, error) {
	entries, err := os.ReadDir(c.snapshotsRootDir)
	if err != nil {
		return nil, 0, 0, err
	}

	result := make(map[string]DiskSnapshotInfo)
	var grandTotalBytes int64
	var grandTotalInodes int64

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, 0, 0, err
		}

		if !entry.IsDir() {
			continue
		}

		snapID := entry.Name()
		snapPath := filepath.Join(c.snapshotsRootDir, snapID)

		info, err := entry.Info()
		if err != nil {
			continue
		}

		snapInfo := DiskSnapshotInfo{
			ID:      snapID,
			Path:    snapPath,
			ModTime: info.ModTime(),
		}

		// Check for presence of work and fs directories
		if _, err := os.Stat(filepath.Join(snapPath, "work")); err == nil {
			snapInfo.HasWorkDir = true
		}
		if _, err := os.Stat(filepath.Join(snapPath, "fs")); err == nil {
			snapInfo.HasFsDir = true
		}

		if c.calculateSizes {
			sizeBytes, inodes := c.calculateDirectoryUsage(ctx, snapPath)
			snapInfo.SizeBytes = sizeBytes
			snapInfo.InodeCount = inodes
			grandTotalBytes += sizeBytes
			grandTotalInodes += inodes
		}

		result[snapID] = snapInfo
	}

	return result, grandTotalBytes, grandTotalInodes, nil
}

// calculateDirectoryUsage computes size in bytes and inode count by traversing the directory in read-only mode.
func (c *Checker) calculateDirectoryUsage(ctx context.Context, dirPath string) (int64, int64) {
	var totalBytes int64
	var totalInodes int64

	_ = filepath.WalkDir(dirPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Skip unreadable files/directories gracefully without stopping scan
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		totalInodes++
		if !d.IsDir() {
			info, err := d.Info()
			if err == nil {
				totalBytes += info.Size()
			}
		}
		return nil
	})

	return totalBytes, totalInodes
}

// parseMountInfo reads and parses /proc/[pid]/mountinfo, extracting OverlayFS mounts.
func (c *Checker) parseMountInfo(ctx context.Context) ([]OverlayMountInfo, error) {
	file, err := os.Open(c.mountinfoPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var overlays []OverlayMountInfo
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		mount, isOverlay, err := parseMountInfoLine(line)
		if err != nil {
			continue
		}

		if isOverlay {
			overlays = append(overlays, mount)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return overlays, nil
}

// parseMountInfoLine parses a single line of Linux mountinfo.
// Format:
// 36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw,errors=continue
// (1)(2) (3)   (4)    (5)      (6)      (7)   (8) (9)   (10)         (11)
func parseMountInfoLine(line string) (OverlayMountInfo, bool, error) {
	fields := strings.Fields(line)
	if len(fields) < 7 {
		return OverlayMountInfo{}, false, errors.New("insufficient fields in mountinfo line")
	}

	mountID, err := strconv.Atoi(fields[0])
	if err != nil {
		return OverlayMountInfo{}, false, fmt.Errorf("invalid mount ID: %w", err)
	}

	parentID, err := strconv.Atoi(fields[1])
	if err != nil {
		return OverlayMountInfo{}, false, fmt.Errorf("invalid parent ID: %w", err)
	}

	majorMinor := fields[2]
	root := decodeProcEscapes(fields[3])
	mountPoint := decodeProcEscapes(fields[4])
	mountOpts := strings.Split(fields[5], ",")

	// Find the separator "-" separating optional tags from fstype
	sepIdx := -1
	for i := 6; i < len(fields); i++ {
		if fields[i] == "-" {
			sepIdx = i
			break
		}
	}

	if sepIdx == -1 || sepIdx+3 > len(fields) {
		return OverlayMountInfo{}, false, errors.New("missing separator or required fields after separator")
	}

	fsType := fields[sepIdx+1]
	mountSource := decodeProcEscapes(fields[sepIdx+2])
	var superOpts []string
	if sepIdx+3 < len(fields) {
		superOpts = strings.Split(fields[sepIdx+3], ",")
	}

	if fsType != "overlay" {
		return OverlayMountInfo{}, false, nil
	}

	overlay := OverlayMountInfo{
		MountID:      mountID,
		ParentID:     parentID,
		MajorMinor:   majorMinor,
		Root:         root,
		MountPoint:   mountPoint,
		MountOptions: mountOpts,
		FSType:       fsType,
		MountSource:  mountSource,
		SuperOptions: superOpts,
		LowerDirs:    make([]string, 0),
	}

	// Extract lowerdir, upperdir, and workdir from super options
	for _, opt := range superOpts {
		if strings.HasPrefix(opt, "lowerdir=") {
			val := strings.TrimPrefix(opt, "lowerdir=")
			dirs := strings.Split(val, ":")
			for _, d := range dirs {
				overlay.LowerDirs = append(overlay.LowerDirs, decodeProcEscapes(d))
			}
		} else if strings.HasPrefix(opt, "upperdir=") {
			overlay.UpperDir = decodeProcEscapes(strings.TrimPrefix(opt, "upperdir="))
		} else if strings.HasPrefix(opt, "workdir=") {
			overlay.WorkDir = decodeProcEscapes(strings.TrimPrefix(opt, "workdir="))
		}
	}

	return overlay, true, nil
}

// decodeProcEscapes decodes standard octal escape sequences used in Linux /proc/mountinfo (e.g. \040 -> space).
func decodeProcEscapes(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}

	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == '\\' && i+3 < len(s) && isOctalDigit(s[i+1]) && isOctalDigit(s[i+2]) && isOctalDigit(s[i+3]) {
			val, err := strconv.ParseInt(s[i+1:i+4], 8, 8)
			if err == nil {
				sb.WriteByte(byte(val))
				i += 4
				continue
			}
		}
		sb.WriteByte(s[i])
		i++
	}
	return sb.String()
}

func isOctalDigit(b byte) bool {
	return b >= '0' && b <= '7'
}
