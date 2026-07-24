package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"tunnelctl/internal/app"
	"tunnelctl/internal/bootstrap"
	"tunnelctl/internal/config"
	"tunnelctl/internal/console"
	"tunnelctl/internal/elevation"
	"tunnelctl/internal/logx"
	"tunnelctl/internal/sshproxy"
	"tunnelctl/internal/supervisor"
	"tunnelctl/internal/versioninfo"
)

func main() {
	args := os.Args[1:]
	handled, exitCode, err := elevation.MaybeRelaunch(args)
	if err != nil {
		console.WriteLevel(os.Stderr, "ОШИБКА", "%v", err)
		os.Exit(1)
	}
	if handled {
		os.Exit(exitCode)
	}
	os.Exit(run(args))
}

func run(args []string) int {
	versioninfo.Set(app.Version)
	if needsTimestampedStdout(args) {
		restore, err := console.EnableStdoutTimestamps()
		if err != nil {
			console.WriteLevel(os.Stderr, "ПРЕДУПРЕЖДЕНИЕ", "не удалось включить метки времени в консоли: %v", err)
		} else {
			defer restore()
		}
	}

	if isImportCommand(args) {
		configPath, err := parseImportArgs(args[1:])
		if err != nil {
			console.WriteLevel(os.Stderr, "ОШИБКА", "%v", err)
			return 1
		}
		if err := bootstrap.RunImport(configPath); err != nil {
			console.WriteLevel(os.Stderr, "ОШИБКА", "%v", err)
			return 1
		}
		return 0
	}

	if isRestartCommand(args) {
		if err := runRestart(args[1:]); err != nil {
			console.WriteLevel(os.Stderr, "ОШИБКА", "%v", err)
			return 1
		}
		return 0
	}

	if err := app.Run(args); err != nil {
		console.WriteLevel(os.Stderr, "ОШИБКА", "%v", err)
		return 1
	}
	printVersionDetails(args)
	if isGeneralHelp(args) {
		fmt.Println("\nДополнительные команды:")
		fmt.Println("  tunnelctl import                  повторно импортировать команды ssh -D")
		fmt.Println("  tunnelctl restart                 перезапустить управляемый tunnelctl текущим бинарником")
	}
	return 0
}

func needsTimestampedStdout(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "connect", "status", "stop", "switch", "doctor":
		return true
	case "autostart":
		if len(args) < 2 || args[1] == "print" {
			return false
		}
		for _, arg := range args[2:] {
			if arg == "--dry-run" {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func printVersionDetails(args []string) {
	if len(args) == 0 {
		return
	}
	switch args[0] {
	case "status":
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		state, err := supervisor.Status(ctx)
		if err == nil {
			fmt.Println("Версия управляемого tunnelctl:", state.ApplicationVersion)
			fmt.Println("Версия управляющего протокола:", state.Version)
		}
	case "doctor":
		fmt.Println("Версия схемы конфигурации:", config.CurrentSchemaVersion)
	}
}

func isImportCommand(args []string) bool {
	return len(args) > 0 && args[0] == "import"
}

func isRestartCommand(args []string) bool {
	return len(args) > 0 && args[0] == "restart"
}

func isGeneralHelp(args []string) bool {
	return len(args) > 0 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h")
}

func parseImportArgs(args []string) (string, error) {
	configPath := ""
	for index := 0; index < len(args); index++ {
		argument := args[index]
		switch {
		case argument == "--config" || argument == "-config":
			if index+1 >= len(args) {
				return "", errors.New("после --config нужен путь")
			}
			configPath = args[index+1]
			index++
		case strings.HasPrefix(argument, "--config="):
			configPath = strings.TrimPrefix(argument, "--config=")
		case strings.HasPrefix(argument, "-config="):
			configPath = strings.TrimPrefix(argument, "-config=")
		default:
			return "", fmt.Errorf("лишний аргумент import: %s", argument)
		}
	}
	return configPath, nil
}

var restartHealthCheck = checkRestartHealth

func checkRestartHealth(state supervisor.State) error {
	if state.ActiveProfile == "" || state.Listen == "" {
		return fmt.Errorf("новый экземпляр ещё не сообщил активный профиль и SOCKS-адрес")
	}
	cfg, err := config.Load(state.ConfigPath)
	if err != nil {
		return fmt.Errorf("не удалось прочитать конфигурацию нового экземпляра: %w", err)
	}
	profile, ok := cfg.ProfileByName(state.ActiveProfile)
	if !ok {
		return fmt.Errorf("активный профиль %s отсутствует в конфигурации", state.ActiveProfile)
	}
	timeout := time.Duration(cfg.Defaults.HealthTimeoutSec) * time.Second
	return sshproxy.CheckHTTPViaSocks(state.Listen, profile.EffectiveHealthURL(cfg), timeout)
}

func restartStateReady(state supervisor.State) (bool, error) {
	switch state.Status {
	case "работает":
		return true, nil
	case "SOCKS-порт открыт, выполняется проверка":
		if err := restartHealthCheck(state); err != nil {
			return false, fmt.Errorf("проверка SOCKS после перезапуска не прошла: %w", err)
		}
		return true, nil
	default:
		return false, nil
	}
}

func runRestart(args []string) error {
	if len(args) == 1 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h") {
		fmt.Println("Использование: tunnelctl restart")
		fmt.Println("Корректно завершает дочерний SSH и заменяет работающий tunnelctl этим бинарником.")
		return nil
	}
	if len(args) != 0 {
		return fmt.Errorf("лишний аргумент restart: %s", args[0])
	}
	if err := logx.Init(); err != nil {
		console.WriteLevel(os.Stderr, "ПРЕДУПРЕЖДЕНИЕ", "не удалось включить логирование: %v", err)
	} else {
		defer logx.Close()
	}

	statusCtx, statusCancel := context.WithTimeout(context.Background(), 4*time.Second)
	oldState, err := supervisor.Status(statusCtx)
	statusCancel()
	if err != nil {
		return fmt.Errorf("управляемый tunnelctl сейчас не запущен или недоступен: %w", err)
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("не удалось определить путь текущего бинарника: %w", err)
	}

	restartCtx, restartCancel := context.WithTimeout(context.Background(), 15*time.Second)
	message, err := supervisor.Restart(restartCtx, executable)
	restartCancel()
	if err != nil {
		return err
	}
	logx.Info("%s", message)

	deadline := time.Now().Add(75 * time.Second)
	lastStatus := "новый экземпляр ещё не отвечает"
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		state, statusErr := supervisor.Status(ctx)
		cancel()
		if statusErr == nil {
			lastStatus = state.Status
			if !state.StartedAt.Equal(oldState.StartedAt) {
				if state.ApplicationVersion != versioninfo.Current() {
					return fmt.Errorf("запущена неожиданная версия tunnelctl: %s вместо %s", state.ApplicationVersion, versioninfo.Current())
				}
				ready, readyErr := restartStateReady(state)
				if readyErr != nil {
					lastStatus = readyErr.Error()
				} else if ready {
					console.WriteLevel(os.Stdout, "ИНФО", "tunnelctl %s перезапущен; активный профиль: %s", state.ApplicationVersion, state.ActiveProfile)
					return nil
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("команда перезапуска принята, но новый экземпляр не подтвердил работоспособность; последнее состояние: %s", lastStatus)
}
