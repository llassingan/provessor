package main

import (
	"database/sql"
	"fmt"
	"os"
	"vps-store/internal/db"
)

func main() {
	key := "b647bf795dddbcd6a38e529c416f1d0d064874f3a949a4f86ed4e1f3e07a08f4"
	database, err := db.Open("data/provessor.db", key)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()
	dumpAll(database)
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
