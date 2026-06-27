package repository

import (
	"database/sql"
	"log"
	"os"
	"path/filepath"
	"time"
)

func SeedAll(db *sql.DB, projectRoot string, dev bool) {
	if !dev {
		return
	}

	if err := seedTemplates(db, projectRoot); err != nil {
		log.Printf("seed templates: %v (continuing)", err)
	}

	if err := seedNetworks(db); err != nil {
		log.Printf("seed networks: %v (continuing)", err)
	}

	if err := seedVPS(db); err != nil {
		log.Printf("seed vps: %v (continuing)", err)
	}
}

func seedTemplates(db *sql.DB, projectRoot string) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM templates`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	cloudInitDir := filepath.Join(projectRoot, "ansible", "cloud-init")

	seeds := []struct {
		name        string
		description string
		logoPath    string
		yamlFile    string
		shape       string
		ocpu        float64
		memory      float64
		bootVol     int
	}{
		{"WordPress", "WordPress on Ubuntu with Nginx, PHP-FPM, MySQL", "wordpress", "wordpress.yaml", "VM.Standard.E4.Flex", 1, 8, 50},
		{"Node.js", "Node.js on Ubuntu with Nginx reverse proxy", "nodejs", "nodejs.yaml", "VM.Standard.E4.Flex", 1, 8, 50},
		{"Docker", "Docker on Ubuntu with Docker Compose", "docker", "docker.yaml", "VM.Standard.E4.Flex", 2, 8, 50},
		{"Ubuntu", "Vanilla Ubuntu with essential tools", "ubuntu", "ubuntu.yaml", "VM.Standard.E4.Flex", 1, 4, 30},
	}

	for _, s := range seeds {
		yamlPath := filepath.Join(cloudInitDir, s.yamlFile)
		yamlContent, err := os.ReadFile(yamlPath)
		if err != nil {
			return err
		}

		now := time.Now().UTC()
		_, err = db.Exec(
			`INSERT INTO templates (name, description, type, logo_url, cloud_init_yaml,
			 shape, default_ocpu, default_memory, boot_volume_size_gb, created_at)
			 VALUES (?, ?, 'predefined', ?, ?, ?, ?, ?, ?, ?)`,
			s.name, s.description, s.logoPath, string(yamlContent),
			s.shape, s.ocpu, s.memory, s.bootVol, now,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

func seedNetworks(db *sql.DB) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM networks`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	now := time.Now().UTC()
	seeds := []struct {
		name       string
		region     string
		cidrVCN    string
		cidrSubnet string
		vcnOCID    string
		subnetOCID string
		status     string
	}{
		{"production", "us-ashburn-1", "10.0.0.0/16", "10.0.1.0/24", "ocid1.vcn.dummy.production", "ocid1.subnet.dummy.production", "ready"},
		{"staging", "ap-singapore-1", "10.1.0.0/16", "10.1.1.0/24", "ocid1.vcn.dummy.staging", "ocid1.subnet.dummy.staging", "ready"},
	}

	for _, s := range seeds {
		_, err := db.Exec(
			`INSERT INTO networks (name, region, cidr_vcn, cidr_subnet, vcn_ocid, subnet_ocid, status, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			s.name, s.region, s.cidrVCN, s.cidrSubnet, s.vcnOCID, s.subnetOCID, s.status, now, now,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

func seedVPS(db *sql.DB) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM vps`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	var networkCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM networks`).Scan(&networkCount); err != nil {
		return err
	}
	if networkCount == 0 {
		return nil
	}

	now := time.Now().UTC()
	creds := `{"ssh_user":"root","ssh_password":"changeme123","app_url":"http://10.0.1.100","app_admin":"admin","app_password":"admin123"}`

	type row struct {
		displayName      string
		templateID       int64
		networkID        int64
		shape            string
		ocpu             float64
		memoryGB         float64
		bootVolumeSizeGB int
		ociInstanceID    string
		publicIP         string
		privateIP        string
		status           string
		credentials      string
	}

	seeds := []row{
		{"wp-client-acme", 1, 1, "VM.Standard.E4.Flex", 1, 8, 50, "ocid1.instance.dummy.wp1", "129.146.100.10", "10.0.1.100", "running", creds},
		{"node-api-beta", 2, 1, "VM.Standard.E4.Flex", 2, 16, 50, "ocid1.instance.dummy.node1", "129.146.100.20", "10.0.1.101", "running", creds},
		{"docker-runner-stg", 3, 2, "VM.Standard.E4.Flex", 2, 8, 30, "ocid1.instance.dummy.docker1", "129.146.100.30", "10.1.1.100", "stopped", creds},
		{"ubuntu-devbox", 4, 2, "VM.Standard.E4.Flex", 1, 4, 30, "ocid1.instance.dummy.ubuntu1", "129.146.100.40", "10.1.1.101", "running", creds},
	}

	for _, s := range seeds {
		_, err := db.Exec(
			`INSERT INTO vps
			 (display_name, template_id, network_id, shape, ocpu, memory_gb, boot_volume_size_gb,
			  oci_instance_id, public_ip, private_ip, status, initial_credentials, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			s.displayName, s.templateID, s.networkID, s.shape, s.ocpu, s.memoryGB, s.bootVolumeSizeGB,
			s.ociInstanceID, s.publicIP, s.privateIP, s.status, s.credentials, now, now,
		)
		if err != nil {
			return err
		}
	}

	return nil
}
