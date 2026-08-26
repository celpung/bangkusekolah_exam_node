package provider

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/celpung/bangkusekolah_exam_node/app/migrations"
)

// Run applies every embedded .sql migration that has not been applied yet, in
// filename order. Idempotent and concurrency-safe: a MySQL advisory lock
// serializes competing runners (examnode + bundleload can target the same DB),
// and the schema_migrations primary key backstops the lock.
func Run(db *sql.DB) error {
	return run(db, "")
}

// RunFrom applies migrations from a filesystem directory instead of the
// embedded set (used by tests that need custom fixtures).
func RunFrom(db *sql.DB, dir string) error {
	return run(db, dir)
}

func run(db *sql.DB, dir string) error {
	// Serialize concurrent runners. wait:false-style timeout keeps a stuck
	// holder from deadlocking startup; the duplicate-insert recovery below
	// handles the losing side gracefully.
	conn, err := db.Conn(context.Background())
	if err != nil {
		return fmt.Errorf("acquire connection for lock: %w", err)
	}
	defer conn.Close()
	lockName := "bangkusekolah_exam_node_migrations"
	var lockResult int
	if err := conn.QueryRowContext(context.Background(), "SELECT GET_LOCK(?, 10)", lockName).Scan(&lockResult); err != nil {
		return fmt.Errorf("GET_LOCK %s: %w", lockName, err)
	}
	if lockResult != 1 {
		return fmt.Errorf("could not acquire migration lock %s (another runner may be stuck)", lockName)
	}
	defer func() {
		_ = conn.QueryRowContext(context.Background(), "SELECT RELEASE_LOCK(?)", lockName).Scan(&lockResult)
	}()

	return runLocked(conn, dir)
}

func runLocked(conn *sql.Conn, dir string) error {
	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name VARCHAR(255) NOT NULL PRIMARY KEY,
		applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	names, bodies, err := loadMigrations(dir)
	if err != nil {
		return err
	}
	for _, name := range names {
		var applied int
		if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE name = ?", name).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if applied > 0 {
			continue
		}
		if err := execMigration(conn, name, bodies[name]); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := conn.ExecContext(ctx, "INSERT INTO schema_migrations (name) VALUES (?)", name); err != nil {
			// Losing a race despite the advisory lock (e.g. lock timeout):
			// re-check whether the competitor finished successfully.
			var done int
			if checkErr := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE name = ?", name).Scan(&done); checkErr == nil && done > 0 {
				continue
			}
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		fmt.Printf("migrated %s\n", name)
	}
	return nil
}

// loadMigrations reads either the embedded FS or a real directory and returns
// file names in application order with their contents.
func loadMigrations(dir string) ([]string, map[string]string, error) {
	bodies := map[string]string{}
	var names []string
	if dir == "" {
		embedded, err := migrations.Names()
		if err != nil {
			return nil, nil, fmt.Errorf("read embedded migrations: %w", err)
		}
		for _, name := range embedded {
			body, err := migrations.Read(name)
			if err != nil {
				return nil, nil, fmt.Errorf("read migration %s: %w", name, err)
			}
			names = append(names, name)
			bodies[name] = body
		}
	} else {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, nil, fmt.Errorf("read %s dir: %w", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
				continue
			}
			body, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				return nil, nil, fmt.Errorf("read migration %s: %w", e.Name(), err)
			}
			names = append(names, e.Name())
			bodies[e.Name()] = string(body)
		}
	}
	sort.Strings(names)
	return names, bodies, nil
}

// execMigration runs a multi-statement .sql body: database/sql does not parse
// multiple statements in one Exec, so the body is stripped of '--' comment
// lines and split into semicolon-terminated statements.
func execMigration(conn *sql.Conn, name, body string) error {
	ctx := context.Background()
	for _, stmt := range splitStatements(body) {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("statement failed (%s): %w", firstLine(stmt), err)
		}
	}
	return nil
}

func splitStatements(body string) []string {
	var clean strings.Builder
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		clean.WriteString(line)
		clean.WriteString("\n")
	}

	var (
		statements []string
		current    strings.Builder
		inString   bool
		quote      rune
	)
	for _, r := range clean.String() {
		switch {
		case inString:
			current.WriteRune(r)
			if r == quote {
				inString = false
			}
		case r == '\'' || r == '"':
			inString = true
			quote = r
			current.WriteRune(r)
		case r == ';':
			statements = append(statements, current.String())
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	if strings.TrimSpace(current.String()) != "" {
		statements = append(statements, current.String())
	}
	return statements
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
