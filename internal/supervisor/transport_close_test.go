//go:build !windows

package supervisor

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestCloseReleasesClientWaitingForReply(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(t.TempDir(), "run"))
	server, err := listenControl()
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	closeServer := func() { once.Do(func() { _ = server.Close() }) }
	t.Cleanup(closeServer)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := Status(ctx); done <- err }()
	select {
	case <-server.Requests():
		// Намеренно не отвечаем: остановка должна освободить ожидающего клиента.
	case <-time.After(time.Second):
		t.Fatal("запрос не принят")
	}
	closeServer()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("остановленный сервер вернул успешный ответ")
		}
	case <-time.After(time.Second):
		t.Fatal("клиент остался ждать после остановки сервера")
	}
	select {
	case _, ok := <-server.Requests():
		if ok {
			t.Fatal("канал остался открыт")
		}
	case <-time.After(time.Second):
		t.Fatal("обработчики не завершились")
	}
}

func TestSwitchReplyCanTakeLongerThanDefaultDeadline(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(t.TempDir(), "run"))
	server, err := listenControl()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 16*time.Second)
	defer cancel()
	type result struct {
		state State
		err   error
	}
	done := make(chan result, 1)
	go func() { state, _, err := Switch(ctx, "next"); done <- result{state, err} }()
	var request controlEnvelope
	select {
	case request = <-server.Requests():
		if request.Request.Action != "switch" {
			t.Fatalf("ожидался switch, получено %q", request.Request.Action)
		}
	case <-time.After(time.Second):
		t.Fatal("запрос switch не принят")
	}
	// Сервер должен дождаться результата SSH, хотя обычный запрос ограничен 10 с.
	time.Sleep(11 * time.Second)
	request.Reply(Response{OK: true, State: &State{ActiveProfile: "second"}})
	select {
	case got := <-done:
		if got.err != nil || got.state.ActiveProfile != "second" {
			t.Fatalf("ответ после 10 секунд потерян: %v, состояние %#v", got.err, got.state)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("клиент не получил ответ switch")
	}
}
