package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/llassingan/provessor/internal/model"
)

type VPSRepository struct {
	db *sql.DB
}

func NewVPSRepository(db *sql.DB) *VPSRepository {
	return &VPSRepository{db: db}
}

func (r *VPSRepository) Create(ctx context.Context, vps *model.VPS) (*model.VPS, error) {
	query := `INSERT INTO vps (display_name, template_id, network_id, shape, ocpu, memory_gb, boot_volume_size_gb, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

	result, err := r.db.ExecContext(ctx, query,
		vps.DisplayName, vps.TemplateID, vps.NetworkID, vps.Shape,
		vps.OCPU, vps.MemoryGB, vps.BootVolumeSizeGB,
		vps.Status,
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

func (r *VPSRepository) List(ctx context.Context, status string) ([]model.VPS, error) {
	var rows *sql.Rows
	var err error

	if status != "" {
		rows, err = r.db.QueryContext(ctx,
			`SELECT id, display_name, template_id, network_id, shape, ocpu, memory_gb, boot_volume_size_gb,
				oci_instance_id, public_ip, private_ip, status, initial_credentials,
				credentials_callback_token_hash, credentials_callback_token_expires_at, credentials_callback_token_used_at, credentials_received_at,
				ssh_username, ssh_password, nsg_id, provider, provisioning_state, created_at, updated_at
			FROM vps WHERE status = ? ORDER BY created_at DESC`, status)
	} else {
		rows, err = r.db.QueryContext(ctx,
			`SELECT id, display_name, template_id, network_id, shape, ocpu, memory_gb, boot_volume_size_gb,
				oci_instance_id, public_ip, private_ip, status, initial_credentials,
				credentials_callback_token_hash, credentials_callback_token_expires_at, credentials_callback_token_used_at, credentials_received_at,
				ssh_username, ssh_password, nsg_id, provider, provisioning_state, created_at, updated_at
			FROM vps ORDER BY created_at DESC`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var vpsList []model.VPS
	for rows.Next() {
		var v model.VPS
		err := rows.Scan(
			&v.ID, &v.DisplayName, &v.TemplateID, &v.NetworkID, &v.Shape, &v.OCPU, &v.MemoryGB, &v.BootVolumeSizeGB,
			&v.OCIInstanceID, &v.PublicIP, &v.PrivateIP, &v.Status, &v.InitialCredentials,
			&v.CredentialsCallbackTokenHash, &v.CredentialsCallbackTokenExpires, &v.CredentialsCallbackTokenUsedAt, &v.CredentialsReceivedAt,
			&v.SSHUsername, &v.SSHPassword,
			&v.NSGID, &v.Provider, &v.ProvisioningState,
			&v.CreatedAt, &v.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		vpsList = append(vpsList, v)
	}

	if vpsList == nil {
		vpsList = []model.VPS{}
	}
	return vpsList, rows.Err()
}

func (r *VPSRepository) Get(ctx context.Context, id int64) (*model.VPS, error) {
	var v model.VPS
	err := r.db.QueryRowContext(ctx,
		`SELECT id, display_name, template_id, network_id, shape, ocpu, memory_gb, boot_volume_size_gb,
			oci_instance_id, public_ip, private_ip, status, initial_credentials,
			credentials_callback_token_hash, credentials_callback_token_expires_at, credentials_callback_token_used_at, credentials_received_at,
			ssh_private_key, ssh_username, ssh_password, nsg_id, provider, provisioning_state, created_at, updated_at
		FROM vps WHERE id = ?`, id,
	).Scan(
		&v.ID, &v.DisplayName, &v.TemplateID, &v.NetworkID, &v.Shape, &v.OCPU, &v.MemoryGB, &v.BootVolumeSizeGB,
		&v.OCIInstanceID, &v.PublicIP, &v.PrivateIP, &v.Status, &v.InitialCredentials,
		&v.CredentialsCallbackTokenHash, &v.CredentialsCallbackTokenExpires, &v.CredentialsCallbackTokenUsedAt, &v.CredentialsReceivedAt,
		&v.SSHPrivateKey, &v.SSHUsername, &v.SSHPassword,
		&v.NSGID, &v.Provider, &v.ProvisioningState,
		&v.CreatedAt, &v.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (r *VPSRepository) Update(ctx context.Context, vps *model.VPS) error {
	query := `UPDATE vps SET display_name=?, template_id=?, network_id=?, shape=?, ocpu=?, memory_gb=?, boot_volume_size_gb=?,
		oci_instance_id=?, public_ip=?, private_ip=?, status=?, initial_credentials=COALESCE(?, initial_credentials), ssh_private_key=?, ssh_username=?, ssh_password=?,
		nsg_id=?, provider=?, provisioning_state=?, updated_at=?
		WHERE id=?`

	_, err := r.db.ExecContext(ctx, query,
		vps.DisplayName, vps.TemplateID, vps.NetworkID, vps.Shape, vps.OCPU, vps.MemoryGB, vps.BootVolumeSizeGB,
		vps.OCIInstanceID, vps.PublicIP, vps.PrivateIP, vps.Status, vps.InitialCredentials,
		vps.SSHPrivateKey, vps.SSHUsername, vps.SSHPassword,
		vps.NSGID, vps.Provider, vps.ProvisioningState,
		time.Now().UTC(), vps.ID,
	)
	return err
}

func (r *VPSRepository) UpdateProvisioningState(ctx context.Context, id int64, state string) error {
	_, err := r.db.ExecContext(ctx, "UPDATE vps SET provisioning_state = ? WHERE id = ?", state, id)
	return err
}

func (r *VPSRepository) UpdateNSGID(ctx context.Context, id int64, nsgID string) error {
	_, err := r.db.ExecContext(ctx, "UPDATE vps SET nsg_id = ? WHERE id = ?", nsgID, id)
	return err
}

func (r *VPSRepository) UpdateStatus(ctx context.Context, id int64, status string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE vps SET status=?, updated_at=? WHERE id=?`,
		status, time.Now().UTC(), id)
	return err
}

func (r *VPSRepository) SetCredentialsCallbackToken(ctx context.Context, id int64, tokenHash string, expiresAt time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE vps
		SET credentials_callback_token_hash=?, credentials_callback_token_expires_at=?,
			credentials_callback_token_used_at=NULL, credentials_received_at=NULL, updated_at=?
		WHERE id=?`,
		tokenHash, expiresAt.UTC(), time.Now().UTC(), id)
	return err
}

func (r *VPSRepository) ConsumeCredentialsCallback(ctx context.Context, id int64, tokenHash string, credentials string) (bool, error) {
	now := time.Now().UTC()
	result, err := r.db.ExecContext(ctx,
		`UPDATE vps
		SET initial_credentials=?, credentials_callback_token_hash=NULL, credentials_callback_token_expires_at=NULL,
			credentials_callback_token_used_at=?, credentials_received_at=?, updated_at=?
		WHERE id=?
			AND credentials_callback_token_hash=?
			AND credentials_callback_token_expires_at > ?
			AND credentials_callback_token_used_at IS NULL
			AND initial_credentials IS NULL
			AND status IN ('provisioning', 'running')`,
		credentials, now, now, now, id, tokenHash, now)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}

func (r *VPSRepository) UpdateCredentials(ctx context.Context, id int64, credentials string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE vps SET initial_credentials=?, status='running', updated_at=? WHERE id=?`,
		credentials, time.Now().UTC(), id)
	return err
}

func (r *VPSRepository) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM vps WHERE id = ?`, id)
	return err
}
