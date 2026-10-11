package containerdchecker

import (
	"context"
	"fmt"
	"maps"
	"time"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/snapshots"
)

// ContainerdInspector defines the interface for collecting containerd runtime inventory.
type ContainerdInspector interface {
	Inspect(ctx context.Context) (*RuntimeInventory, error)
}

// SnapshotInfo holds runtime metadata for a single containerd snapshot.
type SnapshotInfo struct {
	Name      string
	Parent    string
	Kind      string // e.g., "Active", "View", "Committed"
	Namespace string
	CreatedAt time.Time
}

// RuntimeInventory aggregates all known runtime state gathered from containerd.
type RuntimeInventory struct {
	KnownSnapshotIDs   map[string]SnapshotInfo
	ActiveContainerIDs []string
	ActiveLeaseIDs     []string
}

// Checker handles read-only inspection of containerd runtime metadata.
type Checker struct {
	socketPath      string
	snapshotterName string
	timeout         time.Duration
}

// NewChecker initializes a new containerd checker instance.
func NewChecker(socketPath string) *Checker {
	return &Checker{
		socketPath:      socketPath,
		snapshotterName: "overlayfs",
		timeout:         2 * time.Second,
	}
}

// Inspect collects the complete runtime inventory across all containerd namespaces.
func (c *Checker) Inspect(ctx context.Context) (*RuntimeInventory, error) {
	opts := []containerd.ClientOpt{}
	if c.timeout > 0 {
		opts = append(opts, containerd.WithTimeout(c.timeout))
	}

	client, err := containerd.New(c.socketPath, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to containerd at %s: %w", c.socketPath, err)
	}
	defer client.Close()

	nsList, err := c.getNamespaces(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve namespaces: %w", err)
	}

	inventory := &RuntimeInventory{
		KnownSnapshotIDs:   make(map[string]SnapshotInfo),
		ActiveContainerIDs: make([]string, 0),
		ActiveLeaseIDs:     make([]string, 0),
	}

	for _, ns := range nsList {
		snaps, err := c.fetchSnapshots(ctx, client, ns)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch snapshots for namespace %s: %w", ns, err)
		}
		maps.Copy(inventory.KnownSnapshotIDs, snaps)

		containers, err := c.fetchContainers(ctx, client, ns)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch containers for namespace %s: %w", ns, err)
		}
		inventory.ActiveContainerIDs = append(inventory.ActiveContainerIDs, containers...)

		activeLeases, err := c.fetchActiveLeases(ctx, client, ns)
		if err != nil {
			return nil, fmt.Errorf("failed to list leases for namespace %s: %w", ns, err)
		}
		inventory.ActiveLeaseIDs = append(inventory.ActiveLeaseIDs, activeLeases...)
	}

	return inventory, nil
}

// getNamespaces retrieves all available namespaces (e.g., "default", "k8s.io", "moby").
func (c *Checker) getNamespaces(ctx context.Context, client *containerd.Client) ([]string, error) {
	return client.NamespaceService().List(ctx)
}

// fetchSnapshots collects all registered snapshot keys for a given namespace.
func (c *Checker) fetchSnapshots(ctx context.Context, client *containerd.Client, ns string) (map[string]SnapshotInfo, error) {
	ctxWithNS := namespaces.WithNamespace(ctx, ns)
	snapshotter := client.SnapshotService(c.snapshotterName)

	result := make(map[string]SnapshotInfo)

	err := snapshotter.Walk(ctxWithNS, func(ctx context.Context, info snapshots.Info) error {
		result[info.Name] = SnapshotInfo{
			Name:      info.Name,
			Parent:    info.Parent,
			Kind:      info.Kind.String(),
			Namespace: ns,
			CreatedAt: info.Created,
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk snapshots for namespace %s: %w", ns, err)
	}

	return result, nil
}

// fetchContainers collects all registered container IDs for a given namespace.
func (c *Checker) fetchContainers(ctx context.Context, client *containerd.Client, ns string) ([]string, error) {
	ctxWithNS := namespaces.WithNamespace(ctx, ns)

	containerList, err := client.ContainerService().List(ctxWithNS)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers for namespace %s: %w", ns, err)
	}

	ids := make([]string, 0, len(containerList))
	for _, ctr := range containerList {
		ids = append(ids, ctr.ID)
	}
	return ids, nil
}

// fetchActiveLeases collects all active leases/locks to prevent false positives.
func (c *Checker) fetchActiveLeases(ctx context.Context, client *containerd.Client, ns string) ([]string, error) {
	ctxWithNS := namespaces.WithNamespace(ctx, ns)

	leaseList, err := client.LeasesService().List(ctxWithNS)
	if err != nil {
		return nil, fmt.Errorf("failed to list leases for namespace %s: %w", ns, err)
	}

	ids := make([]string, 0, len(leaseList))
	for _, l := range leaseList {
		ids = append(ids, l.ID)
	}
	return ids, nil
}
