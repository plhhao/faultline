// Command mysql demonstrates retry behavior after a lost commit acknowledgment.
package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"os"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	directDSN, proxyDSN := os.Getenv("FAULTLINE_MYSQL_DIRECT"), os.Getenv("FAULTLINE_MYSQL_PROXY")
	if directDSN == "" || proxyDSN == "" {
		return fmt.Errorf("set FAULTLINE_MYSQL_DIRECT and FAULTLINE_MYSQL_PROXY")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	direct, err := sql.Open("mysql", directDSN)
	if err != nil {
		return fmt.Errorf("invalid direct DSN")
	}
	defer direct.Close()
	proxy, err := sql.Open("mysql", proxyDSN)
	if err != nil {
		return fmt.Errorf("invalid proxy DSN")
	}
	defer proxy.Close()
	proxy.SetMaxOpenConns(1)
	for _, deduplicate := range []bool{false, true} {
		table, key, suffix := "faultline_retry_plain", "", ""
		if deduplicate {
			table = "faultline_retry_unique"
			key = " PRIMARY KEY"
			suffix = " ON DUPLICATE KEY UPDATE operation_id=operation_id"
		}
		if _, err = direct.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS "+table+" (operation_id VARCHAR(100)"+key+") ENGINE=InnoDB"); err != nil {
			return fmt.Errorf("cannot create fixture table")
		}
		operation := rand.Text()
		for attempt := 1; attempt <= 2; attempt++ {
			tx, err := proxy.BeginTx(ctx, nil)
			if err != nil {
				return fmt.Errorf("cannot begin transaction")
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO "+table+" (operation_id) VALUES (?)"+suffix, operation); err != nil {
				tx.Rollback()
				return fmt.Errorf("fixture insert failed")
			}
			err = tx.Commit()
			fmt.Printf("deduplicate=%t attempt=%d client_commit_success=%t\n", deduplicate, attempt, err == nil)
		}
		var count int
		if err = direct.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE operation_id=?", operation).Scan(&count); err != nil {
			return fmt.Errorf("independent verification failed")
		}
		fmt.Printf("deduplicate=%t independently_verified_rows=%d\n", deduplicate, count)
		if _, err = direct.ExecContext(ctx, "DELETE FROM "+table+" WHERE operation_id=?", operation); err != nil {
			return fmt.Errorf("fixture cleanup failed")
		}
		want := 2
		if deduplicate {
			want = 1
		}
		if count != want {
			return fmt.Errorf("unexpected rows: want %d got %d", want, count)
		}
	}
	return nil
}
