package sshproxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"tunnelctl/internal/config"
	"tunnelctl/internal/logx"
)

type EventType string

const (
	EventStarting      EventType = "starting"
	EventListening     EventType = "listening"
	EventHealthSuccess EventType = "health_success"
	EventHealthFailure EventType = "health_failure"
	EventStopped       EventType = "stopped"
)

type Event struct {
	Type    EventType
	Profile string
	Listen  string
	Time    time.Time
	Err     error
}

type Observer func(Event)

// Run запускает одиночный профиль. В режиме watch профиль переподключается к тому же VPS.
func Run(ctx context.Context, cfg config.Config, p config.Profile, watch bool) error {
	minDelay, maxDelay := reconnectBounds(cfg)
	delay := minDelay
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		err := RunOnce(ctx, cfg, p, watch)
		if !watch || !cfg.Defaults.Reconnect.Enabled {
			return err
		}
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			fmt.Println("Туннель завершился с ошибкой:", err)
			logx.Warn("туннель завершился с ошибкой: %v", err)
		}
		fmt.Printf("Повторное подключение к тому же профилю через %s...\n", delay)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(delay):
		}
		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}
}

// RunOnce запускает профиль один раз и возвращает управление при завершении ssh или падении health-check.
func RunOnce(ctx context.Context, cfg config.Config, p config.Profile, watch bool) error {
	return RunOnceObserved(ctx, cfg, p, watch, nil)
}

// RunOnceObserved запускает профиль один раз и сообщает безопасные события жизненного цикла.
func RunOnceObserved(ctx context.Context, cfg config.Config, p config.Profile, watch bool, observe Observer) error {
	if err := validateListen(p.EffectiveListen(cfg), cfg.Defaults.AllowListenAllHosts); err != nil {
		return err
	}
	if _, err := exec.LookPath("ssh"); err != nil {
		return fmt.Errorf("не найден ssh. Установи OpenSSH. Команды: Termux: pkg install openssh; Ubuntu/Debian: sudo apt install openssh-client; Windows: winget install --id Microsoft.OpenSSH.Beta -e")
	}
	return runOnce(ctx, cfg, p, watch, observe)
}

func reconnectBounds(cfg config.Config) (time.Duration, time.Duration) {
	minDelay := time.Duration(cfg.Defaults.Reconnect.MinDelaySec) * time.Second
	maxDelay := time.Duration(cfg.Defaults.Reconnect.MaxDelaySec) * time.Second
	if minDelay <= 0 {
		minDelay = 2 * time.Second
	}
	if maxDelay < minDelay {
		maxDelay = minDelay
	}
	return minDelay, maxDelay
}

func runOnce(ctx context.Context, cfg config.Config, p config.Profile, watch bool, observe Observer) error {
	listen := p.EffectiveListen(cfg)
	args := sshArgs(cfg, p)
	emit(observe, Event{Type: EventStarting, Profile: p.Name, Listen: listen, Time: time.Now()})
	fmt.Println("Запускаю SSH:")
	fmt.Println("  ssh", strings.Join(maskArgs(args), " "))
	logx.Info("запуск ssh: ssh %s", strings.Join(maskArgs(args), " "))

	cmd := exec.Command("ssh", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	stopWatcher := make(chan struct{})
	var stopOnce sync.Once
	stopProcess := func() {
		stopOnce.Do(func() {
			close(stopWatcher)
			killProcess(cmd)
		})
	}
	defer stopProcess()
	go func() {
		select {
		case <-ctx.Done():
			stopProcess()
		case <-stopWatcher:
		}
	}()

	if err := waitPort(ctx, listen, 20*time.Second); err != nil {
		stopProcess()
		return err
	}
	fmt.Printf("SOCKS5 слушает %s, профиль %s\n", listen, p.Name)
	logx.Info("SOCKS5 слушает %s, профиль %s", listen, p.Name)
	emit(observe, Event{Type: EventListening, Profile: p.Name, Listen: listen, Time: time.Now()})

	if watch {
		if err := CheckHTTPViaSocks(listen, p.EffectiveHealthURL(cfg), healthTimeout(cfg)); err != nil {
			emit(observe, Event{Type: EventHealthFailure, Profile: p.Name, Listen: listen, Time: time.Now(), Err: err})
			stopProcess()
			return fmt.Errorf("начальная проверка прокси не прошла: %w", err)
		}
		fmt.Println("Проверка прокси успешна.")
		logx.Info("начальная проверка прокси успешна")
		emit(observe, Event{Type: EventHealthSuccess, Profile: p.Name, Listen: listen, Time: time.Now()})
	}

	if !watch {
		select {
		case <-ctx.Done():
			stopProcess()
			return nil
		case err := <-exited:
			emit(observe, Event{Type: EventStopped, Profile: p.Name, Listen: listen, Time: time.Now(), Err: err})
			return err
		}
	}

	interval := time.Duration(cfg.Defaults.HealthIntervalSec) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	fails := 0
	for {
		select {
		case <-ctx.Done():
			stopProcess()
			return nil
		case err := <-exited:
			emit(observe, Event{Type: EventStopped, Profile: p.Name, Listen: listen, Time: time.Now(), Err: err})
			return err
		case <-ticker.C:
			if err := CheckHTTPViaSocks(listen, p.EffectiveHealthURL(cfg), healthTimeout(cfg)); err != nil {
				fails++
				fmt.Println("Проверка прокси не прошла:", err)
				logx.Warn("проверка прокси не прошла: %v", err)
				emit(observe, Event{Type: EventHealthFailure, Profile: p.Name, Listen: listen, Time: time.Now(), Err: err})
				if fails >= 2 {
					fmt.Println("Останавливаю зависший SSH-процесс и возвращаю ошибку супервизору.")
					stopProcess()
					return errors.New("прокси не работает")
				}
			} else {
				if fails > 0 {
					fmt.Println("Прокси снова работает.")
				}
				fails = 0
				logx.Info("проверка прокси успешна")
				emit(observe, Event{Type: EventHealthSuccess, Profile: p.Name, Listen: listen, Time: time.Now()})
			}
		}
	}
}

func healthTimeout(cfg config.Config) time.Duration {
	return time.Duration(cfg.Defaults.HealthTimeoutSec) * time.Second
}

func emit(observe Observer, event Event) {
	if observe != nil {
		observe(event)
	}
}

func sshArgs(cfg config.Config, p config.Profile) []string {
	listen := p.EffectiveListen(cfg)
	args := []string{
		"-D", listen,
		"-N",
		"-T",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", fmt.Sprintf("ConnectTimeout=%d", cfg.Defaults.ConnectTimeoutSec),
		"-p", fmt.Sprint(p.Port),
	}
	if p.Key != "" {
		args = append(args, "-i", expandPath(p.Key))
	}
	args = append(args, fmt.Sprintf("%s@%s", p.User, p.Host))
	return args
}

func maskArgs(args []string) []string {
	out := append([]string(nil), args...)
	for i := 0; i < len(out)-1; i++ {
		if out[i] == "-i" {
			out[i+1] = "<путь-к-ключу-скрыт>"
		}
	}
	if len(out) > 0 {
		last := out[len(out)-1]
		if at := strings.LastIndex(last, "@"); at >= 0 {
			out[len(out)-1] = "<пользователь>@" + last[at+1:]
		}
	}
	return out
}

func expandPath(p string) string {
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[2:])
		}
	}
	return p
}

func validateListen(listen string, allowAll bool) error {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("некорректный локальный адрес %q: %w", listen, err)
	}
	if host == "0.0.0.0" || host == "::" || host == "" {
		if !allowAll {
			return errors.New("по умолчанию запрещено слушать на всех интерфейсах; укажи 127.0.0.1 или включи allow_listen_all_hosts")
		}
	}
	return nil
}

func waitPort(ctx context.Context, addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("локальный порт %s не открылся за %s", addr, timeout)
}

// WaitPortFree ожидает освобождения локального SOCKS-порта.
func WaitPortFree(ctx context.Context, addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			_ = ln.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("локальный порт %s не освободился за %s; возможно, им владеет другой процесс", addr, timeout)
}

func killProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		_ = cmd.Process.Kill()
		return
	}
	_ = cmd.Process.Signal(os.Interrupt)
	time.Sleep(500 * time.Millisecond)
	_ = cmd.Process.Kill()
}

// CheckHTTPViaSocks делает реальный HTTP/HTTPS-запрос через локальный SOCKS5.
func CheckHTTPViaSocks(socksAddr, rawURL string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	host := u.Host
	if !strings.Contains(host, ":") {
		if u.Scheme == "https" {
			host += ":443"
		} else {
			host += ":80"
		}
	}
	conn, err := socksDial(socksAddr, host, timeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	if u.Scheme == "https" {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: u.Hostname()})
		if err := tlsConn.Handshake(); err != nil {
			return err
		}
		conn = tlsConn
	}
	path := u.RequestURI()
	if path == "" {
		path = "/"
	}
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: tunnelctl/0\r\nConnection: close\r\n\r\n", path, u.Host)
	if _, err := conn.Write([]byte(req)); err != nil {
		return err
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 500 {
		return fmt.Errorf("health_url вернул HTTP %d", resp.StatusCode)
	}
	return nil
}

func socksDial(socksAddr, target string, timeout time.Duration) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", socksAddr, timeout)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		conn.Close()
		return nil, err
	}
	var resp [2]byte
	if _, err := io.ReadFull(conn, resp[:]); err != nil {
		conn.Close()
		return nil, err
	}
	if resp[0] != 0x05 || resp[1] != 0x00 {
		conn.Close()
		return nil, errors.New("локальный SOCKS не принял no-auth")
	}
	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		conn.Close()
		return nil, err
	}
	port := 0
	_, _ = fmt.Sscanf(portStr, "%d", &port)
	req := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	req = append(req, []byte(host)...)
	req = append(req, byte(port>>8), byte(port))
	if _, err := conn.Write(req); err != nil {
		conn.Close()
		return nil, err
	}
	var head [4]byte
	if _, err := io.ReadFull(conn, head[:]); err != nil {
		conn.Close()
		return nil, err
	}
	if head[1] != 0x00 {
		conn.Close()
		return nil, fmt.Errorf("SOCKS CONNECT вернул код %d", head[1])
	}
	var skip int
	switch head[3] {
	case 0x01:
		skip = 4
	case 0x03:
		var l [1]byte
		if _, err := io.ReadFull(conn, l[:]); err != nil {
			conn.Close()
			return nil, err
		}
		skip = int(l[0])
	case 0x04:
		skip = 16
	default:
		conn.Close()
		return nil, errors.New("непонятный ответ SOCKS")
	}
	buf := make([]byte, skip+2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}
