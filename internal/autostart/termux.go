//go:build !windows

package autostart

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tunnelctl/internal/supervisor"
)

type termuxBackend struct {
	fs     FileSystem
	runner CommandRunner
}

func newTermuxBackend(fs FileSystem, runner CommandRunner) *termuxBackend {
	return &termuxBackend{fs: fs, runner: runner}
}

func (b *termuxBackend) configureMode(system bool, runAs string) error {
	if system {
		return errors.New("системный режим на этой платформе не поддерживается")
	}
	return nil
}

func (b *termuxBackend) Plan(spec Spec) (Plan, error) {
	if err := validateSpec(spec); err != nil {
		return Plan{}, err
	}
	if spec.System {
		return Plan{}, errors.New("системный режим для Termux:Boot не поддерживается")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Plan{}, err
	}
	path := filepath.Join(home, ".termux", "boot", "tunnelctl")
	logPath := filepath.Join(home, ".local", "state", "tunnelctl", "termux-boot.log")
	content := renderTermux(spec, logPath)
	return Plan{
		Mechanism: "Termux:Boot",
		Object:    path,
		Mode:      "пользовательский",
		Command:   safeCommand(spec.Executable, spec.Target, spec.ConfigPath),
		Content:   content,
	}, nil
}

func (b *termuxBackend) Install(spec Spec) (Result, error) {
	plan, err := b.Plan(spec)
	if err != nil {
		return Result{}, err
	}
	expected := []byte(plan.Content)
	exists, owned, same, _, err := inspectOwned(b.fs, plan.Object, expected)
	if err != nil {
		return Result{}, err
	}
	if exists && !owned {
		return Result{}, fmt.Errorf("сценарий %s существует, но не принадлежит tunnelctl", plan.Object)
	}
	if same {
		status, _ := b.Status()
		return Result{Changed: false, Message: "Сценарий Termux:Boot уже установлен с теми же параметрами", Status: status}, nil
	}
	if err := atomicWrite(b.fs, plan.Object, expected, 0o700); err != nil {
		return Result{}, fmt.Errorf("не удалось записать сценарий Termux:Boot: %w", err)
	}
	status, _ := b.Status()
	return Result{Changed: true, Message: "Сценарий Termux:Boot установлен", Status: status}, nil
}

func (b *termuxBackend) Status() (Status, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Status{}, err
	}
	path := filepath.Join(home, ".termux", "boot", "tunnelctl")
	data, err := b.fs.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Status{Mechanism: "Termux:Boot", Object: path, State: StateNotInstalled, Detail: b.bootPackageDetail()}, nil
	}
	if err != nil {
		return Status{Mechanism: "Termux:Boot", Object: path, State: StateUnknown}, err
	}
	text := string(data)
	if !strings.Contains(text, OwnershipMarker) {
		return Status{Mechanism: "Termux:Boot", Object: path, State: StateForeign, Detail: b.bootPackageDetail()}, nil
	}
	if !strings.Contains(text, " connect ") || !strings.Contains(text, " --watch") {
		return Status{Mechanism: "Termux:Boot", Object: path, State: StateDamaged, Detail: b.bootPackageDetail()}, nil
	}
	state := StateStopped
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if _, err := supervisor.Status(ctx); err == nil {
		state = StateRunning
	}
	return Status{Mechanism: "Termux:Boot", Object: path, State: state, Detail: b.bootPackageDetail()}, nil
}

func (b *termuxBackend) Start() (Result, error) {
	status, err := b.Status()
	if err != nil {
		return Result{}, err
	}
	if status.State == StateNotInstalled {
		return Result{}, errors.New("сценарий Termux:Boot не установлен")
	}
	return Result{Changed: false, Message: "Termux:Boot запускает сценарий после загрузки устройства. Для немедленного запуска используй tunnelctl connect <имя> --watch", Status: status}, nil
}

func (b *termuxBackend) Stop() (Result, error) {
	status, err := b.Status()
	if err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	message, stopErr := supervisor.Stop(ctx)
	if stopErr != nil {
		return Result{}, fmt.Errorf("не удалось остановить управляемый туннель; ненадёжный поиск процессов не выполняется: %w", stopErr)
	}
	status.State = StateStopped
	return Result{Changed: true, Message: message, Status: status}, nil
}

func (b *termuxBackend) Remove() (Result, error) {
	status, err := b.Status()
	if err != nil {
		return Result{}, err
	}
	if status.State == StateNotInstalled {
		return Result{Changed: false, Message: "Сценарий Termux:Boot уже отсутствует", Status: status}, nil
	}
	if status.State == StateForeign {
		return Result{}, fmt.Errorf("сценарий %s не принадлежит tunnelctl", status.Object)
	}
	if err := b.fs.Remove(status.Object); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Result{}, err
	}
	status.State = StateNotInstalled
	return Result{Changed: true, Message: "Сценарий Termux:Boot удалён", Status: status}, nil
}

func (b *termuxBackend) bootPackageDetail() string {
	commands := [][]string{{"pm", "path", "com.termux.boot"}, {"cmd", "package", "path", "com.termux.boot"}}
	for _, cmd := range commands {
		out, err := b.runner.Run(cmd[0], cmd[1:]...)
		if err == nil && strings.Contains(out, "package:") {
			return "Приложение Termux:Boot обнаружено. Его разрешение на автозапуск всё равно нужно проверить вручную."
		}
	}
	return "Termux:Boot, предположительно, не установлен или не настроен; наличие каталога само по себе это не подтверждает."
}

func renderTermux(spec Spec, logPath string) string {
	interpreter := "#!/data/data/com.termux/files/usr/bin/bash"
	command := strings.Join([]string{
		shellQuote(spec.Executable),
		"connect",
		shellQuote(spec.Target),
		"--watch",
		"--config",
		shellQuote(spec.ConfigPath),
	}, " ")
	return fmt.Sprintf(`%s
# %s
umask 077
if command -v termux-wake-lock >/dev/null 2>&1; then
  termux-wake-lock
fi
mkdir -p %s
while true; do
  %s >> %s 2>&1
  code=$?
  if [ "$code" -eq 0 ]; then
    exit 0
  fi
  sleep 15
done
`, interpreter, OwnershipMarker, shellQuote(filepath.Dir(logPath)), command, shellQuote(logPath))
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
