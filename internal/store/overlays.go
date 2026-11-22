package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// OverlayMember records one member's overlay participation: the host address
// it holds on the private bridge and the underlay address its VXLAN tunnels
// terminate on.
type OverlayMember struct {
	Member      string `json:"member"`
	BridgeIP    string `json:"bridge_ip"`
	UnderlayIP  string `json:"underlay_ip"`
	UnderlayNIC string `json:"underlay_nic,omitempty"`
}

// OverlayNetwork is one fleet-managed private network inside a cluster.
type OverlayNetwork struct {
	Cluster       string          `json:"cluster"`
	Name          string          `json:"name"`
	Bridge        string          `json:"bridge"`
	VNI           int             `json:"vni"`
	Subnet        string          `json:"subnet"`
	GatewayMember string          `json:"gateway_member"`
	MTU           int             `json:"mtu"`
	DHCPStart     string          `json:"dhcp_start,omitempty"`
	DHCPEnd       string          `json:"dhcp_end,omitempty"`
	Enabled       bool            `json:"enabled"`
	Members       []OverlayMember `json:"members"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

const overlaySchema = `
CREATE TABLE IF NOT EXISTS overlay_networks (
	cluster TEXT NOT NULL,
	name TEXT NOT NULL,
	bridge TEXT NOT NULL,
	vni INTEGER NOT NULL,
	subnet TEXT NOT NULL,
	gateway_member TEXT NOT NULL,
	mtu INTEGER NOT NULL DEFAULT 1450,
	dhcp_start TEXT NOT NULL DEFAULT '',
	dhcp_end TEXT NOT NULL DEFAULT '',
	enabled INTEGER NOT NULL DEFAULT 0,
	members TEXT NOT NULL DEFAULT '[]',
	created_at BIGINT NOT NULL,
	updated_at BIGINT NOT NULL,
	PRIMARY KEY (cluster, name)
)`

// SaveOverlay inserts or replaces one overlay definition.
func (s *SQLStore) SaveOverlay(ctx context.Context, overlay *OverlayNetwork) error {
	now := time.Now().UTC()
	if overlay.CreatedAt.IsZero() {
		overlay.CreatedAt = now
	}
	overlay.UpdatedAt = now

	members, err := json.Marshal(overlay.Members)
	if err != nil {
		return fmt.Errorf("store: encode overlay members: %w", err)
	}

	const query = `
INSERT INTO overlay_networks
	(cluster, name, bridge, vni, subnet, gateway_member, mtu, dhcp_start, dhcp_end, enabled, members, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (cluster, name) DO UPDATE SET
	bridge = excluded.bridge,
	vni = excluded.vni,
	subnet = excluded.subnet,
	gateway_member = excluded.gateway_member,
	mtu = excluded.mtu,
	dhcp_start = excluded.dhcp_start,
	dhcp_end = excluded.dhcp_end,
	enabled = excluded.enabled,
	members = excluded.members,
	updated_at = excluded.updated_at`
	_, err = s.db.ExecContext(ctx, s.rebind(query), //nolint:gosec // query is a constant
		overlay.Cluster, overlay.Name, overlay.Bridge, overlay.VNI, overlay.Subnet,
		overlay.GatewayMember, overlay.MTU, overlay.DHCPStart, overlay.DHCPEnd,
		boolInt(overlay.Enabled), string(members),
		overlay.CreatedAt.Unix(), overlay.UpdatedAt.Unix(),
	)
	if err != nil {
		return fmt.Errorf("store: save overlay: %w", err)
	}

	return nil
}

// GetOverlay loads one overlay by cluster and name.
func (s *SQLStore) GetOverlay(ctx context.Context, cluster, name string) (*OverlayNetwork, error) {
	const query = `
SELECT cluster, name, bridge, vni, subnet, gateway_member, mtu, dhcp_start, dhcp_end,
	enabled, members, created_at, updated_at
FROM overlay_networks WHERE cluster = ? AND name = ?`
	row := s.db.QueryRowContext(ctx, s.rebind(query), cluster, name) //nolint:gosec // query is a constant

	return scanOverlay(row)
}

// ListOverlays returns every overlay of one cluster.
func (s *SQLStore) ListOverlays(ctx context.Context, cluster string) ([]*OverlayNetwork, error) {
	const query = `
SELECT cluster, name, bridge, vni, subnet, gateway_member, mtu, dhcp_start, dhcp_end,
	enabled, members, created_at, updated_at
FROM overlay_networks WHERE cluster = ? ORDER BY name`
	rows, err := s.db.QueryContext(ctx, s.rebind(query), cluster) //nolint:gosec // query is a constant
	if err != nil {
		return nil, fmt.Errorf("store: list overlays: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []*OverlayNetwork{}
	for rows.Next() {
		overlay, err := scanOverlay(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, overlay)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list overlays: %w", err)
	}

	return out, nil
}

// DeleteOverlay removes one overlay definition.
func (s *SQLStore) DeleteOverlay(ctx context.Context, cluster, name string) error {
	const query = `DELETE FROM overlay_networks WHERE cluster = ? AND name = ?`
	result, err := s.db.ExecContext(ctx, s.rebind(query), cluster, name) //nolint:gosec // query is a constant
	if err != nil {
		return fmt.Errorf("store: delete overlay: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: delete overlay: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("store: overlay %q: %w", name, ErrNotFound)
	}

	return nil
}

// overlayScanner is satisfied by both *sql.Row and *sql.Rows.
type overlayScanner interface {
	Scan(dest ...any) error
}

func scanOverlay(row overlayScanner) (*OverlayNetwork, error) {
	var overlay OverlayNetwork
	var enabled int
	var members string
	var created, updated int64
	err := row.Scan(&overlay.Cluster, &overlay.Name, &overlay.Bridge, &overlay.VNI, &overlay.Subnet,
		&overlay.GatewayMember, &overlay.MTU, &overlay.DHCPStart, &overlay.DHCPEnd,
		&enabled, &members, &created, &updated)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("store: overlay: %w", ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("store: scan overlay: %w", err)
	}

	overlay.Enabled = enabled != 0
	overlay.CreatedAt = time.Unix(created, 0).UTC()
	overlay.UpdatedAt = time.Unix(updated, 0).UTC()
	overlay.Members = []OverlayMember{}
	if members != "" {
		if err := json.Unmarshal([]byte(members), &overlay.Members); err != nil {
			return nil, fmt.Errorf("store: decode overlay members: %w", err)
		}
	}

	return &overlay, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
