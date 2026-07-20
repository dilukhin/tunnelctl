package autostart

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"tunnelctl/internal/paths"
)

const windowsTaskName = `\tunnelctl\tunnelctl`

type windowsBackend struct {
	fs     FileSystem
	runner CommandRunner
}

func newWindowsBackend(fs FileSystem, runner CommandRunner) *windowsBackend {
	return &windowsBackend{fs: fs, runner: runner}
}

func (b *windowsBackend) configureMode(system bool, runAs string) error {
	if system {
		return errors.New("системный режим на этой платформе не поддерживается")
	}
	return nil
}

func (b *windowsBackend) Plan(spec Spec) (Plan, error) {
	if err := validateSpec(spec); err != nil {
		return Plan{}, err
	}
	if spec.System {
		return Plan{}, errors.New("системный режим Windows на первом этапе не поддерживается; используется задача текущего пользователя")
	}
	content := renderWindowsTask(spec)
	return Plan{
		Mechanism: "Планировщик заданий Windows",
		Object:    windowsTaskName,
		Mode:      "текущий пользователь, запуск при входе",
		Command:   safeCommand(spec.Executable, spec.Target, spec.ConfigPath),
		Content:   content,
	}, nil
}

func (b *windowsBackend) Install(spec Spec) (Result, error) {
	plan, err := b.Plan(spec)
	if err != nil {
		return Result{}, err
	}
	old, queryErr := b.queryXML()
	exists := queryErr == nil
	if queryErr != nil && !isTaskNotFound(old) {
		return Result{}, fmt.Errorf("не удалось проверить существующую задачу %s: %s: %w", windowsTaskName, strings.TrimSpace(old), queryErr)
	}
	if exists && !strings.Contains(old, OwnershipMarker) {
		return Result{}, fmt.Errorf("задача %s существует, но не принадлежит tunnelctl", windowsTaskName)
	}
	same := exists && normalizeXML(old) == normalizeXML(plan.Content)
	if same {
		status, _ := b.Status()
		if status.State == StateRunning || status.State == StateStopped {
			return Result{Changed: false, Message: "Задача уже установлена с теми же параметрами", Status: status}, nil
		}
	}
	if err := b.fs.MkdirAll(paths.StateDir(), 0o700); err != nil {
		return Result{}, err
	}
	temp := filepath.Join(paths.StateDir(), "tunnelctl-task.xml")
	if err := b.fs.WriteFile(temp, encodeUTF16LEWithBOM(plan.Content), 0o600); err != nil {
		return Result{}, err
	}
	defer b.fs.Remove(temp)
	if out, err := b.runner.Run("schtasks.exe", "/Create", "/TN", windowsTaskName, "/XML", temp, "/F"); err != nil {
		return Result{}, fmt.Errorf("не удалось создать задачу %s: %s: %w", windowsTaskName, out, err)
	}
	if out, err := b.runner.Run("schtasks.exe", "/Run", "/TN", windowsTaskName); err != nil {
		if !exists {
			_, _ = b.runner.Run("schtasks.exe", "/Delete", "/TN", windowsTaskName, "/F")
		} else {
			restore := filepath.Join(paths.StateDir(), "tunnelctl-task-restore.xml")
			if writeErr := b.fs.WriteFile(restore, encodeUTF16LEWithBOM(old), 0o600); writeErr == nil {
				_, _ = b.runner.Run("schtasks.exe", "/Create", "/TN", windowsTaskName, "/XML", restore, "/F")
				_ = b.fs.Remove(restore)
			}
		}
		return Result{}, fmt.Errorf("задача создана, но не запущена: %s: %w", out, err)
	}
	status, _ := b.Status()
	return Result{Changed: !same, Message: "Задача автозапуска установлена и запущена", Status: status}, nil
}

func (b *windowsBackend) Status() (Status, error) {
	xmlText, err := b.queryXML()
	if err != nil {
		if isTaskNotFound(xmlText) {
			return Status{Mechanism: "Планировщик заданий Windows", Object: windowsTaskName, State: StateNotInstalled}, nil
		}
		return Status{Mechanism: "Планировщик заданий Windows", Object: windowsTaskName, State: StateUnknown, Detail: strings.TrimSpace(xmlText)}, nil
	}
	if !strings.Contains(xmlText, OwnershipMarker) {
		return Status{Mechanism: "Планировщик заданий Windows", Object: windowsTaskName, State: StateForeign}, nil
	}
	if !strings.Contains(xmlText, "<Command>") || !strings.Contains(xmlText, "connect") || !strings.Contains(xmlText, "--watch") {
		return Status{Mechanism: "Планировщик заданий Windows", Object: windowsTaskName, State: StateDamaged}, nil
	}
	out, queryErr := b.runner.Run("schtasks.exe", "/Query", "/TN", windowsTaskName, "/FO", "LIST", "/V")
	if queryErr != nil {
		return Status{Mechanism: "Планировщик заданий Windows", Object: windowsTaskName, State: StateUnknown, Detail: out}, nil
	}
	lower := strings.ToLower(out)
	if strings.Contains(lower, "running") || strings.Contains(lower, "выполняется") {
		return Status{Mechanism: "Планировщик заданий Windows", Object: windowsTaskName, State: StateRunning, Detail: out}, nil
	}
	return Status{Mechanism: "Планировщик заданий Windows", Object: windowsTaskName, State: StateStopped, Detail: out}, nil
}

func (b *windowsBackend) Start() (Result, error) {
	status, err := b.requireOwned()
	if err != nil {
		return Result{}, err
	}
	if status.State == StateRunning {
		return Result{Changed: false, Message: "Задача уже выполняется", Status: status}, nil
	}
	out, err := b.runner.Run("schtasks.exe", "/Run", "/TN", windowsTaskName)
	if err != nil {
		return Result{}, fmt.Errorf("не удалось запустить задачу: %s: %w", out, err)
	}
	status, _ = b.Status()
	return Result{Changed: true, Message: "Задача запущена", Status: status}, nil
}

func (b *windowsBackend) Stop() (Result, error) {
	status, err := b.requireOwned()
	if err != nil {
		return Result{}, err
	}
	if status.State == StateStopped {
		return Result{Changed: false, Message: "Задача уже остановлена", Status: status}, nil
	}
	out, err := b.runner.Run("schtasks.exe", "/End", "/TN", windowsTaskName)
	if err != nil {
		return Result{}, fmt.Errorf("не удалось остановить задачу: %s: %w", out, err)
	}
	status, _ = b.Status()
	return Result{Changed: true, Message: "Задача остановлена", Status: status}, nil
}

func (b *windowsBackend) Remove() (Result, error) {
	status, err := b.Status()
	if err != nil {
		return Result{}, err
	}
	if status.State == StateNotInstalled {
		return Result{Changed: false, Message: "Задача уже отсутствует", Status: status}, nil
	}
	if status.State == StateForeign {
		return Result{}, errors.New("существующая задача не принадлежит tunnelctl")
	}
	out, err := b.runner.Run("schtasks.exe", "/Delete", "/TN", windowsTaskName, "/F")
	if err != nil {
		return Result{}, fmt.Errorf("не удалось удалить задачу: %s: %w", out, err)
	}
	status.State = StateNotInstalled
	return Result{Changed: true, Message: "Задача удалена", Status: status}, nil
}

func (b *windowsBackend) queryXML() (string, error) {
	return b.runner.Run("schtasks.exe", "/Query", "/TN", windowsTaskName, "/XML")
}

func (b *windowsBackend) requireOwned() (Status, error) {
	status, err := b.Status()
	if err != nil {
		return Status{}, err
	}
	switch status.State {
	case StateNotInstalled:
		return Status{}, errors.New("автозапуск не установлен")
	case StateForeign:
		return Status{}, errors.New("существующая задача не принадлежит tunnelctl")
	case StateDamaged:
		return Status{}, errors.New("задача tunnelctl повреждена")
	}
	return status, nil
}

func renderWindowsTask(spec Spec) string {
	args := strings.Join([]string{
		"connect",
		windowsQuoteArg(spec.Target),
		"--watch",
		"--config",
		windowsQuoteArg(spec.ConfigPath),
	}, " ")
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>%s</Description>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger><Enabled>true</Enabled></LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author"><LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>true</StartWhenAvailable>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <RestartOnFailure><Interval>PT1M</Interval><Count>255</Count></RestartOnFailure>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>%s</Command>
      <Arguments>%s</Arguments>
      <WorkingDirectory>%s</WorkingDirectory>
    </Exec>
  </Actions>
</Task>
`, xmlEscape(OwnershipMarker), xmlEscape(spec.Executable), xmlEscape(args), xmlEscape(filepath.Dir(spec.Executable)))
}

func encodeUTF16LEWithBOM(value string) []byte {
	units := utf16.Encode([]rune(value))
	result := make([]byte, 2+len(units)*2)
	result[0], result[1] = 0xff, 0xfe
	for i, unit := range units {
		binary.LittleEndian.PutUint16(result[2+i*2:], unit)
	}
	return result
}

func xmlEscape(value string) string {
	var b bytes.Buffer
	for _, r := range value {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&apos;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func windowsQuoteArg(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t\n\v\"") {
		return value
	}
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for _, r := range value {
		switch r {
		case '\\':
			slashes++
		case '"':
			b.WriteString(strings.Repeat("\\", slashes*2+1))
			b.WriteRune(r)
			slashes = 0
		default:
			b.WriteString(strings.Repeat("\\", slashes))
			slashes = 0
			b.WriteRune(r)
		}
	}
	b.WriteString(strings.Repeat("\\", slashes*2))
	b.WriteByte('"')
	return b.String()
}

func normalizeXML(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func isTaskNotFound(output string) bool {
	lower := strings.ToLower(output)
	markers := []string{
		"cannot find",
		"system cannot find",
		"does not exist",
		"не удается найти",
		"не удаётся найти",
		"не может найти",
		"не существует",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
