package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"tunnelctl/internal/bootstrap"
	"tunnelctl/internal/config"
	"tunnelctl/internal/logx"
	"tunnelctl/internal/paths"
	"tunnelctl/internal/sshproxy"
)

const Version = "0.1.0"

func Run(args []string) error {
	if err := logx.Init(); err != nil {
		fmt.Println("Предупреждение: не удалось включить логирование:", err)
	}
	defer logx.Close()
	if len(args) == 0 {
		return interactive("")
	}
	switch args[0] {
	case "bootstrap":
		return cmdBootstrap(args[1:])
	case "connect":
		return cmdConnect(args[1:])
	case "doctor":
		return cmdDoctor(args[1:])
	case "status":
		return cmdStatus(args[1:])
	case "help", "--help", "-h":
		printHelp()
		return nil
	case "version", "--version", "-v":
		fmt.Println("tunnelctl", Version)
		return nil
	default:
		return fmt.Errorf("неизвестная команда: %s", args[0])
	}
}

func cmdBootstrap(args []string) error {
	configPath := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--config" || a == "-config":
			if i+1 >= len(args) {
				return errors.New("после --config нужен путь")
			}
			configPath = args[i+1]
			i++
		case strings.HasPrefix(a, "--config="):
			configPath = strings.TrimPrefix(a, "--config=")
		case strings.HasPrefix(a, "-config="):
			configPath = strings.TrimPrefix(a, "-config=")
		default:
			return fmt.Errorf("неизвестный аргумент bootstrap: %s", a)
		}
	}
	return bootstrap.Run(configPath)
}

func printHelp() {
	fmt.Println(`tunnelctl — локальный SOCKS5 поверх SSH

Команды:
  tunnelctl                         показать меню выбора
  tunnelctl bootstrap               мастер настройки
  tunnelctl connect <имя>           подключиться к профилю или группе
  tunnelctl connect <имя> --watch   держать соединение и переподключаться
  tunnelctl connect <имя> --no-watch один запуск без автоматического повтора
  tunnelctl doctor                  проверить окружение и конфиг
  tunnelctl status                  проверить локальный SOCKS-порт
  tunnelctl version                 версия

Примеры:
  tunnelctl connect yandex --watch
  tunnelctl connect auto --watch`)
}

func interactive(configPath string) error {
	cfg, err := config.Load(configPath)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Println("Конфиг не найден. Запускаю мастер настройки.")
		return bootstrap.Run(configPath)
	}
	if err != nil {
		return err
	}
	if len(cfg.Profiles) == 0 && len(cfg.Groups) == 0 {
		fmt.Println("В конфиге нет профилей. Запускаю мастер настройки.")
		return bootstrap.Run(configPath)
	}
	reader := bufio.NewReader(os.Stdin)
	items := make([]string, 0, len(cfg.Groups)+len(cfg.Profiles))
	fmt.Println("Выбери туннель:")
	for _, g := range cfg.Groups {
		name := g.Name
		if g.Alias != "" {
			name = g.Alias
		}
		items = append(items, name)
		fmt.Printf("  %d. группа %s (%d профилей)\n", len(items), name, len(g.Profiles))
	}
	for _, p := range cfg.Profiles {
		name := p.Name
		if p.Alias != "" {
			name = p.Alias
		}
		items = append(items, name)
		fmt.Printf("  %d. %s -> %s@%s, SOCKS %s\n", len(items), name, p.User, p.Address(), p.EffectiveListen(cfg))
	}
	fmt.Print("Номер или имя: ")
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	if n, err := strconv.Atoi(line); err == nil && n >= 1 && n <= len(items) {
		line = items[n-1]
	}
	return connectByName(cfg, line, true)
}

func cmdConnect(args []string) error {
	configPath := ""
	watch := true
	name := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--config" || a == "-config":
			if i+1 >= len(args) {
				return errors.New("после --config нужен путь")
			}
			configPath = args[i+1]
			i++
		case strings.HasPrefix(a, "--config="):
			configPath = strings.TrimPrefix(a, "--config=")
		case strings.HasPrefix(a, "-config="):
			configPath = strings.TrimPrefix(a, "-config=")
		case a == "--watch" || a == "-watch":
			watch = true
		case a == "--no-watch" || a == "-no-watch":
			watch = false
		case a == "--watch=false" || a == "-watch=false":
			watch = false
		case a == "--watch=true" || a == "-watch=true":
			watch = true
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("неизвестный аргумент connect: %s", a)
		default:
			if name != "" {
				return fmt.Errorf("лишний аргумент connect: %s", a)
			}
			name = a
		}
	}
	if name == "" {
		return errors.New("нужно указать имя профиля, алиас или группу")
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	return connectByName(cfg, name, watch)
}

func connectByName(cfg config.Config, name string, watch bool) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	ctx := signalContext()
	if p, ok := cfg.ResolveProfile(name); ok {
		return runSingleProfile(ctx, cfg, p, watch)
	}
	if g, ok := cfg.ResolveGroup(name); ok {
		return runGroup(ctx, cfg, g, watch)
	}
	return fmt.Errorf("профиль или группа не найдены: %s", name)
}

func runSingleProfile(ctx context.Context, cfg config.Config, p config.Profile, watch bool) error {
	fmt.Println("Запускаю одиночный профиль:", p.Name)
	logx.Info("запуск одиночного профиля: %s", p.Name)
	return sshproxy.Run(ctx, cfg, p, watch)
}

func runGroup(ctx context.Context, cfg config.Config, g config.Group, watch bool) error {
	profiles, err := resolveGroupProfiles(cfg, g)
	if err != nil {
		return err
	}
	fmt.Printf("Запускаю группу %s, стратегия: %s, профилей: %d\n", groupDisplayName(g), emptyAs(g.Strategy, "failover"), len(profiles))
	logx.Info("запуск группы %s, профилей: %d", groupDisplayName(g), len(profiles))

	minDelay, maxDelay := reconnectBounds(cfg)
	cycleDelay := minDelay
	cycle := 0
	for {
		cycle++
		fmt.Printf("Круг failover #%d\n", cycle)
		logx.Info("круг failover #%d для группы %s", cycle, groupDisplayName(g))

		for i, p := range profiles {
			select {
			case <-ctx.Done():
				return nil
			default:
			}

			fmt.Printf("Пробую профиль %d/%d: %s (%s@%s)\n", i+1, len(profiles), p.Name, p.User, p.Address())
			logx.Info("failover: пробую профиль %d/%d: %s", i+1, len(profiles), p.Name)
			started := time.Now()
			err := sshproxy.RunOnce(ctx, cfg, p, watch)
			runDuration := time.Since(started)

			if ctx.Err() != nil {
				return nil
			}
			if err == nil {
				return nil
			}

			fmt.Printf("Профиль %s завершился с ошибкой: %v\n", p.Name, err)
			logx.Warn("failover: профиль %s завершился с ошибкой: %v", p.Name, err)

			if stableEnoughForDelayReset(cfg, runDuration) {
				cycleDelay = minDelay
			}
			if !watch {
				continue
			}
			if i+1 < len(profiles) {
				fmt.Printf("Перехожу к следующему профилю через %s...\n", minDelay)
				if !sleepContext(ctx, minDelay) {
					return nil
				}
			}
		}

		if !watch {
			return errors.New("ни один профиль группы не подключился")
		}
		fmt.Printf("Все профили группы завершились ошибкой. Новый круг через %s...\n", cycleDelay)
		logx.Warn("failover: все профили группы %s завершились ошибкой, задержка %s", groupDisplayName(g), cycleDelay)
		if !sleepContext(ctx, cycleDelay) {
			return nil
		}
		cycleDelay *= 2
		if cycleDelay > maxDelay {
			cycleDelay = maxDelay
		}
	}
}

func resolveGroupProfiles(cfg config.Config, g config.Group) ([]config.Profile, error) {
	if len(g.Profiles) == 0 {
		return nil, fmt.Errorf("группа %s пустая", groupDisplayName(g))
	}
	profiles := make([]config.Profile, 0, len(g.Profiles))
	for _, pn := range g.Profiles {
		p, ok := cfg.ProfileByName(pn)
		if !ok {
			return nil, fmt.Errorf("в группе %s указан неизвестный профиль: %s", groupDisplayName(g), pn)
		}
		profiles = append(profiles, p)
	}
	return profiles, nil
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

func stableEnoughForDelayReset(cfg config.Config, d time.Duration) bool {
	interval := time.Duration(cfg.Defaults.HealthIntervalSec) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return d >= 2*interval
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

func groupDisplayName(g config.Group) string {
	if g.Alias != "" {
		return g.Alias
	}
	return g.Name
}

func emptyAs(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func signalContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ch
		fmt.Println()
		fmt.Println("Получен сигнал остановки, завершаю работу...")
		cancel()
	}()
	return ctx
}

func cmdDoctor(args []string) error {
	configPath := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--config" || a == "-config":
			if i+1 >= len(args) {
				return errors.New("после --config нужен путь")
			}
			configPath = args[i+1]
			i++
		case strings.HasPrefix(a, "--config="):
			configPath = strings.TrimPrefix(a, "--config=")
		case strings.HasPrefix(a, "-config="):
			configPath = strings.TrimPrefix(a, "-config=")
		default:
			return fmt.Errorf("неизвестный аргумент doctor: %s", a)
		}
	}
	fmt.Println("Диагностика tunnelctl")
	fmt.Println("Версия:", Version)
	fmt.Println("Платформа:", runtime.GOOS, runtime.GOARCH)
	fmt.Println("Конфиг:", choose(configPath, paths.ConfigPath()))
	fmt.Println("Лог:", paths.LogPath())
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Println("Конфиг: ошибка чтения:", err)
		return nil
	}
	fmt.Printf("Профилей: %d, групп: %d\n", len(cfg.Profiles), len(cfg.Groups))
	for _, p := range cfg.Profiles {
		listen := p.EffectiveListen(cfg)
		fmt.Printf("- %s (%s): %s@%s, SOCKS %s\n", p.Name, p.Alias, p.User, p.Address(), listen)
		if p.Key != "" {
			if _, err := os.Stat(expandHome(p.Key)); err != nil {
				fmt.Println("  ключ: не найден:", p.Key)
			} else {
				fmt.Println("  ключ: найден")
			}
		}
		checkPort(listen)
	}
	fmt.Println("Команда установки Go в Termux при необходимости:")
	fmt.Println("  pkg update && pkg install -y golang git curl")
	fmt.Println("Команда установки Go в Ubuntu/Debian при доступном sudo:")
	fmt.Println("  sudo apt update && sudo apt install -y golang-go git curl")
	fmt.Println("Команда установки Go в Windows через winget:")
	fmt.Println("  winget install --id GoLang.Go -e")
	return nil
}

func cmdStatus(args []string) error {
	cfg, err := config.Load("")
	if err != nil {
		return err
	}
	addr := cfg.Defaults.Listen
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		fmt.Println("Локальный SOCKS не отвечает на", addr)
		return nil
	}
	_ = conn.Close()
	fmt.Println("Локальный порт открыт:", addr)
	return nil
}

func checkPort(addr string) {
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		_ = ln.Close()
		fmt.Println("  порт: свободен")
		return
	}
	fmt.Println("  порт: занят или недоступен:", err)
}

func choose(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return h + string(os.PathSeparator) + p[2:]
		}
	}
	return p
}
