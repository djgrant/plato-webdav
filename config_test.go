package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "Settings.json")
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadConfigDefaults(t *testing.T) {
	p := writeConfig(t, `{"server-url": "https://dav.example.com/"}`)
	cfg, err := loadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if !*cfg.Recursive || !*cfg.DeleteRemoved {
		t.Error("recursive and delete-removed should default to true")
	}
	if cfg.TimeoutSeconds != 60 {
		t.Errorf("timeout default = %d, want 60", cfg.TimeoutSeconds)
	}
	if !cfg.kindAllowed("epub") || cfg.kindAllowed("docx") {
		t.Error("default allowed-kinds wrong")
	}
}

func TestLoadConfigOverrides(t *testing.T) {
	p := writeConfig(t, `{
		"server-url": "https://dav.example.com/",
		"recursive": false,
		"delete-removed": false,
		"allowed-kinds": [".EPUB", "Pdf"],
		"timeout-seconds": 5
	}`)
	cfg, err := loadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if *cfg.Recursive || *cfg.DeleteRemoved {
		t.Error("explicit false overrides ignored")
	}
	if !cfg.kindAllowed("epub") || !cfg.kindAllowed("pdf") || cfg.kindAllowed("cbz") {
		t.Error("allowed-kinds not normalized")
	}
}

func TestLoadConfigMissingURL(t *testing.T) {
	p := writeConfig(t, `{}`)
	if _, err := loadConfig(p); err == nil {
		t.Fatal("expected error for missing server-url")
	}
}
