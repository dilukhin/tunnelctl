package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"tunnelctl/internal/config"
	"tunnelctl/internal/supervisor"
)

func cmdEnsure(args []string) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Println("tunnelctl ensure <имя> [--config <путь>] — сохранить владельца или работать как connect --watch")
		return nil
	}
	parsed, err := parseConnectArgs(args)
	if err != nil {
		return err
	}
	if !parsed.watch {
		return errors.New("ensure требует --watch")
	}
	cfg, err := config.Load(parsed.configPath)
	if err != nil {
		return err
	}
	// Termux собирает вывод фоновой команды до её завершения. Долгоживущий
	// владелец пишет диагностику в штатный журнал, а не копит её для PendingIntent.
	quiet, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer quiet.Close()
	stdin, stdout, stderr := os.Stdin, os.Stdout, os.Stderr
	os.Stdin, os.Stdout, os.Stderr = quiet, quiet, quiet
	restore := func() { os.Stdin, os.Stdout, os.Stderr = stdin, stdout, stderr }
	defer restore()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	err = supervisor.Run(ctx, supervisor.Options{
		Config: cfg, ConfigPath: parsed.configPath, Target: parsed.name, Watch: true, Ensure: true,
	})
	restore()
	if errors.Is(err, supervisor.ErrAlreadyManaged) {
		// Узкий машинный ответ без адресов, путей, ключей и состояния SSH.
		fmt.Println("TUNNELCTL_ENSURE_MANAGED_V1")
		return nil
	}
	return err
}
