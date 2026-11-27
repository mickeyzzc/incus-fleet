package plugininstall

import (
	"context"
	"fmt"
	"strings"

	"github.com/mickeyzzc/incus-fleet/internal/clusters"
)

// ClusterImageResolver resolves a manifest image alias or fingerprint on the
// target cluster using the same client path as the image API.
type ClusterImageResolver struct {
	Manager *clusters.Manager
}

// Resolve returns the fingerprint the instance service expects.
func (r ClusterImageResolver) Resolve(_ context.Context, cluster, ref string) (string, error) {
	if r.Manager == nil {
		return "", fmt.Errorf("%w: cluster manager is not configured", ErrInvalidRequest)
	}
	conn, err := r.Manager.Connection(cluster)
	if err != nil {
		return "", err
	}
	client, err := conn.Client()
	if err != nil {
		return "", err
	}
	images, err := client.GetImages()
	if err != nil {
		return "", fmt.Errorf("plugininstall: list images on %s: %w", cluster, err)
	}
	for _, image := range images {
		if image.Fingerprint == ref {
			return image.Fingerprint, nil
		}
		for _, alias := range image.Aliases {
			if alias.Name == strings.TrimSpace(ref) {
				return image.Fingerprint, nil
			}
		}
	}

	return "", fmt.Errorf("%w: image %q is not available on %s", ErrInvalidRequest, ref, cluster)
}
