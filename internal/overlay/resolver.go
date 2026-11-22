package overlay

import (
	"context"
	"fmt"

	"github.com/mickeyzzc/incus-fleet/internal/clusters"
)

// ClusterResolver adapts clusters.Manager to the overlay ClusterLookup.
// Summary() is the only place fleet already translates Incus members into its
// own type, so the adapter reuses it instead of duplicating the mapping.
type ClusterResolver struct {
	Manager interface {
		Connection(name string) (*clusters.Connection, error)
	}
}

// Members returns the live member list of one cluster.
func (r *ClusterResolver) Members(_ context.Context, cluster string) ([]clusters.Member, error) {
	connection, err := r.Manager.Connection(cluster)
	if err != nil {
		return nil, fmt.Errorf("overlay: resolve cluster %s: %w", cluster, err)
	}

	summary := connection.Summary()
	if summary.Error != "" {
		return nil, fmt.Errorf("overlay: cluster %s is unavailable: %s", cluster, summary.Error)
	}

	return summary.Members, nil
}
