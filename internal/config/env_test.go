package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvLocalLoadsMissingVariablesOnly(t *testing.T) {
	dir := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, ".env.local")
	content := "QWEN_API_KEY=file-key\nQWEN_BASE_URL=\"https://example.test/v1\"\nexport EXTRA_FLAG='enabled'\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("QWEN_API_KEY", "process-key")
	if err := LoadEnvLocal(); err != nil {
		t.Fatal(err)
	}

	if got := os.Getenv("QWEN_API_KEY"); got != "process-key" {
		t.Fatalf("expected process env to win, got %q", got)
	}
	if got := os.Getenv("QWEN_BASE_URL"); got != "https://example.test/v1" {
		t.Fatalf("expected env file value, got %q", got)
	}
	if got := os.Getenv("EXTRA_FLAG"); got != "enabled" {
		t.Fatalf("expected exported env file value, got %q", got)
	}
}
