package sshproxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"tunnelctl/internal/commanddiag"
	"tunnelctl/internal/config"
)

const maxCapturedStderr = 64 << 10

type boundedBuffer struct {
	mu   sync.Mutex
	data []byte
	max  int
}

func newBoundedBuffer(max int) *boundedBuffer { return &boundedBuffer{max: max} }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	original := len(p)
	if b.max <= 0 {
		return original, nil
	}
	if len(p) >= b.max {
		b.data = append(b.data[:0], p[len(p)-b.max:]...)
		return original, nil
	}
	if overflow := len(b.data) + len(p) - b.max; overflow > 0 {
		copy(b.data, b.data[overflow:])
		b.data = b.data[:len(b.data)-overflow]
	}
	b.data = append(b.data, p...)
	return original, nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(append([]byte(nil), b.data...))
}

func waitPortOrExit(ctx context.Context, addr string, timeout time.Duration, exited <-chan error) (error, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	tryDial := func() bool {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}
	if tryDial() {
		return nil, nil
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case err := <-exited:
			if err == nil {
				return nil, errors.New("ssh завершился до открытия локального SOCKS-порта")
			}
			return err, fmt.Errorf("ssh завершился до открытия локального SOCKS-порта: %w", err)
		case <-ticker.C:
			if tryDial() {
				return nil, nil
			}
		case <-deadline.C:
			return nil, fmt.Errorf("локальный порт %s не открылся за %s", addr, timeout)
		}
	}
}

func waitExit(exited <-chan error, timeout time.Duration) (error, bool) {
	select {
	case err := <-exited:
		return err, true
	case <-time.After(timeout):
		return nil, false
	}
}

func logSSHFailure(contextName string, attempt uint64, p config.Profile, args []string, err error, stderr string) {
	commanddiag.LogFailure(
		commanddiag.Metadata{
			Context: fmt.Sprintf("ssh attempt=%d %s", attempt, contextName),
			Profile: p.Name,
			Address: p.Address(),
		},
		"ssh",
		args,
		err,
		stderr,
		sshSecrets(p)...,
	)
}

func logSSHStart(contextName string, attempt uint64, p config.Profile, args []string) {
	commanddiag.LogStart(
		commanddiag.Metadata{
			Context: fmt.Sprintf("ssh attempt=%d %s", attempt, contextName),
			Profile: p.Name,
			Address: p.Address(),
		},
		"ssh",
		args,
		sshSecrets(p)...,
	)
}

func sshSecrets(p config.Profile) []string {
	secrets := []string{p.User}
	if p.Key != "" {
		secrets = append(secrets, p.Key, expandPath(p.Key))
	}
	return uniqueNonEmpty(secrets)
}

func uniqueNonEmpty(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
