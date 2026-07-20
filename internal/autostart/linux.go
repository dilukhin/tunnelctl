//go:build !windows

package autostart

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const systemdUnitName = "tunnelctl.service"

type linuxBackend struct {
	fs     FileSystem
	runner CommandRunner
	system bool
	runAs  string
}

func newLinuxBackend(fs FileSystem, runner CommandRunner) *linuxBackend {
	return &linuxBackend{fs: fs, runner: runner}
}

func (b *linuxBackend) configureMode(system bool, runAs string) error {
	b.system = system
	b.runAs = runAs
	return nil
}

func (b *linuxBackend) Plan(spec Spec) (Plan, error) {
	if err := validateSpec(spec); err != nil {
		return Plan{}, err
	}
	b.system = spec.System
	b.runAs = spec.RunAs
	if spec.System && spec.RunAs == "" {
		return Plan{}, errors.New("для системной службы требуется --run-as <пользователь>")
	}
	path, err := b.unitPath(spec.System)
	if err != nil {
		return Plan{}, err
	}
	content := renderSystemd(spec)
	mode := "пользовательский"
	mechanism := "systemd --user"
	if spec.System {
		mode = "системный"
		mechanism = "systemd system"
	}
	return Plan{
		Mechanism: mechanism,
		Object:    path,
		Mode:      mode,
		Command:   safeCommand(spec.Executable, spec.Target, spec.ConfigPath),
		Content:   content,
	}, nil
}

func (b *linuxBackend) Install(spec Spec) (Result, error) {
	if spec.System && os.Geteuid() != 0 {
		return Result{}, errors.New("для установки системной службы требуются права администратора; запусти команду от root")
	}
	plan, err := b.Plan(spec)
	if err != nil {
		return Result{}, err
	}
	expected := []byte(plan.Content)
	exists, owned, same, old, err := inspectOwned(b.fs, plan.Object, expected)
	if err != nil {
		return Result{}, fmt.Errorf("не удалось проверить файл службы %s: %w", plan.Object, err)
	}
	if exists && !owned {
		return Result{}, fmt.Errorf("файл %s существует, но не принадлежит tunnelctl; автоматическая перезапись запрещена", plan.Object)
	}
	if same {
		status, statusErr := b.Status()
		if statusErr == nil && status.State == StateRunning {
			return Result{Changed: false, Message: "Автозапуск уже установлен с теми же параметрами", Status: status}, nil
		}
	}
	if err := atomicWrite(b.fs, plan.Object, expected, 0o600); err != nil {
		return Result{}, fmt.Errorf("не удалось записать файл службы %s: %w", plan.Object, err)
	}
	if _, err := b.systemctl("daemon-reload"); err != nil {
		b.rollback(plan.Object, exists, old)
		return Result{}, fmt.Errorf("файл службы создан, но systemctl daemon-reload завершился ошибкой: %w", err)
	}
	if _, err := b.systemctl("enable", "--now", systemdUnitName); err != nil {
		b.rollback(plan.Object, exists, old)
		_, _ = b.systemctl("daemon-reload")
		return Result{}, fmt.Errorf("служба записана, но не активирована; проверь systemctl status %s: %w", systemdUnitName, err)
	}
	status, _ := b.Status()
	return Result{Changed: !same, Message: "Автозапуск установлен и запущен", Status: status}, nil
}

func (b *linuxBackend) Status() (Status, error) {
	path, err := b.unitPath(b.system)
	if err != nil {
		return Status{}, err
	}
	data, err := b.fs.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Status{Mechanism: b.mechanism(), Object: path, State: StateNotInstalled}, nil
	}
	if err != nil {
		return Status{Mechanism: b.mechanism(), Object: path, State: StateUnknown}, err
	}
	text := string(data)
	if !strings.Contains(text, OwnershipMarker) {
		return Status{Mechanism: b.mechanism(), Object: path, State: StateForeign}, nil
	}
	if !strings.Contains(text, "ExecStart=") || !strings.Contains(text, "connect") || !strings.Contains(text, "--watch") {
		return Status{Mechanism: b.mechanism(), Object: path, State: StateDamaged}, nil
	}
	activeOut, activeErr := b.systemctl("is-active", systemdUnitName)
	enabledOut, _ := b.systemctl("is-enabled", systemdUnitName)
	detail := strings.TrimSpace(strings.Join([]string{activeOut, enabledOut}, "; "))
	if activeErr == nil && strings.TrimSpace(activeOut) == "active" {
		return Status{Mechanism: b.mechanism(), Object: path, State: StateRunning, Detail: detail}, nil
	}
	if strings.TrimSpace(activeOut) == "inactive" || strings.TrimSpace(activeOut) == "failed" || strings.TrimSpace(enabledOut) == "enabled" {
		return Status{Mechanism: b.mechanism(), Object: path, State: StateStopped, Detail: detail}, nil
	}
	return Status{Mechanism: b.mechanism(), Object: path, State: StateUnknown, Detail: detail}, nil
}

func (b *linuxBackend) Start() (Result, error) {
	if b.system && os.Geteuid() != 0 {
		return Result{}, errors.New("для изменения системной службы требуются права администратора; запусти команду от root")
	}
	status, err := b.requireOwned()
	if err != nil {
		return Result{}, err
	}
	if status.State == StateRunning {
		return Result{Changed: false, Message: "Служба уже запущена", Status: status}, nil
	}
	if _, err := b.systemctl("start", systemdUnitName); err != nil {
		return Result{}, fmt.Errorf("не удалось запустить службу через %s: %w", b.mechanism(), err)
	}
	status, _ = b.Status()
	return Result{Changed: true, Message: "Служба запущена", Status: status}, nil
}

func (b *linuxBackend) Stop() (Result, error) {
	if b.system && os.Geteuid() != 0 {
		return Result{}, errors.New("для изменения системной службы требуются права администратора; запусти команду от root")
	}
	status, err := b.requireOwned()
	if err != nil {
		return Result{}, err
	}
	if status.State == StateStopped {
		return Result{Changed: false, Message: "Служба уже остановлена", Status: status}, nil
	}
	if _, err := b.systemctl("stop", systemdUnitName); err != nil {
		return Result{}, fmt.Errorf("не удалось остановить службу через %s: %w", b.mechanism(), err)
	}
	status, _ = b.Status()
	return Result{Changed: true, Message: "Служба остановлена", Status: status}, nil
}

func (b *linuxBackend) Remove() (Result, error) {
	if b.system && os.Geteuid() != 0 {
		return Result{}, errors.New("для изменения системной службы требуются права администратора; запусти команду от root")
	}
	status, err := b.Status()
	if err != nil {
		return Result{}, err
	}
	if status.State == StateNotInstalled {
		return Result{Changed: false, Message: "Автозапуск уже отсутствует", Status: status}, nil
	}
	if status.State == StateForeign {
		return Result{}, fmt.Errorf("объект %s не принадлежит tunnelctl; удаление запрещено", status.Object)
	}
	if _, err := b.systemctl("disable", "--now", systemdUnitName); err != nil {
		return Result{}, fmt.Errorf("не удалось отключить службу; файл не удалён: %w", err)
	}
	if err := b.fs.Remove(status.Object); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Result{}, fmt.Errorf("служба отключена, но файл %s не удалён: %w", status.Object, err)
	}
	if _, err := b.systemctl("daemon-reload"); err != nil {
		return Result{}, fmt.Errorf("файл службы удалён, но daemon-reload завершился ошибкой: %w", err)
	}
	resultStatus := Status{Mechanism: b.mechanism(), Object: status.Object, State: StateNotInstalled}
	return Result{Changed: true, Message: "Автозапуск удалён", Status: resultStatus}, nil
}

func (b *linuxBackend) requireOwned() (Status, error) {
	status, err := b.Status()
	if err != nil {
		return Status{}, err
	}
	switch status.State {
	case StateNotInstalled:
		return Status{}, errors.New("автозапуск не установлен")
	case StateForeign:
		return Status{}, errors.New("существующий объект не принадлежит tunnelctl")
	case StateDamaged:
		return Status{}, errors.New("конфигурация автозапуска повреждена")
	}
	return status, nil
}

func (b *linuxBackend) unitPath(system bool) (string, error) {
	if system {
		return filepath.Join(string(filepath.Separator), "etc", "systemd", "system", systemdUnitName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user", systemdUnitName), nil
}

func (b *linuxBackend) systemctl(args ...string) (string, error) {
	if b.system {
		return b.runner.Run("systemctl", args...)
	}
	userArgs := append([]string{"--user"}, args...)
	return b.runner.Run("systemctl", userArgs...)
}

func (b *linuxBackend) mechanism() string {
	if b.system {
		return "systemd system"
	}
	return "systemd --user"
}

func (b *linuxBackend) rollback(path string, existed bool, old []byte) {
	if existed {
		_ = atomicWrite(b.fs, path, old, 0o600)
	} else {
		_ = b.fs.Remove(path)
	}
}

func renderSystemd(spec Spec) string {
	wantedBy := "default.target"
	userLine := ""
	if spec.System {
		wantedBy = "multi-user.target"
		userLine = "User=" + systemdEscape(spec.RunAs) + "\n"
	}
	args := []string{spec.Executable, "connect", spec.Target, "--watch", "--config", spec.ConfigPath}
	escaped := make([]string, len(args))
	for i, arg := range args {
		escaped[i] = systemdEscape(arg)
	}
	return fmt.Sprintf(`# %s
[Unit]
Description=Устойчивый SSH/SOCKS5-туннель tunnelctl
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
%sExecStart=%s
Restart=on-failure
RestartSec=15s
KillSignal=SIGTERM
TimeoutStopSec=20s

[Install]
WantedBy=%s
`, OwnershipMarker, userLine, strings.Join(escaped, " "), wantedBy)
}

func systemdEscape(value string) string {
	value = strings.ReplaceAll(value, "%", "%%")
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}
