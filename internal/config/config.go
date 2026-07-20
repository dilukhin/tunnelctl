package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"tunnelctl/internal/paths"
)

// Config — основной файл настройки tunnelctl.
type Config struct {
	Defaults Defaults  `json:"defaults"`
	Profiles []Profile `json:"profiles"`
	Groups   []Group   `json:"groups"`
}

type Defaults struct {
	Listen              string    `json:"listen"`
	HealthURL           string    `json:"health_url"`
	HealthIntervalSec   int       `json:"health_interval_sec"`
	HealthTimeoutSec    int       `json:"health_timeout_sec"`
	ConnectTimeoutSec   int       `json:"connect_timeout_sec"`
	Reconnect           Reconnect `json:"reconnect"`
	PortConflict        string    `json:"port_conflict"`
	AllowListenAllHosts bool      `json:"allow_listen_all_hosts"`
}

type Reconnect struct {
	Enabled     bool `json:"enabled"`
	MinDelaySec int  `json:"min_delay_sec"`
	MaxDelaySec int  `json:"max_delay_sec"`
}

type Profile struct {
	Name   string `json:"name"`
	Alias  string `json:"alias,omitempty"`
	User   string `json:"user"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Key    string `json:"key,omitempty"`
	Listen string `json:"listen,omitempty"`
	Health string `json:"health_url,omitempty"`
	Source string `json:"imported_from,omitempty"`
}

type Group struct {
	Name     string   `json:"name"`
	Alias    string   `json:"alias,omitempty"`
	Strategy string   `json:"strategy"`
	Profiles []string `json:"profiles"`
}

func DefaultConfig() Config {
	return Config{
		Defaults: Defaults{
			Listen:            "127.0.0.1:1080",
			HealthURL:         "https://ifconfig.me/",
			HealthIntervalSec: 30,
			HealthTimeoutSec:  8,
			ConnectTimeoutSec: 10,
			PortConflict:      "ask",
			Reconnect:         Reconnect{Enabled: true, MinDelaySec: 2, MaxDelaySec: 60},
		},
		Profiles: []Profile{},
		Groups:   []Group{},
	}
}

func EffectivePath(path string) string {
	if path == "" {
		return paths.ConfigPath()
	}
	return path
}

func Load(path string) (Config, error) {
	path = EffectivePath(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	cfg := DefaultConfig()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	cfg.Normalize()
	return cfg, nil
}

func Save(path string, cfg Config) error {
	path = EffectivePath(path)
	cfg.Normalize()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func Exists(path string) bool {
	_, err := os.Stat(EffectivePath(path))
	return err == nil
}

func Ensure(path string) (Config, bool, error) {
	path = EffectivePath(path)
	if Exists(path) {
		cfg, err := Load(path)
		return cfg, false, err
	}
	cfg := DefaultConfig()
	if err := Save(path, cfg); err != nil {
		return Config{}, false, err
	}
	return cfg, true, nil
}

func (c *Config) Normalize() {
	if c.Defaults.Listen == "" {
		c.Defaults.Listen = "127.0.0.1:1080"
	}
	if c.Defaults.HealthURL == "" {
		c.Defaults.HealthURL = "https://ifconfig.me/"
	}
	if c.Defaults.HealthIntervalSec <= 0 {
		c.Defaults.HealthIntervalSec = 30
	}
	if c.Defaults.HealthTimeoutSec <= 0 {
		c.Defaults.HealthTimeoutSec = 8
	}
	if c.Defaults.ConnectTimeoutSec <= 0 {
		c.Defaults.ConnectTimeoutSec = 10
	}
	if c.Defaults.PortConflict == "" {
		c.Defaults.PortConflict = "ask"
	}
	if c.Defaults.Reconnect.MinDelaySec <= 0 {
		c.Defaults.Reconnect.MinDelaySec = 2
	}
	if c.Defaults.Reconnect.MaxDelaySec <= 0 {
		c.Defaults.Reconnect.MaxDelaySec = 60
	}
	for i := range c.Profiles {
		if c.Profiles[i].Port == 0 {
			c.Profiles[i].Port = 22
		}
		c.Profiles[i].Name = strings.TrimSpace(c.Profiles[i].Name)
		c.Profiles[i].Alias = strings.TrimSpace(c.Profiles[i].Alias)
		c.Profiles[i].User = strings.TrimSpace(c.Profiles[i].User)
		c.Profiles[i].Host = strings.TrimSpace(c.Profiles[i].Host)
	}
}

func (c Config) ResolveProfile(nameOrAlias string) (Profile, bool) {
	for _, p := range c.Profiles {
		if p.Name == nameOrAlias || p.Alias == nameOrAlias {
			return p, true
		}
	}
	return Profile{}, false
}

func (c Config) ResolveGroup(nameOrAlias string) (Group, bool) {
	for _, g := range c.Groups {
		if g.Name == nameOrAlias || g.Alias == nameOrAlias {
			return g, true
		}
	}
	return Group{}, false
}

func (c Config) ResolveTarget(nameOrAlias string) (string, bool) {
	if p, ok := c.ResolveProfile(nameOrAlias); ok {
		return p.Name, true
	}
	if g, ok := c.ResolveGroup(nameOrAlias); ok {
		return g.Name, true
	}
	return "", false
}

func (c Config) ProfileByName(name string) (Profile, bool) {
	for _, p := range c.Profiles {
		if p.Name == name {
			return p, true
		}
	}
	return Profile{}, false
}

func (c Config) Validate() error {
	seen := map[string]bool{}
	aliases := map[string]bool{}
	for _, p := range c.Profiles {
		if p.Name == "" {
			return errors.New("в конфиге есть профиль без name")
		}
		if seen[p.Name] {
			return fmt.Errorf("дубликат профиля: %s", p.Name)
		}
		seen[p.Name] = true
		if p.User == "" || p.Host == "" {
			return fmt.Errorf("профиль %s: user/host обязательны", p.Name)
		}
		if p.Alias != "" && p.Alias != p.Name {
			if aliases[p.Alias] || seen[p.Alias] {
				return fmt.Errorf("дубликат имени или алиаса: %s", p.Alias)
			}
			aliases[p.Alias] = true
		}
	}
	groupNames := map[string]bool{}
	for _, g := range c.Groups {
		if g.Name == "" {
			return errors.New("в конфиге есть группа без name")
		}
		if seen[g.Name] || aliases[g.Name] || groupNames[g.Name] {
			return fmt.Errorf("дубликат имени профиля или группы: %s", g.Name)
		}
		groupNames[g.Name] = true
		if g.Alias != "" && g.Alias != g.Name {
			if seen[g.Alias] || aliases[g.Alias] || groupNames[g.Alias] {
				return fmt.Errorf("дубликат имени или алиаса: %s", g.Alias)
			}
			aliases[g.Alias] = true
		}
		for _, pn := range g.Profiles {
			if _, ok := c.ProfileByName(pn); !ok {
				return fmt.Errorf("в группе %s указан неизвестный профиль: %s", g.Name, pn)
			}
		}
	}
	return nil
}

func (p Profile) EffectiveListen(cfg Config) string {
	if p.Listen != "" {
		return p.Listen
	}
	return cfg.Defaults.Listen
}

func (p Profile) EffectiveHealthURL(cfg Config) string {
	if p.Health != "" {
		return p.Health
	}
	return cfg.Defaults.HealthURL
}

func (p Profile) Address() string {
	port := p.Port
	if port == 0 {
		port = 22
	}
	return fmt.Sprintf("%s:%d", p.Host, port)
}
