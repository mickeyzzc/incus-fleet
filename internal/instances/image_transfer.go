package instances

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	"github.com/mickeyzzc/incus-fleet/internal/clusters"
)

const (
	importTimeout  = 30 * time.Minute
	imageCopyWait  = 30 * time.Minute
	waitPollPeriod = 200 * time.Millisecond
)

// ImportImage streams an uploaded image tarball into a cluster and returns
// the fingerprint once the daemon finished processing it.
func (s *Service) ImportImage(
	ctx context.Context,
	clusterName, filename, imageType, alias string,
	reader io.Reader,
) (string, error) {
	filename = strings.TrimSpace(filename)
	imageType = strings.TrimSpace(imageType)
	alias = strings.TrimSpace(alias)

	if filename == "" {
		return "", fmt.Errorf("%w: image filename is required", ErrInvalidRequest)
	}

	switch imageType {
	case string(api.InstanceTypeContainer), string(api.InstanceTypeVM):
	default:
		return "", fmt.Errorf("%w: image type must be container or virtual-machine", ErrInvalidRequest)
	}

	client, err := s.client(clusterName)
	if err != nil {
		return "", err
	}

	post := api.ImagesPost{Filename: filename}
	if alias != "" {
		post.Aliases = []api.ImageAlias{{Name: alias}}
	}

	ctx, cancel := context.WithTimeout(ctx, importTimeout)
	defer cancel()

	op, err := client.CreateImage(post, &incus.ImageCreateArgs{
		MetaFile: reader,
		MetaName: filename,
		Type:     imageType,
	})
	if err != nil {
		return "", fmt.Errorf("instances: import image %s: %w", clusterName, err)
	}

	fingerprint, waitErr := waitImageFingerprint(ctx, op)
	if waitErr != nil {
		return "", fmt.Errorf("instances: import image %s: %w", clusterName, waitErr)
	}

	return fingerprint, nil
}

// CopyImageTo distributes an image from one cluster to another.
func (s *Service) CopyImageTo(ctx context.Context, clusterName, fingerprint, targetCluster, alias string) error {
	fingerprint = strings.TrimSpace(fingerprint)
	targetCluster = strings.TrimSpace(targetCluster)
	alias = strings.TrimSpace(alias)

	if fingerprint == "" {
		return fmt.Errorf("%w: image fingerprint is required", ErrInvalidRequest)
	}
	if targetCluster == "" {
		return fmt.Errorf("%w: target cluster is required", ErrInvalidRequest)
	}
	if targetCluster == clusterName {
		return fmt.Errorf("%w: target cluster must differ from the source", ErrInvalidRequest)
	}

	sourceConn, err := s.clusters.Connection(clusterName)
	if err != nil {
		return err
	}
	sourceRaw := sourceConn.RawClient()
	if sourceRaw == nil {
		return fmt.Errorf("%w: %s has no raw client for image transfer", clusters.ErrUnavailable, clusterName)
	}

	targetClient, err := s.client(targetCluster)
	if err != nil {
		return err
	}

	sourceClient, err := sourceConn.Client()
	if err != nil {
		return err
	}

	image, _, err := sourceClient.GetImage(fingerprint)
	if err != nil {
		if isNotFound(err) {
			return fmt.Errorf("%w: %s:%s", ErrNotFound, clusterName, fingerprint)
		}

		return fmt.Errorf("instances: get image %s:%s: %w", clusterName, fingerprint, err)
	}

	args := &incus.ImageCopyArgs{CopyAliases: true}
	if alias != "" {
		args.Aliases = []api.ImageAlias{{Name: alias}}
	}

	ctx, cancel := context.WithTimeout(ctx, imageCopyWait)
	defer cancel()

	remote, err := targetClient.CopyImage(sourceRaw, *image, args)
	if err != nil {
		return fmt.Errorf("instances: copy image %s -> %s: %w", clusterName, targetCluster, err)
	}

	if err := waitImageRemote(ctx, remote, "copy image "+clusterName+"->"+targetCluster); err != nil {
		return err
	}

	return nil
}

// waitImageRemote waits for a remote image transfer operation, honouring
// context cancellation (the SDK remote wait has no context variant).
func waitImageRemote(ctx context.Context, remote incus.RemoteOperation, what string) error {
	if remote == nil {
		return nil
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- remote.Wait()
	}()

	select {
	case err := <-waitCh:
		if err != nil {
			return fmt.Errorf("instances: %s: %w", what, err)
		}

		return nil
	case <-ctx.Done():
		return fmt.Errorf("instances: %s: %w", what, ctx.Err())
	}
}

// waitImageFingerprint waits for an image create operation and pulls the
// fingerprint from its metadata.
func waitImageFingerprint(ctx context.Context, op incus.Operation) (string, error) {
	if err := op.WaitContext(ctx); err != nil {
		return "", err
	}

	opAPI := op.Get()
	if fingerprint, ok := opAPI.Metadata["fingerprint"].(string); ok && fingerprint != "" {
		return fingerprint, nil
	}

	return "", nil
}
