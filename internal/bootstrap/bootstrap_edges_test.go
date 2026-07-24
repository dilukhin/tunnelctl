package bootstrap

import (
	"bufio"
	"bytes"
	"strings"
	"testing"

	"tunnelctl/internal/config"
	"tunnelctl/internal/historyscan"
)

func TestImportHistoryEmptySelectionDoesNotSave(t *testing.T) {
	cfg := config.DefaultConfig()
	var out bytes.Buffer
	saves := 0
	count, err := importHistory(
		bufio.NewReader(strings.NewReader("\n")),
		&out,
		&cfg,
		"config.json",
		func(config.Config) ([]historyscan.Candidate, error) { return makeCandidates(2), nil },
		func(string, config.Config) error { saves++; return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 || saves != 0 || len(cfg.Profiles) != 0 {
		t.Fatalf("count=%d saves=%d profiles=%d", count, saves, len(cfg.Profiles))
	}
	if !strings.Contains(out.String(), "Импорт не выполнен") || !strings.Contains(out.String(), "Настройка вручную") {
		t.Fatalf("нет результата отказа: %s", out.String())
	}
}

func TestImportHistoryExactlyTenCandidatesHasNoLimitWarning(t *testing.T) {
	cfg := config.DefaultConfig()
	var out bytes.Buffer
	count, err := importHistory(
		bufio.NewReader(strings.NewReader("10\n")),
		&out,
		&cfg,
		"config.json",
		func(config.Config) ([]historyscan.Candidate, error) { return makeCandidates(10), nil },
		func(string, config.Config) error { return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || len(cfg.Profiles) != 1 || cfg.Profiles[0].Name != "profile-10" {
		t.Fatalf("count=%d profiles=%#v", count, cfg.Profiles)
	}
	text := out.String()
	if !strings.Contains(text, "Показано кандидатов: 10") {
		t.Fatalf("нет сводки: %s", text)
	}
	if strings.Contains(text, "Импорт ограничен") {
		t.Fatalf("для ровно 10 не должно быть предупреждения: %s", text)
	}
}

func TestRunScanRefusalShowsManualSetupWithExistingProfile(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Profiles = []config.Profile{{Name: "existing", User: "user", Host: "example.com", Port: 22}}
	var out bytes.Buffer
	err := run(
		bufio.NewReader(strings.NewReader("n\nn\n")),
		&out,
		"config.json",
		dependencies{
			ensure: func(string) (config.Config, bool, error) { return cfg, false, nil },
			save:   func(string, config.Config) error { return nil },
			scan: func(config.Config) ([]historyscan.Candidate, error) {
				t.Fatal("scan не должен вызываться")
				return nil, nil
			},
			shortcuts: func(*bufio.Reader, config.Config) error { return nil },
			goos:      "test",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Настройка вручную") {
		t.Fatalf("нет ручной инструкции: %s", out.String())
	}
}
