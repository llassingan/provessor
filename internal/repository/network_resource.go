package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/llassingan/provessor/internal/model"
)

type NetworkResourceRepository struct {
	db *sql.DB
}

func NewNetworkResourceRepository(db *sql.DB) *NetworkResourceRepository {
	return &NetworkResourceRepository{db: db}
}

func (r *NetworkResourceRepository) Create(ctx context.Context, networkID int64, resourceType, ocid string) (int64, error) {
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO network_resources (network_id, resource_type, resource_ocid) VALUES (?, ?, ?)`,
		networkID, resourceType, ocid,
	)
	if err != nil {
		return 0, fmt.Errorf("insert network_resource: %w", err)
	}
	return result.LastInsertId()
}

func (r *NetworkResourceRepository) GetByNetworkAndType(ctx context.Context, networkID int64, resourceType string) (*model.NetworkResource, error) {
	var nr model.NetworkResource
	err := r.db.QueryRowContext(ctx,
		`SELECT id, network_id, resource_type, resource_ocid, status, created_at
		 FROM network_resources WHERE network_id = ? AND resource_type = ?`,
		networkID, resourceType,
	).Scan(&nr.ID, &nr.NetworkID, &nr.ResourceType, &nr.ResourceOCID, &nr.Status, &nr.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query network_resource: %w", err)
	}
	return &nr, nil
}

func (r *NetworkResourceRepository) ListByNetwork(ctx context.Context, networkID int64) ([]model.NetworkResource, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, network_id, resource_type, resource_ocid, status, created_at
		 FROM network_resources WHERE network_id = ? ORDER BY id ASC`,
		networkID,
	)
	if err != nil {
		return nil, fmt.Errorf("query network_resources: %w", err)
	}
	defer rows.Close()

	var resources []model.NetworkResource
	for rows.Next() {
		var nr model.NetworkResource
		if err := rows.Scan(&nr.ID, &nr.NetworkID, &nr.ResourceType, &nr.ResourceOCID, &nr.Status, &nr.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan network_resource: %w", err)
		}
		resources = append(resources, nr)
	}

	if resources == nil {
		resources = []model.NetworkResource{}
	}
	return resources, rows.Err()
}

func (r *NetworkResourceRepository) UpdateStatus(ctx context.Context, ocid, status string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE network_resources SET status = ? WHERE resource_ocid = ?`,
		status, ocid,
	)
	return err
}

func (r *NetworkResourceRepository) MarkDeleted(ctx context.Context, ocid string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE network_resources SET status = 'deleted' WHERE resource_ocid = ?`,
		ocid,
	)
	return err
}
