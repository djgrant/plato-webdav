package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Config is read from Settings.json in the process's working directory
// (Plato sets the cwd to the hook binary's parent directory).
type Config struct {
	ServerURL          string   `json:"server-url"`
	Path               string   `json:"path"`
	Username           string   `json:"username"`
	Password           string   `json:"password"`
	InsecureSkipVerify bool     `json:"insecure-skip-verify"`
	Recursive          *bool    `json:"recursive"`
	AllowedKinds       []string `json:"allowed-kinds"`
	DeleteRemoved      *bool    `json:"delete-removed"`
	SanitizeHTML       bool     `json:"sanitize-html"`
	TimeoutSeconds     int      `json:"timeout-seconds"`
}

// Plato's default allowed-kinds for imports.
var defaultKinds = []string{"pdf", "djvu", "epub", "fb2", "txt", "xps", "oxps", "mobi", "cbz"}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := &Config{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if strings.TrimSpace(cfg.ServerURL) == "" {
		return nil, fmt.Errorf("%s: server-url is required", path)
	}
	if cfg.Recursive == nil {
		t := true
		cfg.Recursive = &t
	}
	if cfg.DeleteRemoved == nil {
		t := true
		cfg.DeleteRemoved = &t
	}
	if len(cfg.AllowedKinds) == 0 {
		cfg.AllowedKinds = defaultKinds
	}
	for i, k := range cfg.AllowedKinds {
		cfg.AllowedKinds[i] = strings.ToLower(strings.TrimPrefix(k, "."))
	}
	// Markdown is stored locally as converted HTML, so the local scan must
	// recognize html whenever md is synced.
	if cfg.kindAllowed("md") && !cfg.kindAllowed("html") {
		cfg.AllowedKinds = append(cfg.AllowedKinds, "html")
	}
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = 60
	}
	return cfg, nil
}

func (c *Config) kindAllowed(kind string) bool {
	for _, k := range c.AllowedKinds {
		if k == kind {
			return true
		}
	}
	return false
}
