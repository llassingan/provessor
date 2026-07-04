package model

import "time"

type Network struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	Region     string    `json:"region"`
	CIDRVCN    string    `json:"cidr_vcn"`
	CIDRSubnet string    `json:"cidr_subnet"`
	VCNOCID    string    `json:"vcn_ocid"`
	SubnetOCID string    `json:"subnet_ocid"`
	Status            string    `json:"status"`
	Provider          string    `json:"provider"`
	ProvisioningState string    `json:"provisioning_state"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}
