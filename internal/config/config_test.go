package config

import "testing"

func TestDefaultConfigHasSafeDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Defaults.Listen != "127.0.0.1:1080" {
		t.Fatalf("ожидался безопасный listen по умолчанию, получено %q", cfg.Defaults.Listen)
	}
	if cfg.Defaults.HealthURL == "" {
		t.Fatal("health_url по умолчанию не должен быть пустым")
	}
	if !cfg.Defaults.Reconnect.Enabled {
		t.Fatal("reconnect должен быть включён по умолчанию")
	}
}

func TestResolveProfileAndGroup(t *testing.T) {
	cfg := Config{
		Profiles: []Profile{{Name: "profile-main", Alias: "main", User: "u", Host: "h", Port: 22}},
		Groups:   []Group{{Name: "auto-group", Alias: "auto", Strategy: "failover", Profiles: []string{"profile-main"}}},
	}
	if _, ok := cfg.ResolveProfile("main"); !ok {
		t.Fatal("профиль не найден по алиасу")
	}
	if _, ok := cfg.ResolveGroup("auto"); !ok {
		t.Fatal("группа не найдена по алиасу")
	}
	if _, ok := cfg.ResolveTarget("missing"); ok {
		t.Fatal("неизвестная цель не должна разрешаться")
	}
}

func TestValidateRejectsUnknownGroupProfile(t *testing.T) {
	cfg := Config{
		Profiles: []Profile{{Name: "first", User: "u", Host: "h", Port: 22}},
		Groups:   []Group{{Name: "auto", Profiles: []string{"missing"}}},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("ожидалась ошибка неизвестного профиля группы")
	}
}

func TestValidateRejectsDuplicateAlias(t *testing.T) {
	cfg := Config{Profiles: []Profile{
		{Name: "first", Alias: "same", User: "u", Host: "h", Port: 22},
		{Name: "second", Alias: "same", User: "u", Host: "h2", Port: 22},
	}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("ожидалась ошибка дубликата алиаса")
	}
}

func TestNormalizeProfilePort(t *testing.T) {
	cfg := Config{Profiles: []Profile{{Name: "p1", User: "u", Host: "h"}}}
	cfg.Normalize()
	if cfg.Profiles[0].Port != 22 {
		t.Fatalf("ожидался порт 22, получено %d", cfg.Profiles[0].Port)
	}
}

func TestValidateRejectsDuplicateProfiles(t *testing.T) {
	cfg := Config{Profiles: []Profile{
		{Name: "same", User: "u1", Host: "h1", Port: 22},
		{Name: "same", User: "u2", Host: "h2", Port: 22},
	}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("ожидалась ошибка дубликата профиля")
	}
}

func TestEffectiveListenAndHealthURL(t *testing.T) {
	cfg := DefaultConfig()
	p := Profile{Name: "p", User: "u", Host: "h", Port: 22}
	if p.EffectiveListen(cfg) != cfg.Defaults.Listen {
		t.Fatal("без listen профиль должен использовать значение по умолчанию")
	}
	if p.EffectiveHealthURL(cfg) != cfg.Defaults.HealthURL {
		t.Fatal("без health_url профиль должен использовать значение по умолчанию")
	}
	p.Listen = "127.0.0.1:2080"
	p.Health = "https://example.com/health"
	if p.EffectiveListen(cfg) != "127.0.0.1:2080" || p.EffectiveHealthURL(cfg) != "https://example.com/health" {
		t.Fatal("значения профиля должны перекрывать значения по умолчанию")
	}
}
