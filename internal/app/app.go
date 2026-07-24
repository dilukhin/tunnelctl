package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"tunnelctl/internal/autostart"
	"tunnelctl/internal/bootstrap"
	"tunnelctl/internal/config"
	"tunnelctl/internal/logx"
	"tunnelctl/internal/paths"
	"tunnelctl/internal/sshproxy"
	"tunnelctl/internal/supervisor"
)

const Version = "0.3.0"

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
	case "stop":
		return cmdStop(args[1:])
	case "switch":
		return cmdSwitch(args[1:])
	case "autostart":
		return cmdAutostart(args[1:])
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
  tunnelctl                          показать меню выбора
  tunnelctl bootstrap                мастер настройки
  tunnelctl connect <имя>            подключиться к профилю или группе
  tunnelctl connect <имя> --watch    держать соединение и переподключаться
  tunnelctl connect <имя> --no-watch один запуск без автоматического повтора
  tunnelctl status                   состояние управляемого туннеля и SOCKS
  tunnelctl switch <профиль>         переключиться на указанный профиль
  tunnelctl switch next              перейти к следующему профилю группы
  tunnelctl stop                     корректно остановить управляемый туннель
  tunnelctl autostart install <имя>  установить автозапуск
  tunnelctl autostart status         состояние механизма автозапуска
  tunnelctl autostart start          запустить установленный механизм
  tunnelctl autostart stop           остановить установленный механизм
  tunnelctl autostart remove         удалить автозапуск
  tunnelctl autostart print <имя>    показать создаваемый объект
  tunnelctl doctor                   проверить окружение и конфиг
  tunnelctl version                  версия

Общие параметры:
  --config <путь>                    явный путь к конфигурации

Параметры autostart install/print:
  --dry-run                          только предварительный просмотр
  --system                           системная служба Linux
  --run-as <пользователь>            пользователь системной службы Linux

Примеры:
  tunnelctl connect auto --watch
  tunnelctl switch next
  tunnelctl autostart install auto --dry-run
  tunnelctl autostart install auto --config /home/user/.config/tunnelctl/tunnelctl.json`)
}

func cmdBootstrap(args []string) error {
	configPath, err := parseOnlyConfig("bootstrap", args)
	if err != nil {
		return err
	}
	return bootstrap.Run(configPath)
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
		fmt.Printf("  %d. %s, SOCKS %s\n", len(items), name, p.EffectiveListen(cfg))
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
	return connectByName(cfg, config.EffectivePath(configPath), line, true)
}

type connectArgs struct {
	configPath string
	watch      bool
	name       string
}

func parseConnectArgs(args []string) (connectArgs, error) {
	parsed := connectArgs{watch: true}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--config" || a == "-config":
			if i+1 >= len(args) {
				return parsed, errors.New("после --config нужен путь")
			}
			parsed.configPath = args[i+1]
			i++
		case strings.HasPrefix(a, "--config="):
			parsed.configPath = strings.TrimPrefix(a, "--config=")
		case strings.HasPrefix(a, "-config="):
			parsed.configPath = strings.TrimPrefix(a, "-config=")
		case a == "--watch" || a == "-watch" || a == "--watch=true" || a == "-watch=true":
			parsed.watch = true
		case a == "--no-watch" || a == "-no-watch" || a == "--watch=false" || a == "-watch=false":
			parsed.watch = false
		case strings.HasPrefix(a, "-"):
			return parsed, fmt.Errorf("неизвестный аргумент connect: %s", a)
		default:
			if parsed.name != "" {
				return parsed, fmt.Errorf("лишний аргумент connect: %s", a)
			}
			parsed.name = a
		}
	}
	if parsed.name == "" {
		return parsed, errors.New("нужно указать имя профиля, алиас или группу")
	}
	return parsed, nil
}

func cmdConnect(args []string) error {
	parsed, err := parseConnectArgs(args)
	if err != nil {
		return err
	}
	cfg, err := config.Load(parsed.configPath)
	if err != nil {
		return err
	}
	return connectByName(cfg, config.EffectivePath(parsed.configPath), parsed.name, parsed.watch)
}

func connectByName(cfg config.Config, configPath, name string, watch bool) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if _, ok := cfg.ResolveTarget(name); !ok {
		return fmt.Errorf("профиль или группа не найдены: %s", name)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return supervisor.Run(ctx, supervisor.Options{
		Config:     cfg,
		ConfigPath: configPath,
		Target:     name,
		Watch:      watch,
	})
}

func cmdStatus(args []string) error {
	configPath, err := parseOnlyConfig("status", args)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	state, stateErr := supervisor.Status(ctx)
	if stateErr == nil {
		fmt.Println("Управляемый tunnelctl: запущен")
		fmt.Println("PID:", state.PID)
		fmt.Println("Исходная цель:", state.OriginalTarget, "("+state.OriginalType+")")
		fmt.Println("Активный профиль:", emptyAs(state.ActiveProfile, "не определён"))
		fmt.Println("Локальный SOCKS:", emptyAs(state.Listen, "не определён"))
		fmt.Println("Состояние:", state.Status)
		if !state.LastHealthSuccess.IsZero() {
			fmt.Println("Последняя успешная проверка:", state.LastHealthSuccess.Format(time.RFC3339))
		}
		if state.LastHealthError != "" {
			fmt.Println("Последняя ошибка проверки:", state.LastHealthError)
		}
		fmt.Println("Лог:", state.LogPath)
		return printCurrentHealth(state, configPath)
	}

	fmt.Println("Управляемый tunnelctl сейчас не запущен.")
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("не удалось прочитать конфигурацию для проверки SOCKS: %w", err)
	}
	addr := cfg.Defaults.Listen
	conn, dialErr := net.DialTimeout("tcp", addr, 2*time.Second)
	if dialErr != nil {
		fmt.Println("Локальный SOCKS не отвечает на", addr)
		return nil
	}
	_ = conn.Close()
	fmt.Println("Локальный порт открыт:", addr)
	fmt.Println("Принадлежность процесса tunnelctl не подтверждена.")
	if err := sshproxy.CheckHTTPViaSocks(addr, cfg.Defaults.HealthURL, time.Duration(cfg.Defaults.HealthTimeoutSec)*time.Second); err != nil {
		fmt.Println("Проверка прокси не прошла:", err)
	} else {
		fmt.Println("Проверка прокси успешна, но владелец SOCKS-порта не определён.")
	}
	return nil
}

func printCurrentHealth(state supervisor.State, overrideConfigPath string) error {
	configPath := state.ConfigPath
	if overrideConfigPath != "" {
		configPath = overrideConfigPath
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Println("Текущая проверка работоспособности: не выполнена:", err)
		return nil
	}
	p, ok := cfg.ProfileByName(state.ActiveProfile)
	if !ok {
		fmt.Println("Текущая проверка работоспособности: активный профиль отсутствует в конфигурации")
		return nil
	}
	err = sshproxy.CheckHTTPViaSocks(p.EffectiveListen(cfg), p.EffectiveHealthURL(cfg), time.Duration(cfg.Defaults.HealthTimeoutSec)*time.Second)
	if err != nil {
		fmt.Println("Текущая проверка работоспособности: ошибка:", err)
	} else {
		fmt.Println("Текущая проверка работоспособности: успешно")
	}
	return nil
}

func cmdStop(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("лишний аргумент stop: %s", args[0])
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	message, err := supervisor.Stop(ctx)
	if err != nil {
		return fmt.Errorf("управляемый tunnelctl сейчас не запущен или недоступен: %w", err)
	}
	fmt.Println(message)
	return nil
}

func cmdSwitch(args []string) error {
	if len(args) == 0 {
		return errors.New("нужно указать профиль, алиас или next")
	}
	if len(args) > 1 {
		return fmt.Errorf("лишний аргумент switch: %s", args[1])
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	state, message, err := supervisor.Switch(ctx, args[0])
	if err != nil {
		return err
	}
	if message != "" {
		fmt.Println(message)
	}
	if state.Listen != "" {
		fmt.Println("SOCKS5 слушает", state.Listen)
	}
	if !state.LastHealthSuccess.IsZero() {
		fmt.Println("Проверка прокси успешна.")
	}
	return nil
}

type autostartArgs struct {
	action     string
	target     string
	configPath string
	dryRun     bool
	system     bool
	runAs      string
}

func parseAutostartArgs(args []string) (autostartArgs, error) {
	var parsed autostartArgs
	if len(args) == 0 {
		return parsed, errors.New("нужно указать действие autostart")
	}
	parsed.action = args[0]
	switch parsed.action {
	case "install", "status", "start", "stop", "remove", "print":
	default:
		return parsed, fmt.Errorf("неизвестное действие autostart: %s", parsed.action)
	}
	for i := 1; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--config" || a == "-config":
			if i+1 >= len(args) {
				return parsed, errors.New("после --config нужен путь")
			}
			parsed.configPath = args[i+1]
			i++
		case strings.HasPrefix(a, "--config="):
			parsed.configPath = strings.TrimPrefix(a, "--config=")
		case a == "--dry-run":
			parsed.dryRun = true
		case a == "--system":
			parsed.system = true
		case a == "--run-as":
			if i+1 >= len(args) {
				return parsed, errors.New("после --run-as нужен пользователь")
			}
			parsed.runAs = args[i+1]
			i++
		case strings.HasPrefix(a, "--run-as="):
			parsed.runAs = strings.TrimPrefix(a, "--run-as=")
		case strings.HasPrefix(a, "-"):
			return parsed, fmt.Errorf("неизвестный аргумент autostart %s: %s", parsed.action, a)
		default:
			if parsed.target != "" {
				return parsed, fmt.Errorf("лишний аргумент autostart %s: %s", parsed.action, a)
			}
			parsed.target = a
		}
	}
	if (parsed.action == "install" || parsed.action == "print") && parsed.target == "" {
		return parsed, fmt.Errorf("для autostart %s нужно указать профиль или группу", parsed.action)
	}
	if parsed.action != "install" && parsed.action != "print" && parsed.target != "" {
		return parsed, fmt.Errorf("autostart %s не принимает имя профиля или группы", parsed.action)
	}
	if parsed.dryRun && parsed.action != "install" && parsed.action != "print" {
		return parsed, errors.New("--dry-run поддерживается только для autostart install и print")
	}
	return parsed, nil
}

func cmdAutostart(args []string) error {
	parsed, err := parseAutostartArgs(args)
	if err != nil {
		return err
	}
	manager := autostart.New()
	if err := manager.ConfigureMode(parsed.system, parsed.runAs); err != nil {
		return err
	}

	switch parsed.action {
	case "status":
		status, err := manager.Status()
		if err != nil {
			return err
		}
		printAutostartStatus(status)
		printManagedTunnelSummary()
		return nil
	case "start":
		result, err := manager.Start()
		if err != nil {
			return err
		}
		fmt.Println(result.Message)
		printAutostartStatus(result.Status)
		return nil
	case "stop":
		result, err := manager.Stop()
		if err != nil {
			return err
		}
		fmt.Println(result.Message)
		printAutostartStatus(result.Status)
		return nil
	case "remove":
		result, err := manager.Remove()
		if err != nil {
			return err
		}
		fmt.Println(result.Message)
		printAutostartStatus(result.Status)
		return nil
	}

	spec, cfg, err := makeAutostartSpec(parsed)
	if err != nil {
		return err
	}
	if _, ok := cfg.ResolveTarget(parsed.target); !ok {
		return fmt.Errorf("профиль или группа не найдены: %s", parsed.target)
	}
	plan, err := manager.Plan(spec)
	if err != nil {
		return err
	}
	printAutostartPlan(plan)
	if parsed.action == "print" || parsed.dryRun {
		fmt.Println()
		fmt.Print(plan.Content)
		if parsed.dryRun {
			fmt.Println("Предварительный просмотр завершён. Система не изменена.")
		}
		return nil
	}
	result, err := manager.Install(spec)
	if err != nil {
		return err
	}
	fmt.Println(result.Message)
	printAutostartStatus(result.Status)
	return nil
}

func makeAutostartSpec(parsed autostartArgs) (autostart.Spec, config.Config, error) {
	cfg, err := config.Load(parsed.configPath)
	if err != nil {
		return autostart.Spec{}, config.Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return autostart.Spec{}, config.Config{}, err
	}
	executable, err := os.Executable()
	if err != nil {
		return autostart.Spec{}, config.Config{}, err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return autostart.Spec{}, config.Config{}, err
	}
	configPath, err := filepath.Abs(config.EffectivePath(parsed.configPath))
	if err != nil {
		return autostart.Spec{}, config.Config{}, err
	}
	return autostart.Spec{
		Target:     parsed.target,
		Executable: executable,
		ConfigPath: configPath,
		System:     parsed.system,
		RunAs:      parsed.runAs,
	}, cfg, nil
}

func printAutostartPlan(plan autostart.Plan) {
	fmt.Println("Механизм:", plan.Mechanism)
	fmt.Println("Объект:", plan.Object)
	fmt.Println("Режим:", plan.Mode)
	fmt.Println("Команда:", plan.Command)
}

func printAutostartStatus(status autostart.Status) {
	fmt.Println("Механизм автозапуска:", status.Mechanism)
	fmt.Println("Объект:", status.Object)
	fmt.Println("Состояние:", status.State)
	if status.Detail != "" {
		fmt.Println("Подробности:", status.Detail)
	}
}

func printManagedTunnelSummary() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	state, err := supervisor.Status(ctx)
	if err != nil {
		fmt.Println("Управляемый туннель: наличие запущенного туннеля не удалось определить")
		return
	}
	fmt.Println("Управляемый туннель: запущен")
	fmt.Println("Активный профиль:", emptyAs(state.ActiveProfile, "не определён"))
	fmt.Println("SOCKS:", emptyAs(state.Listen, "не определён"))
	fmt.Println("Важно: состояние процесса не доказывает работоспособность туннеля; используй tunnelctl status для health-check.")
}

func cmdDoctor(args []string) error {
	configPath, err := parseOnlyConfig("doctor", args)
	if err != nil {
		return err
	}
	fmt.Println("Диагностика tunnelctl")
	fmt.Println("Версия:", Version)
	fmt.Println("Платформа:", runtime.GOOS, runtime.GOARCH)
	fmt.Println("Конфиг:", config.EffectivePath(configPath))
	fmt.Println("Лог:", paths.LogPath())
	fmt.Println("State:", paths.StatePath())
	fmt.Println("Управляющий канал:", paths.ControlPath())
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Println("Конфиг: ошибка чтения:", err)
		return nil
	}
	if err := cfg.Validate(); err != nil {
		fmt.Println("Конфиг: ошибка проверки:", err)
		return nil
	}
	fmt.Printf("Профилей: %d, групп: %d\n", len(cfg.Profiles), len(cfg.Groups))
	for _, p := range cfg.Profiles {
		listen := p.EffectiveListen(cfg)
		fmt.Printf("- %s (%s), SOCKS %s\n", p.Name, p.Alias, listen)
		if p.Key != "" {
			if _, err := os.Stat(expandHome(p.Key)); err != nil {
				fmt.Println("  ключ: не найден")
			} else {
				fmt.Println("  ключ: найден")
			}
		}
		checkPort(listen)
	}
	return nil
}

func parseOnlyConfig(command string, args []string) (string, error) {
	configPath := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--config" || a == "-config":
			if i+1 >= len(args) {
				return "", errors.New("после --config нужен путь")
			}
			configPath = args[i+1]
			i++
		case strings.HasPrefix(a, "--config="):
			configPath = strings.TrimPrefix(a, "--config=")
		case strings.HasPrefix(a, "-config="):
			configPath = strings.TrimPrefix(a, "-config=")
		default:
			return "", fmt.Errorf("неизвестный аргумент %s: %s", command, a)
		}
	}
	return configPath, nil
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

func groupDisplayName(g config.Group) string {
	if g.Alias != "" {
		return g.Alias
	}
	return g.Name
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

func emptyAs(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[2:])
		}
	}
	return p
}
