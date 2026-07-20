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
	"time"

	"tunnelctl/internal/paths"
)

type unixServer struct {
	listener *net.UnixListener
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
	if _, err := os.Stat(path); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		if _, reqErr := platformRequest(ctx, Request{Version: protocolVersion, Action: "status"}); reqErr == nil {
			return nil, errors.New("управляемый tunnelctl уже запущен")
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("не удалось удалить устаревший управляющий сокет: %w", err)
		}
	}
	addr, err := net.ResolveUnixAddr("unix", path)
	if err != nil {
		return nil, err
	}
	l, err := net.ListenUnix("unix", addr)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = l.Close()
		_ = os.Remove(path)
		return nil, err
	}
	s := &unixServer{listener: l, requests: make(chan controlEnvelope), done: make(chan struct{})}
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
	envelope := controlEnvelope{
		Request: req,
		Reply: func(resp Response) {
			_ = json.NewEncoder(conn).Encode(resp)
			close(replied)
		},
	}
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
	deadline, ok := ctx.Deadline()
	if ok {
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
