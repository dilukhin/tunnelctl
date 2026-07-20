//go:build !windows

package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"tunnelctl/internal/config"
	"tunnelctl/internal/sshproxy"
)

type fakeProfileRunner struct {
	mu   sync.Mutex
	runs []string
}

func (r *fakeProfileRunner) Run(ctx context.Context, cfg config.Config, p config.Profile, watch bool, observe sshproxy.Observer) error {
	r.mu.Lock()
	r.runs = append(r.runs, p.Name)
	r.mu.Unlock()
	now := time.Now()
	observe(sshproxy.Event{Type: sshproxy.EventStarting, Profile: p.Name, Listen: p.EffectiveListen(cfg), Time: now})
	observe(sshproxy.Event{Type: sshproxy.EventListening, Profile: p.Name, Listen: p.EffectiveListen(cfg), Time: now})
	observe(sshproxy.Event{Type: sshproxy.EventHealthSuccess, Profile: p.Name, Listen: p.EffectiveListen(cfg), Time: now})
	<-ctx.Done()
	return nil
}

func TestSelectSwitchTarget(t *testing.T) {
	cfg := config.Config{Profiles: []config.Profile{
		{Name: "first", Alias: "one", User: "u", Host: "h1", Port: 22},
		{Name: "second", Alias: "two", User: "u", Host: "h2", Port: 22},
	}}
	group := cfg.Profiles
	p, _, err := selectSwitchTarget(cfg, group, true, 0, group[0], "next")
	if err != nil || p.Name != "second" {
		t.Fatalf("неверный next: %#v, %v", p, err)
	}
	p, _, err = selectSwitchTarget(cfg, group, true, 0, group[0], "two")
	if err != nil || p.Name != "second" {
		t.Fatalf("алиас не разрешён: %#v, %v", p, err)
	}
	if _, _, err := selectSwitchTarget(cfg, group[:1], false, 0, group[0], "next"); err == nil {
		t.Fatal("next для одиночного профиля должен завершаться ошибкой")
	}
	if _, _, err := selectSwitchTarget(cfg, group, true, 0, group[0], "missing"); err == nil {
		t.Fatal("неизвестный профиль должен завершаться ошибкой")
	}
}

func TestManagedCommandsWithoutSSH(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(root, "runtime"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	cfg := config.DefaultConfig()
	cfg.Defaults.Listen = "127.0.0.1:0"
	cfg.Profiles = []config.Profile{
		{Name: "first", Alias: "one", User: "u", Host: "h1", Port: 22},
		{Name: "second", Alias: "two", User: "u", Host: "h2", Port: 22},
	}
	cfg.Groups = []config.Group{{Name: "auto", Alias: "auto", Strategy: "failover", Profiles: []string{"first", "second"}}}
	runner := &fakeProfileRunner{}
	runDone := make(chan error, 1)
	go func() {
		runDone <- Run(context.Background(), Options{Config: cfg, ConfigPath: filepath.Join(root, "config.json"), Target: "auto", Watch: true, Runner: runner})
	}()

	state := waitForProfile(t, "first")
	if state.OriginalTarget != "auto" || state.OriginalType != "group" {
		t.Fatalf("неверное исходное состояние: %#v", state)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	switched, _, err := Switch(ctx, "next")
	if err != nil {
		t.Fatalf("switch next завершился ошибкой: %v", err)
	}
	if switched.ActiveProfile != "second" {
		t.Fatalf("ожидался second, получено %#v", switched)
	}

	if _, _, err := Switch(ctx, "missing"); err == nil {
		t.Fatal("неизвестный профиль должен вернуть ошибку")
	}

	message, err := Stop(ctx)
	if err != nil {
		t.Fatalf("stop завершился ошибкой: %v", err)
	}
	if message == "" {
		t.Fatal("stop должен вернуть понятное сообщение")
	}
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("супервизор не завершился после stop")
	}
}

func waitForProfile(t *testing.T, profile string) State {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		state, err := Status(ctx)
		cancel()
		if err == nil && state.ActiveProfile == profile && state.Status == "работает" {
			return state
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("профиль %s не стал активным", profile)
	return State{}
}

func TestStateJSONContainsNoSecrets(t *testing.T) {
	state := State{Version: 1, PID: 1, OriginalTarget: "auto", ActiveProfile: "first", Listen: "127.0.0.1:1080", LogPath: "/state/log", ConfigPath: "/config/tunnelctl.json"}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, forbidden := range []string{"private_key", "password", "token", "user@host"} {
		if contains(text, forbidden) {
			t.Fatalf("state содержит запрещённое поле %q: %s", forbidden, text)
		}
	}
}

func contains(value, part string) bool {
	for i := 0; i+len(part) <= len(value); i++ {
		if value[i:i+len(part)] == part {
			return true
		}
	}
	return false
}

func TestResolveTargetRejectsMissing(t *testing.T) {
	cfg := config.DefaultConfig()
	_, _, err := resolveTarget(cfg, "missing")
	if err == nil || errors.Is(err, context.Canceled) {
		t.Fatal("ожидалась ошибка отсутствующей цели")
	}
}
