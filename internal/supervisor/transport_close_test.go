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
