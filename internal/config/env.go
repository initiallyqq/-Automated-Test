package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// LoadEnvLocal reads .env.local from the current working directory when present.
// Existing process environment variables win over file values.
func LoadEnvLocal() error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	return loadEnvFile(filepath.Join(wd, ".env.local"))
}

func loadEnvFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := parseEnvLine(line)
		if !ok || key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func parseEnvLine(line string) (key, value string, ok bool) {
	if strings.HasPrefix(line, "export ") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
	}
	index := strings.IndexByte(line, '=')
	if index <= 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:index])
	value = strings.TrimSpace(line[index+1:])
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
	}
	return key, value, true
}
