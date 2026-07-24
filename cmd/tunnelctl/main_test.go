package main

import (
	"errors"
	"testing"

	"tunnelctl/internal/supervisor"
)

func TestRestartStateReadyChecksSocksBeforeAcceptingListeningState(t *testing.T) {
	oldCheck := restartHealthCheck
	t.Cleanup(func() { restartHealthCheck = oldCheck })

	called := 0
	restartHealthCheck = func(state supervisor.State) error {
		called++
		if state.ActiveProfile != "first" || state.Listen != "127.0.0.1:1080" {
			t.Fatalf("неверное состояние передано health-check: %#v", state)
		}
		return nil
	}

	ready, err := restartStateReady(supervisor.State{
		Status:        "SOCKS-порт открыт, выполняется проверка",
		ActiveProfile: "first",
		Listen:        "127.0.0.1:1080",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ready || called != 1 {
		t.Fatalf("ready=%v called=%d", ready, called)
	}
}

func TestRestartStateReadyRetriesFailedSocksCheck(t *testing.T) {
	oldCheck := restartHealthCheck
	t.Cleanup(func() { restartHealthCheck = oldCheck })

	restartHealthCheck = func(supervisor.State) error { return errors.New("proxy unavailable") }
	ready, err := restartStateReady(supervisor.State{Status: "SOCKS-порт открыт, выполняется проверка"})
	if ready || err == nil {
		t.Fatalf("ready=%v err=%v", ready, err)
	}
}

func TestRestartStateReadyTrustsSupervisorHealthSuccess(t *testing.T) {
	oldCheck := restartHealthCheck
	t.Cleanup(func() { restartHealthCheck = oldCheck })

	restartHealthCheck = func(supervisor.State) error {
		t.Fatal("для состояния работает не нужен повторный health-check")
		return nil
	}
	ready, err := restartStateReady(supervisor.State{Status: "работает"})
	if err != nil || !ready {
		t.Fatalf("ready=%v err=%v", ready, err)
	}
}
