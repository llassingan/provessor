package repository

import (
	"context"
	"database/sql"

	"github.com/llassingan/provessor/internal/model"
)

type AuditLogRepository struct {
	db *sql.DB
}

func NewAuditLogRepository(db *sql.DB) *AuditLogRepository {
	return &AuditLogRepository{db: db}
}

func (r *AuditLogRepository) Create(ctx context.Context, entry model.AuditLog) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO audit_log (operation, resource_type, resource_id, provider, status, error_message)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		entry.Operation, entry.ResourceType, entry.ResourceID, entry.Provider, entry.Status, entry.ErrorMessage,
	)
	return err
}

// Log writes an audit entry and silently ignores write errors so that
// audit failures never break primary operations.
func (r *AuditLogRepository) Log(ctx context.Context, entry model.AuditLog) {
	_ = r.Create(ctx, entry)
}

func (r *AuditLogRepository) List(ctx context.Context, limit, offset int) ([]model.AuditLog, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, operation, resource_type, resource_id, provider, status, error_message, created_at
		 FROM audit_log ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []model.AuditLog
	for rows.Next() {
		var e model.AuditLog
		if err := rows.Scan(&e.ID, &e.Operation, &e.ResourceType, &e.ResourceID, &e.Provider, &e.Status, &e.ErrorMessage, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}

	if entries == nil {
		entries = []model.AuditLog{}
	}
	return entries, rows.Err()
}
