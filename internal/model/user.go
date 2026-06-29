package model

import "time"

type User struct {
	ID             int64      `json:"id"`
	Email          string     `json:"email"`
	PasswordHash   string     `json:"-"` // NEVER serialize
	FailedAttempts int        `json:"-"`
	LockedUntil    *time.Time `json:"-"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}
