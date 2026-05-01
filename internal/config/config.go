package config

import (
	"os"
	"path/filepath"
)

type Config struct {
	LLM       LLMConfig
	Database  DatabaseConfig
	Runner    RunnerConfig
	Artifacts ArtifactConfig
	Patch     PatchConfig
}

type LLMConfig struct {
	Provider    string
	Model       string
	Temperature float64
	MaxTokens   int
}

type DatabaseConfig struct {
	Driver string
	DSN    string
}

type RunnerConfig struct {
	Type                string
	NodePath            string
	PlaywrightRunnerDir string
}

type ArtifactConfig struct {
	Root string
}

type PatchConfig struct {
	AutoApply       bool
	MaxTestFixRetry int
	AllowedPaths    []string
	BlockedPaths    []string
}

func Default() Config {
	return Config{
		LLM: LLMConfig{
			Provider:    "qwen",
			Model:       "qwen-plus",
			Temperature: 0.2,
			MaxTokens:   8192,
		},
		Database: DatabaseConfig{
			Driver: "sqlite",
			DSN:    ".autotest/autotest.db",
		},
		Runner: RunnerConfig{
			Type:                "local-node",
			NodePath:            "node",
			PlaywrightRunnerDir: "runner/playwright",
		},
		Artifacts: ArtifactConfig{
			Root: "artifacts",
		},
		Patch: PatchConfig{
			AutoApply:       true,
			MaxTestFixRetry: 2,
			AllowedPaths: []string{
				"e2e/specs/**",
				"e2e/pages/**",
				"e2e/actions/**",
				"e2e/fixtures/**",
				"e2e/mocks/**",
				"e2e/helpers/**",
			},
			BlockedPaths: []string{
				"src/core/**",
				"src/auth/**",
				"src/db/migrations/**",
			},
		},
	}
}

func WriteDefault(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(defaultYAML), 0o644)
}

const defaultYAML = `llm:
  provider: qwen
  model: qwen-plus
  temperature: 0.2
  max_tokens: 8192

database:
  driver: sqlite
  dsn: .autotest/autotest.db

runner:
  type: local-node
  node_path: node
  playwright_runner_dir: runner/playwright

artifacts:
  root: artifacts

patch:
  auto_apply: true
  max_test_fix_retry: 2
  allowed_paths:
    - e2e/specs/**
    - e2e/pages/**
    - e2e/actions/**
    - e2e/fixtures/**
    - e2e/mocks/**
    - e2e/helpers/**
  blocked_paths:
    - src/core/**
    - src/auth/**
    - src/db/migrations/**
`
