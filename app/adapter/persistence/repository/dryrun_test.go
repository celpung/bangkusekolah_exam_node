package repository

import (
	"strings"
	"testing"

	"gorm.io/gorm"
	gorm_tests "gorm.io/gorm/utils/tests"
)

type statement struct {
	SQL  string
	Vars []any
}

func newDryRunDB(t *testing.T) (*gorm.DB, *[]statement) {
	t.Helper()
	db, err := gorm.Open(gorm_tests.DummyDialector{}, &gorm.Config{DryRun: true})
	if err != nil {
		t.Fatalf("open dry-run db: %v", err)
	}
	var recorded []statement
	capture := func(tx *gorm.DB) {
		recorded = append(recorded, statement{
			SQL:  tx.Statement.SQL.String(),
			Vars: append([]any{}, tx.Statement.Vars...),
		})
	}
	callbacks := map[string]func(string, func(*gorm.DB)) error{
		"query":  db.Callback().Query().After("gorm:query").Register,
		"create": db.Callback().Create().After("gorm:create").Register,
		"update": db.Callback().Update().After("gorm:update").Register,
		"delete": db.Callback().Delete().After("gorm:delete").Register,
		"raw":    db.Callback().Raw().After("gorm:raw").Register,
	}
	for name, register := range callbacks {
		if err := register("test:capture_"+name, capture); err != nil {
			t.Fatalf("register %s callback: %v", name, err)
		}
	}
	return db, &recorded
}

func requireSQLContaining(t *testing.T, recorded *[]statement, fragments ...string) statement {
	t.Helper()
	var matches []statement
	for _, stmt := range *recorded {
		if containsAll(stmt.SQL, fragments) {
			matches = append(matches, stmt)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0]
	case 0:
		t.Fatalf("no statement contained %v; recorded: %s", fragments, formatSQL(*recorded))
	default:
		t.Fatalf("expected one statement containing %v, got %d; recorded: %s", fragments, len(matches), formatSQL(*recorded))
	}
	return statement{}
}

func requireFirstSQLContaining(t *testing.T, recorded *[]statement, fragments ...string) statement {
	t.Helper()
	for _, stmt := range *recorded {
		if containsAll(stmt.SQL, fragments) {
			return stmt
		}
	}
	t.Fatalf("no statement contained %v; recorded: %s", fragments, formatSQL(*recorded))
	return statement{}
}

func requireNoSQLContaining(t *testing.T, recorded *[]statement, fragment string) {
	t.Helper()
	for _, stmt := range *recorded {
		if strings.Contains(stmt.SQL, fragment) {
			t.Fatalf("expected no statement containing %q, got: %s", fragment, stmt.SQL)
		}
	}
}

func containsAll(sql string, fragments []string) bool {
	for _, fragment := range fragments {
		if !strings.Contains(sql, fragment) {
			return false
		}
	}
	return true
}

func formatSQL(recorded []statement) string {
	if len(recorded) == 0 {
		return "(none)"
	}
	lines := make([]string, len(recorded))
	for i, stmt := range recorded {
		lines[i] = stmt.SQL
	}
	return "\n  " + strings.Join(lines, "\n  ")
}
