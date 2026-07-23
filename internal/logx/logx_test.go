package logx

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"tunnelctl/internal/paths"
	"tunnelctl/internal/versioninfo"
)

func TestLogContainsTimestampAndApplicationVersion(t *testing.T) {
	Close()
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)
	versioninfo.Set("1.2.3-test")
	if err := Init(); err != nil {
		t.Fatal(err)
	}
	Info("проверка")
	Close()

	data, err := os.ReadFile(paths.LogPath())
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "запуск tunnelctl версии 1.2.3-test") {
		t.Fatalf("версия не записана в лог: %s", text)
	}
	linePattern := regexp.MustCompile(`(?m)^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3} \[(ИНФО|ПРЕДУПРЕЖДЕНИЕ|ОШИБКА)\] `)
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for _, line := range lines {
		if !linePattern.MatchString(line) {
			t.Fatalf("строка без метки времени: %q", line)
		}
	}
	if filepath.Dir(paths.LogPath()) == "" {
		t.Fatal("некорректный путь журнала")
	}
}
