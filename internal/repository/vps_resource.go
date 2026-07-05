package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/llassingan/provessor/internal/model"
)

type VPSResourceRepository struct {
	db *sql.DB
}

func NewVPSResourceRepository(db *sql.DB) *VPSResourceRepository {
	return &VPSResourceRepository{db: db}
}

func (r *VPSResourceRepository) Create(ctx context.Context, vpsID int64, resourceType, ocid string) (int64, error) {
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO vps_resources (vps_id, resource_type, resource_ocid) VALUES (?, ?, ?)`,
		vpsID, resourceType, ocid,
	)
	if err != nil {
		return 0, fmt.Errorf("insert vps_resource: %w", err)
	}
	return result.LastInsertId()
}

func (r *VPSResourceRepository) GetByVPSAndType(ctx context.Context, vpsID int64, resourceType string) (*model.VPSResource, error) {
	var vr model.VPSResource
	err := r.db.QueryRowContext(ctx,
		`SELECT id, vps_id, resource_type, resource_ocid, status, created_at
		 FROM vps_resources WHERE vps_id = ? AND resource_type = ?`,
		vpsID, resourceType,
	).Scan(&vr.ID, &vr.VPSID, &vr.ResourceType, &vr.ResourceOCID, &vr.Status, &vr.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query vps_resource: %w", err)
	}
	return &vr, nil
}

func (r *VPSResourceRepository) ListByVPS(ctx context.Context, vpsID int64) ([]model.VPSResource, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, vps_id, resource_type, resource_ocid, status, created_at
		 FROM vps_resources WHERE vps_id = ? ORDER BY id ASC`,
		vpsID,
	)
	if err != nil {
		return nil, fmt.Errorf("query vps_resources: %w", err)
	}
	defer rows.Close()

	var resources []model.VPSResource
	for rows.Next() {
		var vr model.VPSResource
		if err := rows.Scan(&vr.ID, &vr.VPSID, &vr.ResourceType, &vr.ResourceOCID, &vr.Status, &vr.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan vps_resource: %w", err)
		}
		resources = append(resources, vr)
	}

	if resources == nil {
		resources = []model.VPSResource{}
	}
	return resources, rows.Err()
}

func (r *VPSResourceRepository) UpdateStatus(ctx context.Context, ocid, status string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE vps_resources SET status = ? WHERE resource_ocid = ?`,
		status, ocid,
	)
	return err
}

func (r *VPSResourceRepository) MarkDeleted(ctx context.Context, ocid string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE vps_resources SET status = 'deleted' WHERE resource_ocid = ?`,
		ocid,
	)
	return err
}
