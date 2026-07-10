package model

import (
	"database/sql"
	"time"
)

type VPS struct {
	ID                              int64          `json:"id"`
	DisplayName                     string         `json:"display_name"`
	TemplateID                      int64          `json:"template_id"`
	NetworkID                       NullInt64      `json:"network_id"`
	Shape                           string         `json:"shape"`
	OCPU                            float64        `json:"ocpu"`
	MemoryGB                        float64        `json:"memory_gb"`
	BootVolumeSizeGB                int            `json:"boot_volume_size_gb"`
	OCIInstanceID                   NullString     `json:"oci_instance_id"`
	PublicIP                        NullString     `json:"public_ip"`
	PrivateIP                       NullString     `json:"private_ip"`
	Status                          string         `json:"status"`
	InitialCredentials              NullString     `json:"initial_credentials"`
	CredentialsCallbackTokenHash    sql.NullString `json:"-"`
	CredentialsCallbackTokenExpires sql.NullTime   `json:"-"`
	CredentialsCallbackTokenUsedAt  sql.NullTime   `json:"-"`
	CredentialsReceivedAt           sql.NullTime   `json:"-"`
	SSHPrivateKey                   NullString     `json:"-"`
	SSHUsername                     NullString     `json:"ssh_username"`
	SSHPassword                     NullString     `json:"ssh_password"`
	SSHHostKeyFingerprint           sql.NullString `json:"-"`
	NSGID                           string         `json:"nsg_id"`
	Provider                        string         `json:"provider"`
	ProvisioningState               string         `json:"provisioning_state"`
	CreatedAt                       time.Time      `json:"created_at"`
	UpdatedAt                       time.Time      `json:"updated_at"`
}
