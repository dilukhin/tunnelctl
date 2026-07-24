//go:build !windows

package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"tunnelctl/internal/paths"
)

type unixServer struct {
	listener *net.UnixListener
	lockFile *os.File
	requests chan controlEnvelope
	done     chan struct{}
}

func platformListen() (controlServer, error) {
	path := paths.ControlPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("не удалось ограничить доступ к каталогу управляющего сокета: %w", err)
	}

	lockFile, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть блокировку управляющего канала: %w", err)
	}
	if err := os.Chmod(path+".lock", 0o600); err != nil {
		_ = lockFile.Close()
		return nil, fmt.Errorf("не удалось ограничить доступ к блокировке управляющего канала: %w", err)
	}
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lockFile.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrAlreadyRunning
		}
		return nil, fmt.Errorf("не удалось захватить блокировку управляющего канала: %w", err)
	}
	releaseLock := func() {
		_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		_ = lockFile.Close()
	}

	if _, err := os.Stat(path); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, requestErr := platformRequest(ctx, Request{Version: protocolVersion, Action: "status"})
		cancel()
		if requestErr == nil {
			releaseLock()
			return nil, ErrAlreadyRunning
		}
		if err := os.Remove(path); err != nil {
			releaseLock()
			return nil, fmt.Errorf("не удалось удалить устаревший управляющий сокет: %w", err)
		}
	} else if !os.IsNotExist(err) {
		releaseLock()
		return nil, err
	}

	addr, err := net.ResolveUnixAddr("unix", path)
	if err != nil {
		releaseLock()
		return nil, err
	}
	listener, err := net.ListenUnix("unix", addr)
	if err != nil {
		releaseLock()
		if errors.Is(err, syscall.EADDRINUSE) {
			return nil, ErrAlreadyRunning
		}
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		releaseLock()
		return nil, err
	}
	s := &unixServer{listener: listener, lockFile: lockFile, requests: make(chan controlEnvelope), done: make(chan struct{})}
	go s.acceptLoop()
	return s, nil
}

func (s *unixServer) Requests() <-chan controlEnvelope { return s.requests }

func (s *unixServer) Close() error {
	select {
	case <-s.done:
	default:
		close(s.done)
	}
	err := s.listener.Close()
	_ = os.Remove(paths.ControlPath())
	if s.lockFile != nil {
		_ = syscall.Flock(int(s.lockFile.Fd()), syscall.LOCK_UN)
		_ = s.lockFile.Close()
	}
	return err
}

func (s *unixServer) acceptLoop() {
	defer close(s.requests)
	for {
		conn, err := s.listener.AcceptUnix()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				continue
			}
		}
		go s.handle(conn)
	}
}

func (s *unixServer) handle(conn *net.UnixConn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(Response{OK: false, Error: "не удалось прочитать запрос управления"})
		return
	}
	replied := make(chan struct{})
	envelope := controlEnvelope{Request: req, Reply: func(resp Response) { _ = json.NewEncoder(conn).Encode(resp); close(replied) }}
	select {
	case s.requests <- envelope:
		<-replied
	case <-s.done:
		_ = json.NewEncoder(conn).Encode(Response{OK: false, Error: "управляющий канал остановлен"})
	}
}

func platformRequest(ctx context.Context, req Request) (Response, error) {
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "unix", paths.ControlPath())
	if err != nil {
		return Response{}, fmt.Errorf("не удалось подключиться к управляющему каналу tunnelctl: %w", err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	req.Version = protocolVersion
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return Response{}, err
	}
	return resp, nil
}
