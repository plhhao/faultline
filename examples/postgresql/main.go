// Command postgresql demonstrates retry behavior after a lost commit acknowledgment.
package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	directDSN, proxyDSN := os.Getenv("FAULTLINE_PG_DIRECT"), os.Getenv("FAULTLINE_PG_PROXY")
	if directDSN == "" || proxyDSN == "" {
		return fmt.Errorf("set FAULTLINE_PG_DIRECT and FAULTLINE_PG_PROXY")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	direct, err := pgx.Connect(ctx, directDSN)
	if err != nil {
		return fmt.Errorf("cannot connect to direct database")
	}
	defer direct.Close(context.Background())
	if _, err := direct.Exec(ctx, "CREATE TABLE IF NOT EXISTS faultline_retry_plain (operation_id text); CREATE TABLE IF NOT EXISTS faultline_retry_unique (operation_id text PRIMARY KEY)"); err != nil {
		return fmt.Errorf("cannot create fixture tables")
	}
	for _, deduplicate := range []bool{false, true} {
		table := "faultline_retry_plain"
		suffix := ""
		if deduplicate {
			table = "faultline_retry_unique"
			suffix = " ON CONFLICT DO NOTHING"
		}
		operation := rand.Text()
		for attempt := 1; attempt <= 2; attempt++ {
			c, err := pgx.Connect(ctx, proxyDSN)
			if err != nil {
				return fmt.Errorf("cannot connect to proxy")
			}
			tx, err := c.Begin(ctx)
			if err != nil {
				c.Close(ctx)
				return fmt.Errorf("cannot begin transaction")
			}
			if _, err := tx.Exec(ctx, "INSERT INTO "+table+" (operation_id) VALUES ($1)"+suffix, operation); err != nil {
				c.Close(ctx)
				return fmt.Errorf("fixture insert failed")
			}
			err = tx.Commit(ctx)
			c.Close(context.Background())
			fmt.Printf("deduplicate=%t attempt=%d client_commit_success=%t\n", deduplicate, attempt, err == nil)
		}
		var count int
		if err := direct.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE operation_id=$1", operation).Scan(&count); err != nil {
			return fmt.Errorf("independent verification failed")
		}
		fmt.Printf("deduplicate=%t independently_verified_rows=%d\n", deduplicate, count)
		if _, err := direct.Exec(ctx, "DELETE FROM "+table+" WHERE operation_id=$1", operation); err != nil {
			return fmt.Errorf("fixture cleanup failed")
		}
		expected := 2
		if deduplicate {
			expected = 1
		}
		if count != expected {
			return fmt.Errorf("unexpected row count: want %d got %d", expected, count)
		}
	}
	return nil
}
