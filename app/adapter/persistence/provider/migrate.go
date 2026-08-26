package provider

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Run applies every .sql file in the migrations directory that has not been
// applied yet, in filename order. Idempotent: each file runs at most once,
// tracked in schema_migrations. Safe to call on every start.
func Run(db *sql.DB) error {
	return run(db, "migrations")
}

// RunFrom applies migrations from an explicit directory (used by tests whose
// working directory is not the repo root).
func RunFrom(db *sql.DB, dir string) error {
	return run(db, dir)
}

func run(db *sql.DB, migrationsDir string) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		name VARCHAR(255) NOT NULL PRIMARY KEY,
		applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("read %s dir: %w", migrationsDir, err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, name := range files {
		var applied int
		if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE name = ?", name).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if applied > 0 {
			continue
		}
		body, err := os.ReadFile(filepath.Join(migrationsDir, name))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if err := execMigration(db, name, string(body)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}

		if _, err := db.Exec("INSERT INTO schema_migrations (name) VALUES (?)", name); err != nil {
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		fmt.Printf("migrated %s\n", name)
	}
	return nil
}

// execMigration runs a multi-statement .sql file: database/sql does not parse
// multiple statements in one Exec, so the file is stripped of '--' comment
// lines and split into semicolon-terminated statements.
func execMigration(db *sql.DB, name, body string) error {
	for _, stmt := range splitStatements(body) {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
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
