//go:build !windows

package supervisor

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"tunnelctl/internal/config"
	"tunnelctl/internal/sshproxy"
	"tunnelctl/internal/versioninfo"
)

type restartFakeRunner struct{}

func (restartFakeRunner) Run(ctx context.Context, cfg config.Config, p config.Profile, watch bool, observe sshproxy.Observer) error {
	now := time.Now()
	observe(sshproxy.Event{Type: sshproxy.EventStarting, Profile: p.Name, Listen: p.EffectiveListen(cfg), Time: now})
	observe(sshproxy.Event{Type: sshproxy.EventListening, Profile: p.Name, Listen: p.EffectiveListen(cfg), Time: now})
	observe(sshproxy.Event{Type: sshproxy.EventHealthSuccess, Profile: p.Name, Listen: p.EffectiveListen(cfg), Time: now})
	<-ctx.Done()
	return nil
}

func TestRestartReplacesSupervisorAfterManagedStop(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(root, "runtime"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	versioninfo.Set("0.3.0-test")

	candidate := filepath.Join(root, "tunnelctl.new")
	if err := os.WriteFile(candidate, []byte("test"), 0o700); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var gotExecutable string
	var gotArgs []string
	oldReplace := replaceProcess
	replaceProcess = func(executable string, args []string) error {
		mu.Lock()
		defer mu.Unlock()
		gotExecutable = executable
		gotArgs = append([]string(nil), args...)
		return nil
	}
	t.Cleanup(func() { replaceProcess = oldReplace })

	cfg := config.DefaultConfig()
	cfg.Defaults.Listen = "127.0.0.1:0"
	cfg.Profiles = []config.Profile{{Name: "first", User: "u", Host: "h", Port: 22}}
	configPath := filepath.Join(root, "config.json")
	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(), Options{Config: cfg, ConfigPath: configPath, Target: "first", Watch: true, Runner: restartFakeRunner{}})
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		state, err := Status(ctx)
		cancel()
		if err == nil && state.Status == "работает" {
			if state.ApplicationVersion != "0.3.0-test" {
				t.Fatalf("версия приложения не записана в state: %#v", state)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("супервизор не перешёл в рабочее состояние")
		}
		time.Sleep(20 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	message, err := Restart(ctx, candidate)
	cancel()
	if err != nil {
		t.Fatalf("restart завершился ошибкой: %v", err)
	}
	if message == "" {
		t.Fatal("restart должен вернуть понятное сообщение")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run завершился ошибкой: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("супервизор не завершил замену процесса")
	}

	absoluteCandidate, _ := filepath.Abs(candidate)
	mu.Lock()
	defer mu.Unlock()
	if gotExecutable != absoluteCandidate {
		t.Fatalf("запущен неверный бинарник: %q", gotExecutable)
	}
	wantArgs := []string{"connect", "first", "--watch", "--config", configPath}
	absoluteConfig, _ := filepath.Abs(configPath)
	wantArgs[len(wantArgs)-1] = absoluteConfig
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("неверные аргументы перезапуска: %#v, ожидалось %#v", gotArgs, wantArgs)
	}
}

func TestValidateRestartExecutableRejectsDirectory(t *testing.T) {
	if _, err := validateRestartExecutable(t.TempDir()); err == nil {
		t.Fatal("каталог не должен приниматься как бинарник")
	}
}
