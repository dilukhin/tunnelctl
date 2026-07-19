package app

import (
	"testing"
	"time"

	"tunnelctl/internal/config"
)

func TestResolveGroupProfilesKeepsConfiguredOrder(t *testing.T) {
	cfg := config.Config{
		Profiles: []config.Profile{
			{Name: "first", User: "u1", Host: "h1", Port: 22},
			{Name: "second", User: "u2", Host: "h2", Port: 22},
		},
	}
	g := config.Group{Name: "auto", Profiles: []string{"second", "first"}}

	profiles, err := resolveGroupProfiles(cfg, g)
	if err != nil {
		t.Fatalf("resolveGroupProfiles вернул ошибку: %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("ожидалось 2 профиля, получено %d", len(profiles))
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

func TestReconnectBounds(t *testing.T) {
	cfg := config.Config{Defaults: config.Defaults{Reconnect: config.Reconnect{MinDelaySec: 5, MaxDelaySec: 2}}}
	minDelay, maxDelay := reconnectBounds(cfg)

	if minDelay != 5*time.Second {
		t.Fatalf("ожидался minDelay 5s, получено %s", minDelay)
	}
	if maxDelay != 5*time.Second {
		t.Fatalf("maxDelay должен быть не меньше minDelay, получено %s", maxDelay)
	}
}

func TestStableEnoughForDelayReset(t *testing.T) {
	cfg := config.Config{Defaults: config.Defaults{HealthIntervalSec: 10}}

	if stableEnoughForDelayReset(cfg, 19*time.Second) {
		t.Fatal("19 секунд при интервале 10 секунд не должны считаться стабильной работой")
	}
	if !stableEnoughForDelayReset(cfg, 20*time.Second) {
		t.Fatal("20 секунд при интервале 10 секунд должны считаться стабильной работой")
	}
}
