package app

import (
	"bufio"
	"context"
	"errors"
	"flag"
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
		fs := flag.NewFlagSet("bootstrap", flag.ExitOnError)
		configPath := fs.String("config", "", "путь к конфигу")
		_ = fs.Parse(args[1:])
		return bootstrap.Run(*configPath)
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

func printHelp() {
	fmt.Println(`tunnelctl — локальный SOCKS5 поверх SSH

Команды:
  tunnelctl                      показать меню выбора
  tunnelctl bootstrap            мастер настройки
  tunnelctl connect <имя>        подключиться к профилю или группе
  tunnelctl connect <имя> --watch держать соединение и переподключаться
  tunnelctl doctor               проверить окружение и конфиг
  tunnelctl status               проверить локальный SOCKS-порт
  tunnelctl version              версия

Пример:
  tunnelctl connect yandex --watch`)
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
	fs := flag.NewFlagSet("connect", flag.ExitOnError)
	configPath := fs.String("config", "", "путь к конфигу")
	watch := fs.Bool("watch", true, "держать соединение и переподключаться")
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		return errors.New("нужно указать имя профиля, алиас или группу")
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	return connectByName(cfg, fs.Arg(0), *watch)
}

func connectByName(cfg config.Config, name string, watch bool) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	ctx := signalContext()
	if p, ok := cfg.ResolveProfile(name); ok {
		return sshproxy.Run(ctx, cfg, p, watch)
	}
	if g, ok := cfg.ResolveGroup(name); ok {
		if len(g.Profiles) == 0 {
			return fmt.Errorf("группа %s пустая", name)
		}
		for {
			for _, pn := range g.Profiles {
				p, ok := cfg.ProfileByName(pn)
				if !ok {
					fmt.Println("В группе указан неизвестный профиль:", pn)
					continue
				}
				fmt.Println("Пробую профиль:", p.Name)
				err := sshproxy.Run(ctx, cfg, p, watch)
				if ctx.Err() != nil {
					return nil
				}
				if err != nil {
					fmt.Println("Профиль не сработал:", err)
					continue
				}
				return nil
			}
			if !watch {
				return errors.New("ни один профиль группы не подключился")
			}
			fmt.Println("Все профили группы не сработали. Новая попытка через 30 секунд.")
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(30 * time.Second):
			}
		}
	}
	return fmt.Errorf("профиль или группа не найдены: %s", name)
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
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	configPath := fs.String("config", "", "путь к конфигу")
	_ = fs.Parse(args)
	fmt.Println("Диагностика tunnelctl")
	fmt.Println("Версия:", Version)
	fmt.Println("Платформа:", runtime.GOOS, runtime.GOARCH)
	fmt.Println("Конфиг:", choose(*configPath, paths.ConfigPath()))
	fmt.Println("Лог:", paths.LogPath())
	cfg, err := config.Load(*configPath)
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
