package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/llassingan/provessor/internal/model"
)

type NetworkRepository struct {
	db *sql.DB
}

func NewNetworkRepository(db *sql.DB) *NetworkRepository {
	return &NetworkRepository{db: db}
}

const MaxNetworks = 15

var cidrBlocks = [][2]string{
	{"10.0.0.0/16", "10.0.0.0/20"},
	{"10.1.0.0/16", "10.1.0.0/20"},
	{"10.2.0.0/16", "10.2.0.0/20"},
	{"10.3.0.0/16", "10.3.0.0/20"},
	{"10.4.0.0/16", "10.4.0.0/20"},
	{"10.5.0.0/16", "10.5.0.0/20"},
	{"10.6.0.0/16", "10.6.0.0/20"},
	{"10.7.0.0/16", "10.7.0.0/20"},
	{"10.8.0.0/16", "10.8.0.0/20"},
	{"10.9.0.0/16", "10.9.0.0/20"},
	{"10.10.0.0/16", "10.10.0.0/20"},
	{"10.11.0.0/16", "10.11.0.0/20"},
	{"10.12.0.0/16", "10.12.0.0/20"},
	{"10.13.0.0/16", "10.13.0.0/20"},
	{"10.14.0.0/16", "10.14.0.0/20"},
}

func (r *NetworkRepository) Count(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM networks").Scan(&count)
	return count, err
}

func (r *NetworkRepository) allocateCIDRSlot(ctx context.Context) ([2]string, error) {
	existing, err := r.List(ctx)
	if err != nil {
		return [2]string{}, err
	}

	used := make(map[string]bool)
	for _, n := range existing {
		used[n.CIDRVCN] = true
	}

	for _, cidr := range cidrBlocks {
		if !used[cidr[0]] {
			return cidr, nil
		}
	}

	return [2]string{}, fmt.Errorf("all %d network slots are in use", MaxNetworks)
}

func (r *NetworkRepository) Create(ctx context.Context, name, region string) (*model.Network, error) {
	count, err := r.Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count networks: %w", err)
	}
	if count >= MaxNetworks {
		return nil, fmt.Errorf("maximum of %d networks reached", MaxNetworks)
	}

	cidr, err := r.allocateCIDRSlot(ctx)
	if err != nil {
		return nil, err
	}

	result, err := r.db.ExecContext(ctx,
		`INSERT INTO networks (name, region, cidr_vcn, cidr_subnet) VALUES (?, ?, ?, ?)`,
		name, region, cidr[0], cidr[1],
	)
	if err != nil {
		return nil, fmt.Errorf("insert network: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("get last insert id: %w", err)
	}

	return r.Get(ctx, id)
}

func (r *NetworkRepository) List(ctx context.Context) ([]model.Network, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, region, cidr_vcn, cidr_subnet, vcn_ocid, subnet_ocid, status, provider, provisioning_state, created_at, updated_at
		 FROM networks ORDER BY id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query networks: %w", err)
	}
	defer rows.Close()

	var networks []model.Network
	for rows.Next() {
		var n model.Network
		if err := rows.Scan(&n.ID, &n.Name, &n.Region, &n.CIDRVCN, &n.CIDRSubnet,
			&n.VCNOCID, &n.SubnetOCID, &n.Status, &n.Provider, &n.ProvisioningState, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan network: %w", err)
		}
		networks = append(networks, n)
	}

	if networks == nil {
		networks = []model.Network{}
	}
	return networks, rows.Err()
}

func (r *NetworkRepository) ListByStatus(ctx context.Context, statuses []string) ([]model.Network, error) {
	if len(statuses) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(statuses))
	args := make([]interface{}, len(statuses))
	for i, s := range statuses {
		placeholders[i] = "?"
		args[i] = s
	}
	query := "SELECT id, name, region, cidr_vcn, cidr_subnet, vcn_ocid, subnet_ocid, status, provider, provisioning_state, created_at, updated_at FROM networks WHERE status IN (" + strings.Join(placeholders, ",") + ") ORDER BY id ASC"
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var networks []model.Network
	for rows.Next() {
		var n model.Network
		if err := rows.Scan(&n.ID, &n.Name, &n.Region, &n.CIDRVCN, &n.CIDRSubnet,
			&n.VCNOCID, &n.SubnetOCID, &n.Status, &n.Provider, &n.ProvisioningState, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, err
		}
		networks = append(networks, n)
	}
	if networks == nil {
		networks = []model.Network{}
	}
	return networks, rows.Err()
}

func (r *NetworkRepository) Get(ctx context.Context, id int64) (*model.Network, error) {
	var n model.Network
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, region, cidr_vcn, cidr_subnet, vcn_ocid, subnet_ocid, status, provider, provisioning_state, created_at, updated_at
		 FROM networks WHERE id = ?`, id,
	).Scan(&n.ID, &n.Name, &n.Region, &n.CIDRVCN, &n.CIDRSubnet,
		&n.VCNOCID, &n.SubnetOCID, &n.Status, &n.Provider, &n.ProvisioningState, &n.CreatedAt, &n.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query network: %w", err)
	}
	return &n, nil
}

func (r *NetworkRepository) Delete(ctx context.Context, id int64) error {
	var vpsCount int
	if err := r.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM vps WHERE network_id = ?", id,
	).Scan(&vpsCount); err != nil {
		return fmt.Errorf("count vps on network: %w", err)
	}
	if vpsCount > 0 {
		return fmt.Errorf("cannot delete network with %d active VPS instances", vpsCount)
	}

	_, err := r.db.ExecContext(ctx, "DELETE FROM networks WHERE id = ?", id)
	return err
}

func (r *NetworkRepository) UpdateProvisionResult(ctx context.Context, id int64, vcnOCID, subnetOCID string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE networks SET vcn_ocid = ?, subnet_ocid = ?, status = 'ready', updated_at = ? WHERE id = ?`,
		vcnOCID, subnetOCID, time.Now().UTC(), id,
	)
	return err
}

func (r *NetworkRepository) UpdateStatus(ctx context.Context, id int64, status string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE networks SET status = ?, updated_at = ? WHERE id = ?`,
		status, time.Now().UTC(), id,
	)
	return err
}

func (r *NetworkRepository) CountVPS(ctx context.Context, id int64) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM vps WHERE network_id = ?", id,
	).Scan(&count)
	return count, err
}

func (r *NetworkRepository) UpdateProvisioningState(ctx context.Context, id int64, state string) error {
	_, err := r.db.ExecContext(ctx, "UPDATE networks SET provisioning_state = ?, updated_at = ? WHERE id = ?", state, time.Now().UTC(), id)
	return err
}
