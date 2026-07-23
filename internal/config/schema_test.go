package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyConfigLoadsAndSavesWithSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tunnelctl.json")
	legacy := `{"defaults":{"listen":"127.0.0.1:1080"},"profiles":[],"groups":[]}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("legacy-конфиг не загрузился: %v", err)
	}
	if cfg.Defaults.HealthURL == "" {
		t.Fatal("значения по умолчанию потеряны при миграции")
	}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"version": 1`) {
		t.Fatalf("версия схемы не записана: %s", data)
	}
}

func TestFutureConfigVersionIsRejected(t *testing.T) {
	cfg := DefaultConfig()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	future := strings.Replace(string(data), `"version":1`, `"version":2`, 1)
	var loaded Config
	if err := json.Unmarshal([]byte(future), &loaded); err == nil {
		t.Fatal("более новая схема должна быть отклонена")
	}
}

func TestCurrentConfigRoundTrip(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Profiles = []Profile{{Name: "one", User: "u", Host: "example.invalid", Port: 22}}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var loaded Config
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	if len(loaded.Profiles) != 1 || loaded.Profiles[0].Name != "one" {
		t.Fatalf("конфигурация изменилась после round-trip: %#v", loaded)
	}
}
