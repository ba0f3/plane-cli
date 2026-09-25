package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	EnvBaseURL   = "PLANE_BASE_URL"
	EnvToken     = "PLANE_TOKEN"
	EnvAPIKey    = "PLANE_API_KEY"
	EnvWorkspace = "PLANE_WORKSPACE"
	EnvConfig    = "PLANE_CONFIG"
)

type Config struct {
	BaseURL   string `json:"base_url"`
	Token     string `json:"token"`
	Workspace string `json:"workspace,omitempty"`
}

type Overrides struct {
	BaseURL   string
	Token     string
	Workspace string
	Config    string
}

func DefaultPath() (string, error) {
	if v := strings.TrimSpace(os.Getenv(EnvConfig)); v != "" {
		return expandHome(v)
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "plane-cli", "config.json"), nil
}

func Load(over Overrides) (Config, string, error) {
	path := strings.TrimSpace(over.Config)
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return Config{}, "", err
		}
	} else {
		var err error
		path, err = expandHome(path)
		if err != nil {
			return Config{}, "", err
		}
	}

	cfg := Config{}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &cfg); err != nil {
			return Config{}, path, fmt.Errorf("parse config %s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Config{}, path, fmt.Errorf("read config %s: %w", path, err)
	}

	if v := strings.TrimSpace(os.Getenv(EnvBaseURL)); v != "" {
		cfg.BaseURL = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvAPIKey)); v != "" {
		cfg.Token = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvToken)); v != "" {
		cfg.Token = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvWorkspace)); v != "" {
		cfg.Workspace = v
	}

	if strings.TrimSpace(over.BaseURL) != "" {
		cfg.BaseURL = over.BaseURL
	}
	if strings.TrimSpace(over.Token) != "" {
		cfg.Token = over.Token
	}
	if strings.TrimSpace(over.Workspace) != "" {
		cfg.Workspace = over.Workspace
	}

	cfg.BaseURL = normalizeBaseURL(cfg.BaseURL)
	cfg.Token = strings.TrimSpace(cfg.Token)
	cfg.Workspace = strings.TrimSpace(cfg.Workspace)
	return cfg, path, nil
}

func Save(path string, cfg Config) error {
	var err error
	path, err = expandHome(path)
	if err != nil {
		return err
	}
	if path == "" {
		path, err = DefaultPath()
		if err != nil {
			return err
		}
	}
	cfg.BaseURL = normalizeBaseURL(cfg.BaseURL)
	cfg.Token = strings.TrimSpace(cfg.Token)
	cfg.Workspace = strings.TrimSpace(cfg.Workspace)
	if cfg.BaseURL == "" {
		return errors.New("base URL is required")
	}
	if cfg.Token == "" {
		return errors.New("token is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return os.Chmod(path, 0o600)
}

func (c Config) Validate() error {
	if c.BaseURL == "" {
		return fmt.Errorf("missing Plane base URL; run `plane configure` or set %s", EnvBaseURL)
	}
	if c.Token == "" {
		return fmt.Errorf("missing Plane token; run `plane configure` or set %s/%s", EnvToken, EnvAPIKey)
	}
	return nil
}

func normalizeBaseURL(v string) string {
	v = strings.TrimSpace(v)
	return strings.TrimRight(v, "/")
}

func expandHome(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" || path[0] != '~' {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if path == "~" {
		return home, nil
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}
