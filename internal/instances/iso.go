package instances

import (
	"context"
	"fmt"
	"io"
	"strings"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

const isoVolumeType = "custom"

// ISOSummary describes one installer ISO stored as a custom volume.
type ISOSummary struct {
	Name        string `json:"name"`
	Pool        string `json:"pool"`
	Description string `json:"description,omitempty"`
	Size        string `json:"size,omitempty"`
}

// LaunchISORequest creates an empty virtual machine that boots from an ISO.
type LaunchISORequest struct {
	Name        string `json:"name"`
	ISOName     string `json:"iso_name"`
	StoragePool string `json:"storage_pool,omitempty"`
	Network     string `json:"network,omitempty"`
	RootDisk    string `json:"root_disk,omitempty"`
	Start       *bool  `json:"start,omitempty"`
}

// ImportISO uploads an ISO image into a cluster as a custom storage volume.
// The volume name is derived from the file name and returned.
func (s *Service) ImportISO(ctx context.Context, clusterName, pool, filename string, reader io.Reader) (string, error) {
	filename = strings.TrimSpace(filename)
	pool = strings.TrimSpace(pool)
	if filename == "" {
		return "", fmt.Errorf("%w: iso filename is required", ErrInvalidRequest)
	}
	if pool == "" {
		return "", fmt.Errorf("%w: storage pool is required", ErrInvalidRequest)
	}

	client, err := s.client(clusterName)
	if err != nil {
		return "", err
	}

	name := strings.TrimSuffix(filename, ".iso")
	if name == "" {
		name = filename
	}

	ctx, cancel := context.WithTimeout(ctx, importTimeout)
	defer cancel()

	op, err := client.CreateStoragePoolVolumeFromISO(pool, incus.StorageVolumeBackupArgs{
		Name:       name,
		BackupFile: reader,
	})
	if err != nil {
		return "", fmt.Errorf("instances: import iso %s:%s: %w", clusterName, filename, err)
	}
	if err := waitOperation(ctx, op, "import iso "+name); err != nil {
		return "", err
	}

	return name, nil
}

// ListISOs lists installer ISO volumes across all storage pools.
func (s *Service) ListISOs(clusterName string) ([]ISOSummary, error) {
	client, err := s.client(clusterName)
	if err != nil {
		return nil, err
	}

	pools, err := client.GetStoragePoolNames()
	if err != nil {
		return nil, fmt.Errorf("instances: list storage pools for %s: %w", clusterName, err)
	}

	var isos []ISOSummary
	for _, pool := range pools {
		volumes, err := client.GetStoragePoolVolumes(pool)
		if err != nil {
			return nil, fmt.Errorf("instances: list volumes %s:%s: %w", clusterName, pool, err)
		}

		for i := range volumes {
			if volumes[i].ContentType != "iso" {
				continue
			}
			isos = append(isos, ISOSummary{
				Name:        volumes[i].Name,
				Pool:        pool,
				Description: volumes[i].Description,
				Size:        volumes[i].Config["size"],
			})
		}
	}

	return isos, nil
}

// DeleteISO removes one ISO volume from its pool.
func (s *Service) DeleteISO(clusterName, pool, name string) error {
	name = strings.TrimSpace(name)
	pool = strings.TrimSpace(pool)
	if name == "" {
		return fmt.Errorf("%w: iso volume name is required", ErrInvalidRequest)
	}
	if pool == "" {
		return fmt.Errorf("%w: storage pool is required", ErrInvalidRequest)
	}

	client, err := s.client(clusterName)
	if err != nil {
		return err
	}

	if err := client.DeleteStoragePoolVolume(pool, isoVolumeType, name); err != nil {
		if isNotFound(err) {
			return fmt.Errorf("%w: %s:%s/%s", ErrNotFound, clusterName, pool, name)
		}

		return fmt.Errorf("instances: delete iso %s:%s/%s: %w", clusterName, pool, name, err)
	}

	return nil
}

// LaunchFromISO creates an empty VM with the ISO attached as the boot device
// so the installer starts on first power-on.
func (s *Service) LaunchFromISO(ctx context.Context, clusterName string, req LaunchISORequest) (Detail, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.ISOName = strings.TrimSpace(req.ISOName)
	req.StoragePool = strings.TrimSpace(req.StoragePool)
	if req.StoragePool == "" {
		req.StoragePool = "default"
	}
	if req.Name == "" {
		return Detail{}, fmt.Errorf("%w: instance name is required", ErrInvalidRequest)
	}
	if req.ISOName == "" {
		return Detail{}, fmt.Errorf("%w: iso volume name is required", ErrInvalidRequest)
	}

	client, err := s.client(clusterName)
	if err != nil {
		return Detail{}, err
	}

	pools, err := client.GetStoragePoolNames()
	if err != nil {
		return Detail{}, fmt.Errorf("instances: list storage pools for %s: %w", clusterName, err)
	}
	if !containsString(pools, req.StoragePool) {
		return Detail{}, fmt.Errorf("%w: storage pool %q does not exist on %s", ErrInvalidRequest, req.StoragePool, clusterName)
	}

	root := map[string]string{"type": "disk", "path": "/", "pool": req.StoragePool}
	if req.RootDisk != "" {
		root["size"] = req.RootDisk
	}
	devices := api.DevicesMap{
		"root": root,
		"iso0": {
			"type":          "disk",
			"pool":          req.StoragePool,
			"source":        req.ISOName,
			"boot.priority": "1",
		},
	}

	if network := strings.TrimSpace(req.Network); network != "" {
		networks, err := client.GetNetworks()
		if err != nil {
			return Detail{}, fmt.Errorf("instances: list networks for %s: %w", clusterName, err)
		}

		var target *api.Network
		for i := range networks {
			if networks[i].Name == network {
				target = &networks[i]
				break
			}
		}
		if target == nil {
			return Detail{}, fmt.Errorf("%w: network %q does not exist on %s", ErrInvalidRequest, network, clusterName)
		}

		if target.Managed {
			devices["eth0"] = map[string]string{"type": "nic", "network": network}
		} else {
			devices["eth0"] = map[string]string{"type": "nic", "nictype": "macvlan", "parent": network}
		}
	}

	start := true
	if req.Start != nil {
		start = *req.Start
	}

	post := api.InstancesPost{
		Name: req.Name,
		Type: api.InstanceTypeVM,
		InstancePut: api.InstancePut{
			Devices: devices,
		},
		Source: api.InstanceSource{Type: "none"},
		Start:  start,
	}

	ctx, cancel := context.WithTimeout(ctx, createTimeout)
	defer cancel()

	op, err := client.CreateInstance(post)
	if err != nil {
		return Detail{}, fmt.Errorf("instances: create %s:%s: %w", clusterName, req.Name, err)
	}
	if err := waitOperation(ctx, op, "create "+req.Name); err != nil {
		return Detail{}, err
	}

	return s.Get(clusterName, req.Name)
}
