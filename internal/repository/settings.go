package repository

import (
	"context"
	"database/sql"
	"time"

	"vps-store/internal/model"
)

type SettingsRepository struct {
	db *sql.DB
}

func NewSettingsRepository(db *sql.DB) *SettingsRepository {
	return &SettingsRepository{db: db}
}

func (r *SettingsRepository) Get(ctx context.Context) (*model.Settings, error) {
	var s model.Settings
	err := r.db.QueryRowContext(ctx,
		`SELECT id, tenancy_ocid, user_ocid, fingerprint, private_key, region,
		        compartment_ocid, api_base_url, api_token,
		        created_at, updated_at
		 FROM settings WHERE id = 1`,
	).Scan(
		&s.ID, &s.TenancyOCID, &s.UserOCID, &s.Fingerprint, &s.PrivateKey,
		&s.Region, &s.CompartmentOCID,
		&s.APIBaseURL, &s.APIToken,
		&s.CreatedAt, &s.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return &model.Settings{ID: 1}, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *SettingsRepository) Update(ctx context.Context, s *model.Settings) error {
	s.UpdatedAt = time.Now().UTC()
	_, err := r.db.ExecContext(ctx,
		`UPDATE settings SET
			tenancy_ocid = ?, user_ocid = ?, fingerprint = ?, private_key = ?,
			region = ?, compartment_ocid = ?,
			api_base_url = ?, api_token = ?,
			updated_at = ?
		 WHERE id = 1`,
		s.TenancyOCID, s.UserOCID, s.Fingerprint, s.PrivateKey,
		s.Region, s.CompartmentOCID,
		s.APIBaseURL, s.APIToken, s.UpdatedAt,
	)
	return err
}
