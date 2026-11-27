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
	// Copier imports a missing alias from another fleet cluster. It is
	// optional; without it Resolve remains a read-only lookup.
	Copier ImageCopier
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

// Import copies an alias that is missing on the target cluster from another
// online fleet cluster. It returns the source fingerprint so the install can
// continue immediately; Incus copies the aliases together with the image.
func (r ClusterImageResolver) Import(ctx context.Context, cluster, ref string) (string, error) {
	if r.Manager == nil || r.Copier == nil {
		return "", fmt.Errorf("%w: image copier is not configured", ErrInvalidRequest)
	}
	target := strings.TrimSpace(cluster)
	alias := strings.TrimSpace(ref)
	if target == "" || alias == "" {
		return "", fmt.Errorf("%w: target cluster and image reference are required", ErrInvalidRequest)
	}
	for _, summary := range r.Manager.List() {
		if !summary.Online || summary.Name == target {
			continue
		}
		fingerprint, err := r.Resolve(ctx, summary.Name, alias)
		if err != nil {
			continue
		}
		copyAlias := alias
		if fingerprint == alias {
			copyAlias = "" // a fingerprint reference does not need an alias
		}
		if err := r.Copier.CopyImageTo(ctx, summary.Name, fingerprint, target, copyAlias); err != nil {
			return "", fmt.Errorf("plugininstall: copy image %s from %s: %w", alias, summary.Name, err)
		}
		return fingerprint, nil
	}

	return "", fmt.Errorf("%w: image %q is not available on any cluster", ErrInvalidRequest, alias)
}
