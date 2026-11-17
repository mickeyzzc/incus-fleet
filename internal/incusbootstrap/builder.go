// Package incusbootstrap renders declarative Incus preseed documents.
package incusbootstrap

import (
	"fmt"
	"net"
	"strings"

	"gopkg.in/yaml.v3"
)

// MemberConfig is one per-member cluster configuration key.
type MemberConfig struct {
	Entity string `yaml:"entity"`
	Name   string `yaml:"name"`
	Key    string `yaml:"key"`
	Value  string `yaml:"value"`
}

// JoinOptions builds a join preseed.
type JoinOptions struct {
	Member        string
	Address       string
	StorageSource string
	Token         string
	MemberConfig  []MemberConfig
}

// InitOptions builds a new cluster preseed.
type InitOptions struct {
	Member        string
	Address       string
	StorageSource string
	NetworkCIDR   string
}

// Join renders the preseed used by `incus admin init --preseed` on a joining
// member.
func Join(opts JoinOptions) (string, error) {
	if strings.TrimSpace(opts.Member) == "" || strings.TrimSpace(opts.Address) == "" || strings.TrimSpace(opts.Token) == "" {
		return "", fmt.Errorf("incusbootstrap: member, address, and token are required")
	}
	if net.ParseIP(strings.TrimSpace(opts.Address)) == nil {
		return "", fmt.Errorf("incusbootstrap: address %q is invalid", opts.Address)
	}
	memberConfig := append([]MemberConfig(nil), opts.MemberConfig...)
	storage := strings.TrimSpace(opts.StorageSource)
	if storage == "" {
		storage = "/srv/incus"
	}
	foundStorage := false
	for i := range memberConfig {
		if memberConfig[i].Entity == "storage-pool" && memberConfig[i].Key == "source" {
			memberConfig[i].Value = storage
			foundStorage = true
		}
	}
	if !foundStorage {
		memberConfig = append(memberConfig, MemberConfig{
			Entity: "storage-pool", Name: "default", Key: "source", Value: storage,
		})
	}

	doc := map[string]any{
		"cluster": map[string]any{
			"enabled":        true,
			"server_address": net.JoinHostPort(opts.Address, "8443"),
			"cluster_token":  opts.Token,
			"member_config":  memberConfig,
		},
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// Init renders the preseed for a new bootstrap member.
func Init(opts InitOptions) (string, error) {
	if strings.TrimSpace(opts.Member) == "" || strings.TrimSpace(opts.Address) == "" {
		return "", fmt.Errorf("incusbootstrap: member and address are required")
	}
	if net.ParseIP(strings.TrimSpace(opts.Address)) == nil {
		return "", fmt.Errorf("incusbootstrap: address %q is invalid", opts.Address)
	}
	storage := strings.TrimSpace(opts.StorageSource)
	if storage == "" {
		storage = "/srv/incus"
	}
	cidr := strings.TrimSpace(opts.NetworkCIDR)
	if cidr == "" {
		cidr = "10.0.0.1/24"
	}
	if _, _, err := net.ParseCIDR(cidr); err != nil {
		return "", fmt.Errorf("incusbootstrap: network CIDR %q is invalid", cidr)
	}

	doc := map[string]any{
		"config": map[string]any{
			"core.https_address":          net.JoinHostPort(opts.Address, "8443"),
			"images.auto_update_interval": 24,
		},
		"storage_pools": []map[string]any{{
			"name": "default", "driver": "dir",
			"config": map[string]string{"source": storage},
		}},
		"networks": []map[string]any{{
			"name": "incusbr0", "type": "bridge",
			"config": map[string]string{
				"ipv4.address": cidr,
				"ipv4.nat":     "true",
				"ipv6.address": "none",
			},
		}},
		"profiles": []map[string]any{{
			"name": "default",
			"devices": map[string]any{
				"root": map[string]string{"path": "/", "pool": "default", "type": "disk"},
				"eth0": map[string]string{"name": "eth0", "nictype": "bridged", "parent": "incusbr0", "type": "nic"},
			},
		}},
		"cluster": map[string]any{"enabled": true, "server_name": opts.Member},
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		return "", err
	}

	return string(data), nil
}
