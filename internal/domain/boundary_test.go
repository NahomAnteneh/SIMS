package domain_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGovmomiImportBoundary ensures govmomi types do NOT leak into domain, persistence, or DB packages.
func TestGovmomiImportBoundary(t *testing.T) {
	checkedPackages := []string{
		"../../internal/domain",
		"../../internal/persistence",
		"../../generated/db",
	}

	for _, pkgPath := range checkedPackages {
		if _, err := os.Stat(pkgPath); os.IsNotExist(err) {
			continue
		}

		fset := token.NewFileSet()
		pkgs, err := parser.ParseDir(fset, pkgPath, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("failed to parse package %s: %v", pkgPath, err)
		}

		for _, pkg := range pkgs {
			for fileName, file := range pkg.Files {
				// Skip test files if they don't leak into production binaries, but ideally even tests shouldn't leak
				if strings.HasSuffix(fileName, "_test.go") {
					continue
				}
				for _, imp := range file.Imports {
					importPath := strings.Trim(imp.Path.Value, `"`)
					if strings.HasPrefix(importPath, "github.com/vmware/govmomi") {
						t.Errorf("FORBIDDEN IMPORT LEAK in %s: %s imports %s", filepath.Base(pkgPath), fileName, importPath)
					}
				}
			}
		}
	}
}
