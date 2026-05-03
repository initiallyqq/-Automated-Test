package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type TargetConfig struct {
	BaseURL    string            `json:"baseUrl" yaml:"base_url"`
	Commands   TargetCommands    `json:"commands,omitempty" yaml:"commands"`
	Auth       TargetAuth        `json:"auth,omitempty" yaml:"auth"`
	Safety     TargetSafety      `json:"safety,omitempty" yaml:"safety"`
	Assertions TargetAssertions  `json:"assertions,omitempty" yaml:"assertions"`
	Metadata   map[string]string `json:"metadata,omitempty" yaml:"metadata"`
}

type TargetCommands struct {
	Start string `json:"start,omitempty" yaml:"start"`
	Seed  string `json:"seed,omitempty" yaml:"seed"`
	Reset string `json:"reset,omitempty" yaml:"reset"`
}

type TargetAuth struct {
	LoginURL    string `json:"loginUrl,omitempty" yaml:"login_url"`
	Username    string `json:"username,omitempty" yaml:"username"`
	UsernameEnv string `json:"usernameEnv,omitempty" yaml:"username_env"`
	Password    string `json:"password,omitempty" yaml:"password"`
	PasswordEnv string `json:"passwordEnv,omitempty" yaml:"password_env"`
}

type TargetSafety struct {
	BlockedEndpoints []string `json:"blockedEndpoints,omitempty" yaml:"blocked_endpoints"`
}

type TargetAssertions struct {
	Rules []string `json:"rules,omitempty" yaml:"rules"`
}

func LoadTargetConfig(repoPath string) (TargetConfig, string, bool, error) {
	path, found, err := FindTargetConfig(repoPath)
	if err != nil || !found {
		return TargetConfig{}, path, found, err
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		return TargetConfig{}, path, true, err
	}
	var cfg TargetConfig
	if err := yaml.Unmarshal(bytes, &cfg); err != nil {
		return TargetConfig{}, path, true, err
	}
	cfg.normalize()
	return cfg, path, true, nil
}

func FindTargetConfig(repoPath string) (string, bool, error) {
	root := strings.TrimSpace(repoPath)
	if root == "" {
		root = "."
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", false, err
	}
	for _, name := range []string{".autotest.yaml", ".autotest.yml"} {
		path := filepath.Join(abs, name)
		if _, err := os.Stat(path); err == nil {
			return path, true, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return path, false, err
		}
	}
	return "", false, nil
}

func (cfg *TargetConfig) normalize() {
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.Commands.Start = strings.TrimSpace(cfg.Commands.Start)
	cfg.Commands.Seed = strings.TrimSpace(cfg.Commands.Seed)
	cfg.Commands.Reset = strings.TrimSpace(cfg.Commands.Reset)
	cfg.Auth.LoginURL = strings.TrimSpace(cfg.Auth.LoginURL)
	cfg.Auth.Username = strings.TrimSpace(cfg.Auth.Username)
	cfg.Auth.UsernameEnv = strings.TrimSpace(cfg.Auth.UsernameEnv)
	cfg.Auth.Password = strings.TrimSpace(cfg.Auth.Password)
	cfg.Auth.PasswordEnv = strings.TrimSpace(cfg.Auth.PasswordEnv)
	cfg.Safety.BlockedEndpoints = nonEmptyTrimmed(cfg.Safety.BlockedEndpoints)
	cfg.Assertions.Rules = nonEmptyTrimmed(cfg.Assertions.Rules)
}

func nonEmptyTrimmed(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}
