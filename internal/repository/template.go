package repository

import (
	"context"
	"database/sql"

	"github.com/llassingan/provessor/internal/model"
)

type TemplateRepository struct {
	db *sql.DB
}

func NewTemplateRepository(db *sql.DB) *TemplateRepository {
	return &TemplateRepository{db: db}
}

func (r *TemplateRepository) List(ctx context.Context) ([]model.Template, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, description, type, logo_url, cloud_init_yaml,
		        shape, default_ocpu, default_memory, boot_volume_size_gb, created_at
		 FROM templates ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var templates []model.Template
	for rows.Next() {
		var t model.Template
		var logoURL sql.NullString
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.Type, &logoURL,
			&t.CloudInitYAML, &t.Shape, &t.DefaultOCPU, &t.DefaultMemory,
			&t.BootVolumeSizeGB, &t.CreatedAt); err != nil {
			return nil, err
		}
		if logoURL.Valid {
			t.LogoURL = logoURL.String
		}
		templates = append(templates, t)
	}
	return templates, rows.Err()
}

func (r *TemplateRepository) Get(ctx context.Context, id int64) (*model.Template, error) {
	var t model.Template
	var logoURL sql.NullString
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, description, type, logo_url, cloud_init_yaml,
		        shape, default_ocpu, default_memory, boot_volume_size_gb, created_at
		 FROM templates WHERE id = ?`, id,
	).Scan(&t.ID, &t.Name, &t.Description, &t.Type, &logoURL,
		&t.CloudInitYAML, &t.Shape, &t.DefaultOCPU, &t.DefaultMemory,
		&t.BootVolumeSizeGB, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	if logoURL.Valid {
		t.LogoURL = logoURL.String
	}
	return &t, nil
}

func (r *TemplateRepository) Create(ctx context.Context, t *model.Template) (*model.Template, error) {
	var logoURL interface{}
	if t.LogoURL != "" {
		logoURL = t.LogoURL
	}
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO templates (name, description, type, logo_url, cloud_init_yaml,
		 shape, default_ocpu, default_memory, boot_volume_size_gb)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.Name, t.Description, t.Type, logoURL, t.CloudInitYAML,
		t.Shape, t.DefaultOCPU, t.DefaultMemory, t.BootVolumeSizeGB,
	)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func (r *TemplateRepository) Count(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM templates`).Scan(&count)
	return count, err
}
