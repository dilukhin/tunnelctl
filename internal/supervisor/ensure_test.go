//go:build !windows

package supervisor

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"tunnelctl/internal/config"
)

func ensureFixture(t *testing.T) Options {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(root, "runtime"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	cfg := config.DefaultConfig()
	cfg.Defaults.Listen = "127.0.0.1:0"
	cfg.Profiles = []config.Profile{
		{Name: "first", Alias: "one", User: "u", Host: "example.invalid", Port: 22},
		{Name: "second", User: "u", Host: "example.invalid", Port: 22},
	}
	return Options{Config: cfg, ConfigPath: filepath.Join(root, "config.json"),
		Target: "first", Watch: true, Ensure: true, Runner: &fakeProfileRunner{}}
}

func startEnsureOwner(t *testing.T, opts Options) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, opts) }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(5 * time.Second):
				t.Error("владелец не остановился")
			}
		})
	}
	t.Cleanup(stop)
	waitForProfile(t, "first")
	return stop
}

func TestEnsureReusesOwnerAndKeepsManualSwitch(t *testing.T) {
	opts := ensureFixture(t)
	opts.Config.Groups = []config.Group{{Name: "auto", Alias: "all", Strategy: "failover", Profiles: []string{"first", "second"}}}
	opts.Target = "auto"
	stop := startEnsureOwner(t, opts)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := Switch(ctx, "next"); err != nil {
		t.Fatal(err)
	}
	opts.Target = "all"
	second := &fakeProfileRunner{}
	opts.Runner = second
	if err := Run(ctx, opts); !errors.Is(err, ErrAlreadyManaged) {
		t.Fatalf("повтор: %v", err)
	}
	state, err := Status(ctx)
	if err != nil || state.ActiveProfile != "second" {
		t.Fatalf("ручной выбор потерян: %#v, %v", state, err)
	}
	if len(second.runs) != 0 {
		t.Fatal("ensure запустил второго владельца")
	}
}

func TestEnsureRejectsDifferentOwner(t *testing.T) {
	for _, kind := range []string{"config", "target", "no-watch"} {
		t.Run(kind, func(t *testing.T) {
			opts := ensureFixture(t)
			owner := opts
			owner.Ensure = false
			if kind == "no-watch" {
				owner.Watch = false
			}
			stop := startEnsureOwner(t, owner)
			defer stop()
			if kind == "config" {
				opts.ConfigPath += ".other"
			}
			if kind == "target" {
				opts.Target = "second"
			}
			opts.Runner = &fakeProfileRunner{}
			err := Run(context.Background(), opts)
			if err == nil || errors.Is(err, ErrAlreadyManaged) {
				t.Fatalf("конфликт принят: %v", err)
			}
			if len(opts.Runner.(*fakeProfileRunner).runs) != 0 {
				t.Fatal("запущен чужой профиль")
			}
		})
	}
}

func TestEnsureRecoversAfterOwnerExit(t *testing.T) {
	opts := ensureFixture(t)
	stop := startEnsureOwner(t, opts)
	stop()
	stop = startEnsureOwner(t, opts)
	stop()
	runner := opts.Runner.(*fakeProfileRunner)
	if len(runner.runs) != 2 {
		t.Fatalf("число запусков: %d", len(runner.runs))
	}
}

func TestEnsureConcurrentCallsStartOneOwner(t *testing.T) {
	opts := ensureFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = Run(ctx, opts) }()
	}
	waitForProfile(t, "first")
	time.Sleep(100 * time.Millisecond)
	cancel()
	wg.Wait()
	runner := opts.Runner.(*fakeProfileRunner)
	if len(runner.runs) != 1 {
		t.Fatalf("запущено владельцев: %d", len(runner.runs))
	}
}

func TestEnsureDoesNotTakeOccupiedPort(t *testing.T) {
	opts := ensureFixture(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	opts.Config.Defaults.Listen = listener.Addr().String()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := Run(ctx, opts); err == nil {
		t.Fatal("занятый порт принят")
	}
	if len(opts.Runner.(*fakeProfileRunner).runs) != 0 {
		t.Fatal("запущен SSH на занятом порту")
	}
	conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal("посторонний порт повреждён:", err)
	}
	conn.Close()
}

func TestEnsureBusyUnresponsiveOwnerDoesNotStartRunner(t *testing.T) {
	opts := ensureFixture(t)
	server, err := listenControl()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := Run(ctx, opts); err == nil || errors.Is(err, ErrAlreadyManaged) {
		t.Fatalf("неотвечающий владелец принят: %v", err)
	}
	if len(opts.Runner.(*fakeProfileRunner).runs) != 0 {
		t.Fatal("создан второй владелец")
	}
}
