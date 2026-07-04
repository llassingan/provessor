package model

import "time"

type AuditLog struct {
	ID           int64     `json:"id"`
	Operation    string    `json:"operation"`
	ResourceType string    `json:"resource_type"`
	ResourceID   int64     `json:"resource_id"`
	Provider     string    `json:"provider"`
	Status       string    `json:"status"`
	ErrorMessage string    `json:"error_message"`
	CreatedAt    time.Time `json:"created_at"`
}
