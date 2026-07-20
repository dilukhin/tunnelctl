package app

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tunnelctl/internal/config"
)

func TestResolveGroupProfilesKeepsConfiguredOrder(t *testing.T) {
	cfg := config.Config{Profiles: []config.Profile{
		{Name: "first", User: "u1", Host: "h1", Port: 22},
		{Name: "second", User: "u2", Host: "h2", Port: 22},
	}}
	g := config.Group{Name: "auto", Profiles: []string{"second", "first"}}
	profiles, err := resolveGroupProfiles(cfg, g)
	if err != nil {
		t.Fatalf("resolveGroupProfiles вернул ошибку: %v", err)
	}
	if profiles[0].Name != "second" || profiles[1].Name != "first" {
		t.Fatalf("порядок профилей нарушен: %#v", profiles)
	}
}

func TestResolveGroupProfilesRejectsMissingProfile(t *testing.T) {
	cfg := config.Config{Profiles: []config.Profile{{Name: "first", User: "u", Host: "h", Port: 22}}}
	g := config.Group{Name: "auto", Profiles: []string{"missing"}}
	if _, err := resolveGroupProfiles(cfg, g); err == nil {
		t.Fatal("ожидалась ошибка неизвестного профиля")
	}
}

func TestStableEnoughForDelayReset(t *testing.T) {
	cfg := config.Config{Defaults: config.Defaults{HealthIntervalSec: 10}}
	if stableEnoughForDelayReset(cfg, 19*time.Second) {
		t.Fatal("19 секунд не должны считаться стабильной работой")
	}
	if !stableEnoughForDelayReset(cfg, 20*time.Second) {
		t.Fatal("20 секунд должны считаться стабильной работой")
	}
}

func TestReconnectBounds(t *testing.T) {
	cfg := config.Config{Defaults: config.Defaults{Reconnect: config.Reconnect{MinDelaySec: 5, MaxDelaySec: 2}}}
	minDelay, maxDelay := reconnectBounds(cfg)
	if minDelay != 5*time.Second || maxDelay != 5*time.Second {
		t.Fatalf("неверные границы reconnect: %s, %s", minDelay, maxDelay)
	}
}

func TestParseAutostartRequiresTarget(t *testing.T) {
	for _, args := range [][]string{{"install"}, {"print"}} {
		if _, err := parseAutostartArgs(args); err == nil {
			t.Fatalf("для %#v ожидалась ошибка обязательного имени", args)
		}
	}
}

func TestParseAutostartRejectsExtraArguments(t *testing.T) {
	if _, err := parseAutostartArgs([]string{"install", "auto", "extra"}); err == nil {
		t.Fatal("ожидалась ошибка лишнего аргумента")
	}
	if _, err := parseAutostartArgs([]string{"status", "auto"}); err == nil {
		t.Fatal("status не должен принимать имя")
	}
}

func TestParseAutostartFlags(t *testing.T) {
	got, err := parseAutostartArgs([]string{"install", "auto", "--config", "/tmp/cfg.json", "--dry-run", "--system", "--run-as", "svc"})
	if err != nil {
		t.Fatal(err)
	}
	if got.target != "auto" || got.configPath != "/tmp/cfg.json" || !got.dryRun || !got.system || got.runAs != "svc" {
		t.Fatalf("неверный разбор: %#v", got)
	}
}

func TestParseConnectRejectsExtraArguments(t *testing.T) {
	if _, err := parseConnectArgs([]string{"one", "two"}); err == nil {
		t.Fatal("ожидалась ошибка лишнего аргумента connect")
	}
}

func TestAutostartPrintAndDryRunDoNotWrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgPath := filepath.Join(t.TempDir(), "tunnelctl.json")
	cfg := config.DefaultConfig()
	cfg.Profiles = []config.Profile{{Name: "first", Alias: "first", User: "user", Host: "example.invalid", Port: 22, Key: "/secret/private-key"}}
	cfg.Groups = []config.Group{{Name: "auto", Alias: "auto", Strategy: "failover", Profiles: []string{"first"}}}
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	servicePath := filepath.Join(home, ".config", "systemd", "user", "tunnelctl.service")

	output := captureStdout(t, func() {
		if err := cmdAutostart([]string{"print", "auto", "--config", cfgPath}); err != nil {
			t.Fatal(err)
		}
		if err := cmdAutostart([]string{"install", "auto", "--config", cfgPath, "--dry-run"}); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := os.Stat(servicePath); !os.IsNotExist(err) {
		t.Fatalf("preview создал файл службы: %v", err)
	}
	if strings.Contains(output, "private-key") || strings.Contains(output, "example.invalid") || strings.Contains(output, "user@") {
		t.Fatalf("preview содержит чувствительные данные: %s", output)
	}
	if !strings.Contains(output, "Предварительный просмотр завершён") {
		t.Fatalf("нет сообщения dry-run: %s", output)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = old
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
