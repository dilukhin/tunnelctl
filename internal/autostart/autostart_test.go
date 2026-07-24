//go:build !windows

package autostart

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type fakeRunner struct {
	mu      sync.Mutex
	calls   []string
	handler func(string, []string) (string, error)
}

func (r *fakeRunner) Run(name string, args ...string) (string, error) {
	r.mu.Lock()
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	r.mu.Unlock()
	if r.handler != nil {
		return r.handler(name, args)
	}
	return "", nil
}

func testSpec(t *testing.T) Spec {
	t.Helper()
	return Spec{Target: "auto", Executable: filepath.Join(t.TempDir(), "tunnelctl"), ConfigPath: filepath.Join(t.TempDir(), "tunnelctl.json")}
}

func TestValidateSpecRequiresAbsolutePaths(t *testing.T) {
	spec := Spec{Target: "auto", Executable: "tunnelctl", ConfigPath: "config.json"}
	if err := validateSpec(spec); err == nil {
		t.Fatal("ожидалась ошибка относительных путей")
	}
}

func TestRenderUserSystemd(t *testing.T) {
	spec := testSpec(t)
	text := renderSystemd(spec)
	required := []string{OwnershipMarker, "ExecStart=", "connect", "--watch", "Restart=on-failure", "WantedBy=default.target"}
	for _, value := range required {
		if !strings.Contains(text, value) {
			t.Fatalf("пользовательская служба не содержит %q:\n%s", value, text)
		}
	}
	if strings.Contains(text, "User=") {
		t.Fatal("пользовательская служба не должна задавать User")
	}
}

func TestRenderSystemSystemd(t *testing.T) {
	spec := testSpec(t)
	spec.System = true
	spec.RunAs = "tunnel-user"
	text := renderSystemd(spec)
	if !strings.Contains(text, `User="tunnel-user"`) || !strings.Contains(text, "WantedBy=multi-user.target") {
		t.Fatalf("неверная системная служба:\n%s", text)
	}
}

func TestSystemdEscaping(t *testing.T) {
	got := systemdEscape(`/opt/tunnel ctl/%name"x`)
	if !strings.HasPrefix(got, `"`) || !strings.Contains(got, `%%name`) || !strings.Contains(got, `\"`) {
		t.Fatalf("неверное экранирование systemd: %s", got)
	}
}

func TestRenderTermux(t *testing.T) {
	spec := testSpec(t)
	spec.Target = "group name"
	text := renderTermux(spec, "/tmp/log file")
	for _, value := range []string{OwnershipMarker, "termux-wake-lock", "while true", "--watch", "sleep 15"} {
		if !strings.Contains(text, value) {
			t.Fatalf("сценарий Termux не содержит %q", value)
		}
	}
	if !strings.Contains(text, `'group name'`) {
		t.Fatalf("имя группы не экранировано:\n%s", text)
	}
}

func TestRenderWindowsTask(t *testing.T) {
	spec := testSpec(t)
	spec.Executable = `C:\Program Files\tunnelctl\tunnelctl.exe`
	spec.ConfigPath = `C:\Users\User Name\AppData\Roaming\tunnelctl\tunnelctl.json`
	text := renderWindowsTask(spec)
	for _, value := range []string{OwnershipMarker, "<LogonTrigger>", "<Command>", "connect", "--watch", "RestartOnFailure"} {
		if !strings.Contains(text, value) {
			t.Fatalf("XML задачи не содержит %q", value)
		}
	}
	if !strings.Contains(text, `&quot;C:\Users\User Name`) {
		t.Fatalf("путь конфигурации не заключён в кавычки: %s", text)
	}
}

func TestWindowsQuoteArg(t *testing.T) {
	got := windowsQuoteArg(`C:\Path With Space\file.json`)
	if got != `"C:\Path With Space\file.json"` {
		t.Fatalf("неверное Windows quoting: %s", got)
	}
}

func TestLinuxInstallIsIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	runner := &fakeRunner{handler: func(name string, args []string) (string, error) {
		joined := strings.Join(args, " ")
		switch {
		case strings.Contains(joined, "is-active"):
			return "active", nil
		case strings.Contains(joined, "is-enabled"):
			return "enabled", nil
		default:
			return "", nil
		}
	}}
	backend := newLinuxBackend(osFS{}, runner)
	spec := testSpec(t)
	first, err := backend.Install(spec)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Changed {
		t.Fatal("первая установка должна изменить систему")
	}
	callsAfterFirst := len(runner.calls)
	second, err := backend.Install(spec)
	if err != nil {
		t.Fatal(err)
	}
	if second.Changed {
		t.Fatal("повторная одинаковая установка не должна менять систему")
	}
	if len(runner.calls) > callsAfterFirst+2 {
		t.Fatalf("повторная установка выполнила лишние команды: %#v", runner.calls[callsAfterFirst:])
	}
}

func TestLinuxInstallRefusesForeignObject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".config", "systemd", "user", systemdUnitName)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[Service]\nExecStart=/foreign\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	backend := newLinuxBackend(osFS{}, &fakeRunner{})
	if _, err := backend.Install(testSpec(t)); err == nil {
		t.Fatal("ожидался отказ перезаписи чужого объекта")
	}
}

func TestLinuxStatusStates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".config", "systemd", "user", systemdUnitName)
	runner := &fakeRunner{handler: func(name string, args []string) (string, error) {
		if strings.Contains(strings.Join(args, " "), "is-active") {
			return "active", nil
		}
		return "enabled", nil
	}}
	backend := newLinuxBackend(osFS{}, runner)
	status, err := backend.Status()
	if err != nil || status.State != StateNotInstalled {
		t.Fatalf("ожидалось not installed: %#v, %v", status, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, _ = backend.Status()
	if status.State != StateForeign {
		t.Fatalf("ожидался foreign: %#v", status)
	}
	if err := os.WriteFile(path, []byte("# "+OwnershipMarker+"\n[Service]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, _ = backend.Status()
	if status.State != StateDamaged {
		t.Fatalf("ожидался damaged: %#v", status)
	}
	if err := os.WriteFile(path, []byte(renderSystemd(testSpec(t))), 0o600); err != nil {
		t.Fatal(err)
	}
	status, _ = backend.Status()
	if status.State != StateRunning {
		t.Fatalf("ожидался running: %#v", status)
	}
}

func TestPlanDoesNotContainConfigSecrets(t *testing.T) {
	spec := testSpec(t)
	plan, err := newLinuxBackend(osFS{}, &fakeRunner{}).Plan(spec)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private-key", "password", "user@example.com"} {
		if strings.Contains(plan.Content, secret) || strings.Contains(plan.Command, secret) {
			t.Fatalf("план содержит секрет %q", secret)
		}
	}
}

func TestInspectOwnedMissing(t *testing.T) {
	_, _, _, _, err := inspectOwned(osFS{}, filepath.Join(t.TempDir(), "missing"), []byte("x"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}

func TestWindowsStatusDistinguishesMissingAndUnknown(t *testing.T) {
	missing := newWindowsBackend(osFS{}, &fakeRunner{handler: func(name string, args []string) (string, error) {
		return "ОШИБКА: Системе не удается найти указанный файл.", errors.New("exit status 1")
	}})
	status, err := missing.Status()
	if err != nil || status.State != StateNotInstalled {
		t.Fatalf("отсутствующая задача распознана неверно: %#v, %v", status, err)
	}

	unknown := newWindowsBackend(osFS{}, &fakeRunner{handler: func(name string, args []string) (string, error) {
		return "ОШИБКА: Отказано в доступе.", errors.New("exit status 1")
	}})
	status, err = unknown.Status()
	if err != nil || status.State != StateUnknown {
		t.Fatalf("ошибка доступа должна давать неизвестное состояние: %#v, %v", status, err)
	}
}

func TestWindowsInstallDoesNotOverwriteAfterQueryError(t *testing.T) {
	backend := newWindowsBackend(osFS{}, &fakeRunner{handler: func(name string, args []string) (string, error) {
		return "ОШИБКА: Отказано в доступе.", errors.New("exit status 1")
	}})
	if _, err := backend.Install(testSpec(t)); err == nil {
		t.Fatal("установка не должна продолжаться после неожиданной ошибки чтения задачи")
	}
}
