//go:build integration

package integration_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func getDBHost() string {
	if host := os.Getenv("POSTGRES_HOST"); host != "" {
		return host
	}
	return "localhost"
}

func getDBPort() string {
	if port := os.Getenv("POSTGRES_PORT"); port != "" {
		return port
	}
	return "5433"
}

func getConnString(user, password string) string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/sims?sslmode=disable", user, password, getDBHost(), getDBPort())
}

func TestMigrations(t *testing.T) {
	connStr := getConnString("postgres", "postgres")
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		t.Fatalf("failed to connect as admin: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("database ping failed: %v", err)
	}

	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("failed to set goose dialect: %v", err)
	}

	migrationsDir := "../../db/migrations"

	// 1. Run migrations up
	t.Log("Applying migrations...")
	if err := goose.Up(db, migrationsDir); err != nil {
		t.Fatalf("failed to run goose Up: %v", err)
	}

	// 2. Verify extensions enabled
	rows, err := db.QueryContext(ctx, "SELECT extname FROM pg_extension WHERE extname NOT IN ('plpgsql') ORDER BY extname")
	if err != nil {
		t.Fatalf("failed to query pg_extension: %v", err)
	}
	defer rows.Close()

	var extensions []string
	for rows.Next() {
		var ext string
		if err := rows.Scan(&ext); err != nil {
			t.Fatalf("failed to scan extension name: %v", err)
		}
		extensions = append(extensions, ext)
	}

	expected := []string{"ltree", "pg_trgm"}
	if len(extensions) != len(expected) {
		t.Fatalf("expected exactly extensions %v, got %v", expected, extensions)
	}
	for i, ext := range extensions {
		if ext != expected[i] {
			t.Errorf("extension[%d]: expected %q, got %q", i, expected[i], ext)
		}
	}

	// 3. Verify idempotency: running goose Up again should succeed cleanly with no changes
	t.Log("Verifying migration idempotency...")
	if err := goose.Up(db, migrationsDir); err != nil {
		t.Fatalf("goose Up idempotency failed: %v", err)
	}

	// 4. Test rollback and re-apply
	t.Log("Verifying migration rollback and re-application...")
	if err := goose.Down(db, migrationsDir); err != nil {
		t.Fatalf("goose Down failed: %v", err)
	}
	if err := goose.Up(db, migrationsDir); err != nil {
		t.Fatalf("re-applying goose Up failed: %v", err)
	}
}

func TestRolePrivileges(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ownerConnStr := getConnString("migration_owner", "migration_owner_pass")
	ownerDB, err := sql.Open("pgx", ownerConnStr)
	if err != nil {
		t.Fatalf("failed to connect as migration_owner: %v", err)
	}
	defer ownerDB.Close()

	// migration_owner creates a throwaway table to test least-privilege boundary
	_, _ = ownerDB.ExecContext(ctx, "DROP TABLE IF EXISTS test_role_boundary")
	_, err = ownerDB.ExecContext(ctx, `
		CREATE TABLE test_role_boundary (
			id SERIAL PRIMARY KEY,
			resource_name TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		t.Fatalf("migration_owner failed to create table: %v", err)
	}
	defer func() {
		_, _ = ownerDB.ExecContext(context.Background(), "DROP TABLE IF EXISTS test_role_boundary")
	}()

	// 1. Verify collector_rw permissions
	t.Run("collector_rw permissions", func(t *testing.T) {
		rwConnStr := getConnString("collector_rw", "collector_rw_pass")
		rwDB, err := sql.Open("pgx", rwConnStr)
		if err != nil {
			t.Fatalf("failed to connect as collector_rw: %v", err)
		}
		defer rwDB.Close()

		// Allowed: INSERT
		var insertedID int
		err = rwDB.QueryRowContext(ctx, "INSERT INTO test_role_boundary (resource_name) VALUES ('vm-prod-01') RETURNING id").Scan(&insertedID)
		if err != nil {
			t.Errorf("collector_rw should be able to INSERT, got error: %v", err)
		}

		// Allowed: UPDATE
		_, err = rwDB.ExecContext(ctx, "UPDATE test_role_boundary SET resource_name = 'vm-prod-01-renamed' WHERE id = $1", insertedID)
		if err != nil {
			t.Errorf("collector_rw should be able to UPDATE, got error: %v", err)
		}

		// Allowed: SELECT
		var count int
		err = rwDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM test_role_boundary").Scan(&count)
		if err != nil || count != 1 {
			t.Errorf("collector_rw should be able to SELECT, got count: %d, err: %v", count, err)
		}

		// FORBIDDEN: CREATE TABLE (DDL)
		_, err = rwDB.ExecContext(ctx, "CREATE TABLE test_rw_forbidden (id int)")
		if err == nil {
			_, _ = rwDB.ExecContext(ctx, "DROP TABLE test_rw_forbidden")
			t.Errorf("collector_rw MUST NOT be able to CREATE TABLE")
		} else if !strings.Contains(err.Error(), "permission denied") {
			t.Logf("collector_rw CREATE correctly failed: %v", err)
		}

		// FORBIDDEN: ALTER TABLE (DDL)
		_, err = rwDB.ExecContext(ctx, "ALTER TABLE test_role_boundary ADD COLUMN forbidden_col text")
		if err == nil {
			t.Errorf("collector_rw MUST NOT be able to ALTER TABLE")
		} else if !strings.Contains(err.Error(), "must be owner") && !strings.Contains(err.Error(), "permission denied") {
			t.Logf("collector_rw ALTER correctly failed: %v", err)
		}

		// FORBIDDEN: DROP TABLE (DDL)
		_, err = rwDB.ExecContext(ctx, "DROP TABLE test_role_boundary")
		if err == nil {
			t.Errorf("collector_rw MUST NOT be able to DROP TABLE")
		} else if !strings.Contains(err.Error(), "must be owner") && !strings.Contains(err.Error(), "permission denied") {
			t.Logf("collector_rw DROP correctly failed: %v", err)
		}
	})

	// 2. Verify query_ro permissions
	t.Run("query_ro permissions", func(t *testing.T) {
		roConnStr := getConnString("query_ro", "query_ro_pass")
		roDB, err := sql.Open("pgx", roConnStr)
		if err != nil {
			t.Fatalf("failed to connect as query_ro: %v", err)
		}
		defer roDB.Close()

		// Allowed: SELECT
		var name string
		err = roDB.QueryRowContext(ctx, "SELECT resource_name FROM test_role_boundary LIMIT 1").Scan(&name)
		if err != nil {
			t.Errorf("query_ro should be able to SELECT, got error: %v", err)
		}

		// FORBIDDEN: INSERT
		_, err = roDB.ExecContext(ctx, "INSERT INTO test_role_boundary (resource_name) VALUES ('forbidden-insert')")
		if err == nil {
			t.Errorf("query_ro MUST NOT be able to INSERT")
		} else if !strings.Contains(err.Error(), "permission denied") {
			t.Logf("query_ro INSERT correctly failed with: %v", err)
		}

		// FORBIDDEN: UPDATE
		_, err = roDB.ExecContext(ctx, "UPDATE test_role_boundary SET resource_name = 'hacked'")
		if err == nil {
			t.Errorf("query_ro MUST NOT be able to UPDATE")
		} else if !strings.Contains(err.Error(), "permission denied") {
			t.Logf("query_ro UPDATE correctly failed with: %v", err)
		}

		// FORBIDDEN: DELETE
		_, err = roDB.ExecContext(ctx, "DELETE FROM test_role_boundary")
		if err == nil {
			t.Errorf("query_ro MUST NOT be able to DELETE")
		} else if !strings.Contains(err.Error(), "permission denied") {
			t.Logf("query_ro DELETE correctly failed with: %v", err)
		}

		// FORBIDDEN: DDL
		_, err = roDB.ExecContext(ctx, "DROP TABLE test_role_boundary")
		if err == nil {
			t.Errorf("query_ro MUST NOT be able to DROP TABLE")
		}
	})
}

func TestAllApplicationTablesRolePrivileges(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	allTables := []string{
		"vcenters",
		"collector_targets",
		"collector_leases",
		"collector_state",
		"datacenters",
		"inventory_folders",
		"clusters",
		"hosts",
		"datastores",
		"distributed_switches",
		"portgroups",
		"networks",
		"virtual_machines",
		"vm_datastores",
		"vm_network_interfaces",
		"inventory_events",
	}

	rwConnStr := getConnString("collector_rw", "collector_rw_pass")
	rwDB, err := sql.Open("pgx", rwConnStr)
	if err != nil {
		t.Fatalf("failed to connect as collector_rw: %v", err)
	}
	defer rwDB.Close()

	roConnStr := getConnString("query_ro", "query_ro_pass")
	roDB, err := sql.Open("pgx", roConnStr)
	if err != nil {
		t.Fatalf("failed to connect as query_ro: %v", err)
	}
	defer roDB.Close()

	for _, tbl := range allTables {
		tbl := tbl
		t.Run(tbl+" permissions", func(t *testing.T) {
			// collector_rw: SELECT allowed
			var rwCount int
			err := rwDB.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", tbl)).Scan(&rwCount)
			if err != nil {
				t.Errorf("collector_rw failed to SELECT from %s: %v", tbl, err)
			}

			// query_ro: SELECT allowed
			var roCount int
			err = roDB.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", tbl)).Scan(&roCount)
			if err != nil {
				t.Errorf("query_ro failed to SELECT from %s: %v", tbl, err)
			}

			// query_ro: DELETE forbidden
			_, err = roDB.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE 1=0", tbl))
			if err == nil {
				t.Errorf("query_ro MUST NOT be able to DELETE from %s", tbl)
			} else if !strings.Contains(err.Error(), "permission denied") {
				t.Errorf("expected permission denied for query_ro DELETE on %s, got: %v", tbl, err)
			}

			// query_ro: UPDATE forbidden
			var colName string
			err = roDB.QueryRowContext(ctx, "SELECT column_name FROM information_schema.columns WHERE table_name = $1 LIMIT 1", tbl).Scan(&colName)
			if err != nil {
				t.Fatalf("failed to get column name for %s: %v", tbl, err)
			}
			_, err = roDB.ExecContext(ctx, fmt.Sprintf("UPDATE %s SET %s = %s WHERE 1=0", tbl, colName, colName))
			if err == nil {
				t.Errorf("query_ro MUST NOT be able to UPDATE %s", tbl)
			} else if !strings.Contains(err.Error(), "permission denied") {
				t.Errorf("expected permission denied for query_ro UPDATE on %s, got: %v", tbl, err)
			}

			// query_ro: DROP TABLE forbidden (DDL)
			_, err = roDB.ExecContext(ctx, fmt.Sprintf("DROP TABLE %s", tbl))
			if err == nil {
				t.Errorf("query_ro MUST NOT be able to DROP TABLE %s", tbl)
			}

			// collector_rw: DROP TABLE forbidden (DDL)
			_, err = rwDB.ExecContext(ctx, fmt.Sprintf("DROP TABLE %s", tbl))
			if err == nil {
				t.Errorf("collector_rw MUST NOT be able to DROP TABLE %s", tbl)
			}
		})
	}
}
