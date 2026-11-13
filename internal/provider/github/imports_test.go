package github

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/testenv"
)

// adapterImportPath is the import path of this package.
const adapterImportPath = "github.com/cultofthecode/agent-id/internal/provider/github"

// TestOnlyTheCompositionRootImportsTheAdapter proves the ADR #35 import rule
// beside depguard: no Go file outside internal/provider/github imports the
// adapter, except cmd/agent-id/main.go.
func TestOnlyTheCompositionRootImportsTheAdapter(t *testing.T) {
	testenv.Isolate(t)
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root not found at %s: %v", root, err)
	}
	allowed := map[string]bool{filepath.Join("cmd", "agent-id", "main.go"): true}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && path != root || rel == filepath.Join("internal", "provider", "github") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || allowed[rel] {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			if p, _ := strconv.Unquote(spec.Path.Value); p == adapterImportPath || strings.HasPrefix(p, adapterImportPath+"/") {
				t.Errorf("%s imports %s; only cmd/agent-id/main.go may (ADR #35)", rel, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
