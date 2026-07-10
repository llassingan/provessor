package main

import (
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/llassingan/provessor/internal/db"
)

func main() {
	key := resolveEncryptionKey()
	database, err := db.Open("data/provessor.db", key)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	var vpsID int64
	if len(os.Args) > 1 {
		fmt.Sscanf(os.Args[1], "%d", &vpsID)
	}
	if vpsID == 0 {
		vpsID = 5
	}

	var (
		id         int64
		displayName sql.NullString
		publicIP   sql.NullString
		sshUser    sql.NullString
		sshPass    sql.NullString
		sshPrivKey sql.NullString
		status     sql.NullString
	)
	err = database.QueryRow(
		`SELECT id, display_name, public_ip, ssh_username, ssh_password, ssh_private_key, status FROM vps WHERE id = ?`,
		vpsID,
	).Scan(&id, &displayName, &publicIP, &sshUser, &sshPass, &sshPrivKey, &status)
	if err != nil {
		fmt.Fprintf(os.Stderr, "query vps %d: %v\n", vpsID, err)
		os.Exit(1)
	}

	fmt.Printf("VPS %d | %s | %s | %s\n", id, displayName.String, publicIP.String, status.String)
	if sshUser.Valid {
		fmt.Printf("  SSH User:     %s\n", sshUser.String)
	}
	if sshPass.Valid {
		fmt.Printf("  SSH Password: %s\n", sshPass.String)
	}
	if sshPrivKey.Valid {
		opensshKey, err := convertPKCS8ToOpenSSH(sshPrivKey.String)
		if err != nil {
			fmt.Fprintf(os.Stderr, "convert key: %v (falling back to raw PKCS#8)\n", err)
			fmt.Printf("\n%s", sshPrivKey.String)
		} else {
			fmt.Print(opensshKey)
		}
	} else {
		fmt.Println("  SSH Private Key: (not set)")
	}
}

func resolveEncryptionKey() string {
	if key := os.Getenv("DB_ENCRYPTION_KEY"); key != "" {
		return key
	}
	if data, err := os.ReadFile(".env"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if after, ok := strings.CutPrefix(line, "DB_ENCRYPTION_KEY="); ok {
				return after
			}
		}
	}
	fmt.Fprintf(os.Stderr, "DB_ENCRYPTION_KEY not set in env or .env\n")
	os.Exit(1)
	return ""
}

func convertPKCS8ToOpenSSH(pkcs8PEM string) (string, error) {
	block, _ := pem.Decode([]byte(pkcs8PEM))
	if block == nil || block.Type != "PRIVATE KEY" {
		return "", fmt.Errorf("not a valid PKCS#8 private key PEM block")
	}
	priv, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse PKCS#8: %w", err)
	}
	opensshBlock, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return "", fmt.Errorf("marshal OpenSSH: %w", err)
	}
	return string(pem.EncodeToMemory(opensshBlock)), nil
}
