package model

import "time"

type NetworkResource struct {
	ID           int64     `json:"id"`
	NetworkID    int64     `json:"network_id"`
	ResourceType string    `json:"resource_type"`
	ResourceOCID string    `json:"resource_ocid"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
}
