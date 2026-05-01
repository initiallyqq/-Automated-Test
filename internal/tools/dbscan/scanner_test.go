package dbscan

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestScannerDetectsSQLAndPrismaModels(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "migrations", "001_init.sql"), `CREATE TABLE users (
  id text primary key,
  email text not null,
  created_at text,
  constraint users_email_unique unique(email)
);`)
	writeFile(t, filepath.Join(dir, "prisma", "schema.prisma"), `model Post {
  id String @id
  title String
  @@index([title])
}`)

	result, err := NewScanner().Scan(context.Background(), ScanRequest{RepoPath: dir})
	if err != nil {
		t.Fatal(err)
	}

	users := findModel(result.Models, "users")
	if users == nil {
		t.Fatalf("expected users model in %+v", result.Models)
	}
	assertField(t, users.Fields, "email")
	assertField(t, users.Fields, "id")

	post := findModel(result.Models, "Post")
	if post == nil {
		t.Fatalf("expected Post model in %+v", result.Models)
	}
	assertField(t, post.Fields, "title")
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

func findModel(models []Model, name string) *Model {
	for i := range models {
		if models[i].Name == name {
			return &models[i]
		}
	}
	return nil
}

func assertField(t *testing.T, fields []string, want string) {
	t.Helper()
	for _, field := range fields {
		if field == want {
			return
		}
	}
	t.Fatalf("expected field %q in %v", want, fields)
}
