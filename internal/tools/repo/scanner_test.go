package repo

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestScannerDetectsFullstackProject(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example\n\ngo 1.22\n")
	writeFile(t, filepath.Join(dir, "package.json"), `{
  "dependencies": {
    "react": "^18.0.0",
    "@playwright/test": "^1.45.0",
    "prisma": "^5.0.0"
  }
}`)
	writeFile(t, filepath.Join(dir, "src", "api", "handler.go"), "package api\n")
	writeFile(t, filepath.Join(dir, "src", "pages", "home.tsx"), "export default function Home() { return null }\n")
	writeFile(t, filepath.Join(dir, "schema.sql"), "create table users(id text primary key);\n")

	result, err := NewScanner().Scan(context.Background(), ScanRequest{RepoPath: dir})
	if err != nil {
		t.Fatal(err)
	}

	if !result.Frontend {
		t.Fatal("expected frontend=true")
	}
	if !result.Backend {
		t.Fatal("expected backend=true")
	}
	if !result.Database {
		t.Fatal("expected database=true")
	}
	assertContains(t, result.Languages, "go")
	assertContains(t, result.Languages, "typescript")
	assertContains(t, result.Frameworks, "react")
	assertContains(t, result.Frameworks, "playwright")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertContains(t *testing.T, values []string, want string) {
	t.Helper()
	for _, value := range values {
		if value == want {
			return
		}
	}
	t.Fatalf("expected %q in %v", want, values)
}
