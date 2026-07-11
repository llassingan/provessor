package db

import (
	"database/sql"

	"github.com/llassingan/provessor/internal/logger"
)

func DumpVPS(db *sql.DB, dev bool, log *logger.Logger) {
	if !dev {
		return
	}
	rows, err := db.Query(`SELECT id, display_name, public_ip, ssh_username, ssh_password, initial_credentials, status FROM vps ORDER BY id DESC`)
	if err != nil {
		log.Error("dump_vps_query_failed", "error", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var name, publicIP, sshUser, sshPass, creds, status sql.NullString
		rows.Scan(&id, &name, &publicIP, &sshUser, &sshPass, &creds, &status)
		log.Info("dump_vps_row",
			"id", id,
			"display_name", name.String,
			"public_ip", publicIP.String,
			"status", status.String,
			"has_ssh_user", sshUser.Valid,
			"has_ssh_password", sshPass.Valid,
			"has_credentials", creds.Valid,
		)
	}
}
