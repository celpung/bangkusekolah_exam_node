// Package migrations embeds the node's SQL migration files so production
// binaries never depend on the process working directory. The canonical copy
// lives in the repo-root migrations/ directory; this package re-embeds the
// same files for go:embed (which cannot reference parent directories).
// A test asserts both sets stay identical.
package migrations

import (
	"embed"
	"sort"
	"strings"
)

//go:embed all:*.sql
var FS embed.FS

// Names returns embedded migration file names in application order.
func Names() ([]string, error) {
	entries, err := FS.ReadDir(".")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// Read returns the contents of one embedded migration.
func Read(name string) (string, error) {
	b, err := FS.ReadFile(name)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
