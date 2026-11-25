package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// RegisteredCluster is one cluster added at runtime through the web UI.
// Config-file clusters and registered clusters merge at startup.
type RegisteredCluster struct {
	Name          string    `json:"name"`
	DisplayName   string    `json:"display_name,omitempty"`
	Endpoint      string    `json:"endpoint"`
	Project       string    `json:"project,omitempty"`
	TLSCert       string    `json:"tls_cert"`
	TLSKey        string    `json:"tls_key"`
	TLSServerCert string    `json:"tls_server_cert,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

const clusterSchema = `
CREATE TABLE IF NOT EXISTS registered_clusters (
	name TEXT PRIMARY KEY,
	display_name TEXT NOT NULL DEFAULT '',
	endpoint TEXT NOT NULL,
	project TEXT NOT NULL DEFAULT '',
	tls_cert TEXT NOT NULL,
	tls_key TEXT NOT NULL,
	tls_server_cert TEXT NOT NULL DEFAULT '',
	created_at BIGINT NOT NULL
)`

// InsertCluster persists one dynamically registered cluster.
func (s *SQLStore) InsertCluster(ctx context.Context, cluster *RegisteredCluster) error {
	cluster.CreatedAt = time.Now().UTC()

	const query = `
INSERT INTO registered_clusters (name, display_name, endpoint, project, tls_cert, tls_key, tls_server_cert, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, s.rebind(query), //nolint:gosec // query is a constant
		cluster.Name, cluster.DisplayName, cluster.Endpoint, cluster.Project,
		cluster.TLSCert, cluster.TLSKey, cluster.TLSServerCert, cluster.CreatedAt.Unix(),
	)
	if err != nil {
		return fmt.Errorf("store: insert cluster: %w", err)
	}

	return nil
}

// ListClusters returns every dynamically registered cluster.
func (s *SQLStore) ListClusters(ctx context.Context) ([]*RegisteredCluster, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT name, display_name, endpoint, project, tls_cert, tls_key, tls_server_cert, created_at
FROM registered_clusters ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("store: list clusters: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []*RegisteredCluster{}
	for rows.Next() {
		var cluster RegisteredCluster
		var created int64
		if err := rows.Scan(&cluster.Name, &cluster.DisplayName, &cluster.Endpoint, &cluster.Project,
			&cluster.TLSCert, &cluster.TLSKey, &cluster.TLSServerCert, &created); err != nil {
			return nil, fmt.Errorf("store: scan cluster: %w", err)
		}
		cluster.CreatedAt = time.Unix(created, 0).UTC()
		out = append(out, &cluster)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list clusters: %w", err)
	}

	return out, nil
}

// GetCluster loads one registered cluster by name.
func (s *SQLStore) GetCluster(ctx context.Context, name string) (*RegisteredCluster, error) {
	const getQuery = `
SELECT name, display_name, endpoint, project, tls_cert, tls_key, tls_server_cert, created_at
FROM registered_clusters WHERE name = ?`
	row := s.db.QueryRowContext(ctx, s.rebind(getQuery), name) //nolint:gosec // query is a constant

	var cluster RegisteredCluster
	var created int64
	err := row.Scan(&cluster.Name, &cluster.DisplayName, &cluster.Endpoint, &cluster.Project,
		&cluster.TLSCert, &cluster.TLSKey, &cluster.TLSServerCert, &created)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("store: cluster %q: %w", name, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("store: get cluster: %w", err)
	}

	cluster.CreatedAt = time.Unix(created, 0).UTC()

	return &cluster, nil
}

// SetClusterDisplayName stores the operator-facing name for any cluster,
// including config-file clusters. The Incus identifier in the name column stays
// untouched.
func (s *SQLStore) SetClusterDisplayName(ctx context.Context, cluster, displayName string) error {
	const query = `
INSERT INTO cluster_display_names (cluster, display_name)
VALUES (?, ?)
ON CONFLICT(cluster) DO UPDATE SET display_name = excluded.display_name`
	if _, err := s.db.ExecContext(ctx, s.rebind(query), cluster, displayName); err != nil {
		return fmt.Errorf("store: set cluster display name: %w", err)
	}

	return nil
}

// ListClusterDisplayNames returns every persisted display-name override.
func (s *SQLStore) ListClusterDisplayNames(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT cluster, display_name FROM cluster_display_names`)
	if err != nil {
		return nil, fmt.Errorf("store: list cluster display names: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string]string{}
	for rows.Next() {
		var cluster, displayName string
		if err := rows.Scan(&cluster, &displayName); err != nil {
			return nil, fmt.Errorf("store: scan cluster display name: %w", err)
		}
		out[cluster] = displayName
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list cluster display names rows: %w", err)
	}

	return out, nil
}

// DeleteClusterDisplayName removes a stale override after a cluster is
// unregistered.
func (s *SQLStore) DeleteClusterDisplayName(ctx context.Context, cluster string) error {
	const query = `DELETE FROM cluster_display_names WHERE cluster = ?`
	if _, err := s.db.ExecContext(ctx, s.rebind(query), cluster); err != nil {
		return fmt.Errorf("store: delete cluster display name: %w", err)
	}

	return nil
}

// DeleteCluster removes one registered cluster.
func (s *SQLStore) DeleteCluster(ctx context.Context, name string) error {
	const deleteQuery = `DELETE FROM registered_clusters WHERE name = ?`
	result, err := s.db.ExecContext(ctx, s.rebind(deleteQuery), name) //nolint:gosec // query is a constant
	if err != nil {
		return fmt.Errorf("store: delete cluster: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: delete cluster: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("store: cluster %q: %w", name, ErrNotFound)
	}

	return nil
}
