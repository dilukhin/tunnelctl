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

func TestNormalizeProfilePort(t *testing.T) {
	cfg := Config{
		Profiles: []Profile{{Name: "p1", User: "u", Host: "h"}},
	}

	cfg.Normalize()

	if cfg.Profiles[0].Port != 22 {
		t.Fatalf("ожидался порт 22, получено %d", cfg.Profiles[0].Port)
	}
	if cfg.Defaults.Listen != "127.0.0.1:1080" {
		t.Fatalf("ожидался listen по умолчанию, получено %q", cfg.Defaults.Listen)
	}
}

func TestResolveProfileByNameAndAlias(t *testing.T) {
	cfg := Config{Profiles: []Profile{{Name: "profile-main", Alias: "main", User: "u", Host: "h", Port: 22}}}

	if _, ok := cfg.ResolveProfile("profile-main"); !ok {
		t.Fatal("профиль не найден по имени")
	}
	if _, ok := cfg.ResolveProfile("main"); !ok {
		t.Fatal("профиль не найден по алиасу")
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
	if p.EffectiveListen(cfg) != "127.0.0.1:2080" {
		t.Fatal("listen профиля должен перекрывать значение по умолчанию")
	}
	if p.EffectiveHealthURL(cfg) != "https://example.com/health" {
		t.Fatal("health_url профиля должен перекрывать значение по умолчанию")
	}
}
