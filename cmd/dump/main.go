package main

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
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
	dumpAll(database)
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

func dumpAll(d *sql.DB) {
	tables := []string{"users", "vps"}
	for _, t := range tables {
		rows, _ := d.Query("SELECT * FROM " + t)
		cols, _ := rows.Columns()
		fmt.Printf("\n=== %s ===\n", t)
		for _, c := range cols { fmt.Printf("%s | ", c) }
		fmt.Println()
		for rows.Next() {
			vals := make([]interface{}, len(cols))
			for i := range vals { var s string; vals[i] = &s }
			rows.Scan(vals...)
			for _, v := range vals { fmt.Printf("%s | ", *(v.(*string))) }
			fmt.Println()
		}
	}
}
