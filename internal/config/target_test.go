package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadTargetConfigReadsAutotestYAML(t *testing.T) {
	dir := t.TempDir()
	content := `
base_url: http://127.0.0.1:3000/
commands:
  start: npm run dev
  seed: npm run test:seed
  reset: npm run test:reset
auth:
  login_url: /login
  username: demo@example.com
  password_env: AUTOTEST_DEMO_PASSWORD
safety:
  blocked_endpoints:
    - " DELETE /api/users/* "
    - ""
assertions:
  rules:
    - " created notes must be visible "
`
	if err := os.WriteFile(filepath.Join(dir, ".autotest.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, path, found, err := LoadTargetConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected target config to be found")
	}
	if filepath.Base(path) != ".autotest.yaml" {
		t.Fatalf("unexpected config path: %s", path)
	}
	if cfg.BaseURL != "http://127.0.0.1:3000" {
		t.Fatalf("expected normalized base url, got %q", cfg.BaseURL)
	}
	if cfg.Commands.Start != "npm run dev" || cfg.Commands.Seed != "npm run test:seed" || cfg.Commands.Reset != "npm run test:reset" {
		t.Fatalf("unexpected commands: %+v", cfg.Commands)
	}
	if cfg.Auth.LoginURL != "/login" || cfg.Auth.Username != "demo@example.com" || cfg.Auth.PasswordEnv != "AUTOTEST_DEMO_PASSWORD" {
		t.Fatalf("unexpected auth: %+v", cfg.Auth)
	}
	if len(cfg.Safety.BlockedEndpoints) != 1 || cfg.Safety.BlockedEndpoints[0] != "DELETE /api/users/*" {
		t.Fatalf("unexpected blocked endpoints: %+v", cfg.Safety.BlockedEndpoints)
	}
	if len(cfg.Assertions.Rules) != 1 || cfg.Assertions.Rules[0] != "created notes must be visible" {
		t.Fatalf("unexpected assertion rules: %+v", cfg.Assertions.Rules)
	}
}

func TestLoadTargetConfigMissingFile(t *testing.T) {
	cfg, path, found, err := LoadTargetConfig(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if found || path != "" || cfg.BaseURL != "" {
		t.Fatalf("expected no config, got found=%v path=%q cfg=%+v", found, path, cfg)
	}
}
