//go:build !windows

package supervisor

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"tunnelctl/internal/config"
)

func TestSecondSupervisorDoesNotStartProfile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(root, "runtime"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))

	cfg := config.DefaultConfig()
	cfg.Defaults.Listen = "127.0.0.1:0"
	cfg.Profiles = []config.Profile{{Name: "first", User: "u", Host: "h", Port: 22}}
	configPath := filepath.Join(root, "config.json")

	firstRunner := &fakeProfileRunner{}
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- Run(context.Background(), Options{Config: cfg, ConfigPath: configPath, Target: "first", Watch: true, Runner: firstRunner})
	}()
	waitForProfile(t, "first")

	secondRunner := &fakeProfileRunner{}
	err := Run(context.Background(), Options{Config: cfg, ConfigPath: configPath, Target: "first", Watch: true, Runner: secondRunner})
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("ожидалась ErrAlreadyRunning, получено %v", err)
	}
	secondRunner.mu.Lock()
	secondRuns := len(secondRunner.runs)
	secondRunner.mu.Unlock()
	if secondRuns != 0 {
		t.Fatalf("второй экземпляр запустил профиль %d раз", secondRuns)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := Stop(ctx); err != nil {
		t.Fatalf("не удалось остановить первый экземпляр: %v", err)
	}
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("первый экземпляр не завершился")
	}
}
