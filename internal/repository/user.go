package repository

import (
	"context"
	"database/sql"
	"time"

	"vps-store/internal/model"
)

type UserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) CreateUser(ctx context.Context, email, passwordHash string) (*model.User, error) {
	now := time.Now().UTC()
	result, err := r.db.ExecContext(ctx,
		`INSERT INTO users (email, password_hash, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		email, passwordHash, now, now,
	)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &model.User{
		ID:           id,
		Email:        email,
		PasswordHash: passwordHash,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	var u model.User
	err := r.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, failed_attempts, locked_until, created_at, updated_at FROM users WHERE email = ?`,
		email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.FailedAttempts, &u.LockedUntil, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *UserRepository) GetByID(ctx context.Context, id int64) (*model.User, error) {
	var u model.User
	err := r.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, failed_attempts, locked_until, created_at, updated_at FROM users WHERE id = ?`,
		id,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.FailedAttempts, &u.LockedUntil, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *UserRepository) Count(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (r *UserRepository) IncrementFailedAttempts(ctx context.Context, id int64, lockedUntil *time.Time) error {
	var err error
	if lockedUntil != nil {
		_, err = r.db.ExecContext(ctx,
			`UPDATE users SET failed_attempts = failed_attempts + 1, locked_until = ?, updated_at = ? WHERE id = ?`,
			lockedUntil.UTC(), time.Now().UTC(), id,
		)
	} else {
		_, err = r.db.ExecContext(ctx,
			`UPDATE users SET failed_attempts = failed_attempts + 1, updated_at = ? WHERE id = ?`,
			time.Now().UTC(), id,
		)
	}
	return err
}

func (r *UserRepository) ResetFailedAttempts(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET failed_attempts = 0, locked_until = NULL, updated_at = ? WHERE id = ?`,
		time.Now().UTC(), id,
	)
	return err
}
