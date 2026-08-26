package migrations

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEmbeddedMatchesRepoRoot pins the duplicate-copy contract: the embedded
// migration set must stay byte-identical to the canonical repo-root
// migrations/ directory.
func TestEmbeddedMatchesRepoRoot(t *testing.T) {
	names, err := Names()
	if err != nil {
		t.Fatalf("embedded names: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("no embedded migrations")
	}
	repoDir := filepath.Join("..", "..", "migrations")
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		t.Skipf("repo migrations dir not reachable: %v", err)
	}
	var repoNames []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".sql" {
			repoNames = append(repoNames, e.Name())
		}
	}
	if len(names) != len(repoNames) {
		t.Fatalf("embedded has %d migrations, repo root has %d", len(names), len(repoNames))
	}
	for _, name := range names {
		want, err := os.ReadFile(filepath.Join(repoDir, name))
		if err != nil {
			t.Fatalf("read repo %s: %v", name, err)
		}
		got, err := Read(name)
		if err != nil {
			t.Fatalf("read embedded %s: %v", name, err)
		}
		if got != string(want) {
			t.Errorf("embedded %s differs from repo-root copy — re-sync app/migrations/", name)
		}
	}
}
