package model

import "time"

type VPSResource struct {
	ID           int64     `json:"id"`
	VPSID        int64     `json:"vps_id"`
	ResourceType string    `json:"resource_type"`
	ResourceOCID string    `json:"resource_ocid"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
}
