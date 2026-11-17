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

	_, err := s.db.ExecContext(ctx, `
INSERT INTO registered_clusters (name, display_name, endpoint, project, tls_cert, tls_key, tls_server_cert, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
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
	row := s.db.QueryRowContext(ctx, `
SELECT name, display_name, endpoint, project, tls_cert, tls_key, tls_server_cert, created_at
FROM registered_clusters WHERE name = ?`, name)

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

// DeleteCluster removes one registered cluster.
func (s *SQLStore) DeleteCluster(ctx context.Context, name string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM registered_clusters WHERE name = ?`, name)
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
