package autostart

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const OwnershipMarker = "Создано tunnelctl. Ручные изменения могут быть заменены."

type State string

const (
	StateNotInstalled State = "не установлен"
	StateStopped      State = "установлен, но не запущен"
	StateRunning      State = "установлен и запущен"
	StateUnknown      State = "состояние неизвестно"
	StateDamaged      State = "конфигурация повреждена"
	StateForeign      State = "объект не принадлежит tunnelctl"
)

type Spec struct {
	Target     string
	Executable string
	ConfigPath string
	System     bool
	RunAs      string
}

type Plan struct {
	Mechanism string
	Object    string
	Mode      string
	Command   string
	Content   string
}

type Status struct {
	Mechanism string
	Object    string
	State     State
	Detail    string
}

type Result struct {
	Changed bool
	Message string
	Status  Status
}

type FileSystem interface {
	ReadFile(string) ([]byte, error)
	WriteFile(string, []byte, os.FileMode) error
	MkdirAll(string, os.FileMode) error
	Remove(string) error
	Rename(string, string) error
	Stat(string) (os.FileInfo, error)
}

type osFS struct{}

func (osFS) ReadFile(p string) ([]byte, error)                 { return os.ReadFile(p) }
func (osFS) WriteFile(p string, b []byte, m os.FileMode) error { return os.WriteFile(p, b, m) }
func (osFS) MkdirAll(p string, m os.FileMode) error            { return os.MkdirAll(p, m) }
func (osFS) Remove(p string) error                             { return os.Remove(p) }
func (osFS) Rename(a, b string) error                          { return os.Rename(a, b) }
func (osFS) Stat(p string) (os.FileInfo, error)                { return os.Stat(p) }

type CommandRunner interface {
	Run(name string, args ...string) (string, error)
}

type execRunner struct{}

func (execRunner) Run(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return decodeCommandOutput(out), err
}

type Backend interface {
	Plan(Spec) (Plan, error)
	Install(Spec) (Result, error)
	Status() (Status, error)
	Start() (Result, error)
	Stop() (Result, error)
	Remove() (Result, error)
}

// Manager предоставляет единый интерфейс платформенного автозапуска.
type Manager struct {
	backend Backend
}

func New() *Manager {
	return &Manager{backend: currentBackend(osFS{}, execRunner{})}
}

func NewWithBackend(backend Backend) *Manager {
	return &Manager{backend: backend}
}

func (m *Manager) ConfigureMode(system bool, runAs string) error {
	if setter, ok := m.backend.(interface{ configureMode(bool, string) error }); ok {
		return setter.configureMode(system, runAs)
	}
	if system {
		return errors.New("системный режим на этой платформе не поддерживается")
	}
	return nil
}

func (m *Manager) Plan(spec Spec) (Plan, error)      { return m.backend.Plan(spec) }
func (m *Manager) Install(spec Spec) (Result, error) { return m.backend.Install(spec) }
func (m *Manager) Status() (Status, error)           { return m.backend.Status() }
func (m *Manager) Start() (Result, error)            { return m.backend.Start() }
func (m *Manager) Stop() (Result, error)             { return m.backend.Stop() }
func (m *Manager) Remove() (Result, error)           { return m.backend.Remove() }

func validateSpec(spec Spec) error {
	if spec.Target == "" {
		return errors.New("не указано имя профиля или группы")
	}
	if !filepath.IsAbs(spec.Executable) {
		return errors.New("путь к tunnelctl должен быть абсолютным")
	}
	if !filepath.IsAbs(spec.ConfigPath) {
		return errors.New("путь к конфигурации должен быть абсолютным")
	}
	for _, value := range []string{spec.Target, spec.Executable, spec.ConfigPath, spec.RunAs} {
		if strings.ContainsAny(value, "\r\n\x00") {
			return errors.New("аргументы автозапуска содержат недопустимые символы")
		}
	}
	return nil
}

func atomicWrite(fs FileSystem, path string, content []byte, mode os.FileMode) error {
	if err := fs.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := fs.WriteFile(tmp, content, mode); err != nil {
		return err
	}
	if err := fs.Rename(tmp, path); err != nil {
		_ = fs.Remove(tmp)
		return err
	}
	return nil
}

func inspectOwned(fs FileSystem, path string, expected []byte) (exists, owned, same bool, old []byte, err error) {
	old, err = fs.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, false, false, nil, nil
	}
	if err != nil {
		return false, false, false, nil, err
	}
	return true, bytes.Contains(old, []byte(OwnershipMarker)), bytes.Equal(old, expected), old, nil
}

func safeCommand(executable, target, configPath string) string {
	return fmt.Sprintf("%s connect %s --watch --config %s", quoteDisplay(executable), quoteDisplay(target), quoteDisplay(configPath))
}

func quoteDisplay(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t\"") {
		return value
	}
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}
