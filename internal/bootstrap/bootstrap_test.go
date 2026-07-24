package bootstrap

import (
	"bufio"
	"bytes"
	"errors"
	"strconv"
	"strings"
	"testing"

	"tunnelctl/internal/config"
	"tunnelctl/internal/historyscan"
)

func TestParseSelection(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		max      int
		want     []int
		declined bool
		wantErr  bool
	}{
		{name: "empty", value: "", max: 3, declined: true},
		{name: "explicit refusal", value: "нет", max: 3, declined: true},
		{name: "multiple", value: "1,3,5", max: 5, want: []int{0, 2, 4}},
		{name: "spaces", value: " 1, 3 , 5 ", max: 5, want: []int{0, 2, 4}},
		{name: "duplicate", value: "1,1", max: 3, wantErr: true},
		{name: "bad format", value: "1,a", max: 3, wantErr: true},
		{name: "bad range", value: "4", max: 3, wantErr: true},
		{name: "empty item", value: "1,,2", max: 3, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, declined, err := parseSelection(tt.value, tt.max)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v", err)
			}
			if declined != tt.declined {
				t.Fatalf("declined=%v", declined)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got=%v want=%v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("got=%v want=%v", got, tt.want)
				}
			}
		})
	}
}

func TestImportHistorySavesSelectedProfilesOnce(t *testing.T) {
	cfg := config.DefaultConfig()
	candidates := makeCandidates(3)
	var out bytes.Buffer
	saves := 0
	var saved config.Config
	count, err := importHistory(
		bufio.NewReader(strings.NewReader("1, 3\n")),
		&out,
		&cfg,
		"/tmp/tunnelctl.json",
		func(config.Config) ([]historyscan.Candidate, error) { return candidates, nil },
		func(_ string, value config.Config) error { saves++; saved = value; return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("count=%d", count)
	}
	if saves != 1 {
		t.Fatalf("save вызван %d раз", saves)
	}
	if len(saved.Profiles) != 2 || saved.Profiles[0].Name != "profile-1" || saved.Profiles[1].Name != "profile-3" {
		t.Fatalf("saved=%#v", saved.Profiles)
	}
	if !strings.Contains(out.String(), "Импортировано профилей: 2") {
		t.Fatalf("нет итоговой сводки: %s", out.String())
	}
}

func TestImportHistoryRetriesInvalidSelectionWithoutPartialSave(t *testing.T) {
	cfg := config.DefaultConfig()
	var out bytes.Buffer
	saves := 0
	count, err := importHistory(
		bufio.NewReader(strings.NewReader("1,1\n2\n")),
		&out,
		&cfg,
		"config.json",
		func(config.Config) ([]historyscan.Candidate, error) { return makeCandidates(2), nil },
		func(_ string, value config.Config) error { saves++; return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || saves != 1 {
		t.Fatalf("count=%d saves=%d", count, saves)
	}
	if len(cfg.Profiles) != 1 || cfg.Profiles[0].Name != "profile-2" {
		t.Fatalf("cfg=%#v", cfg.Profiles)
	}
	if !strings.Contains(out.String(), "указан повторно") {
		t.Fatalf("не показана ошибка: %s", out.String())
	}
}

func TestImportHistoryLimitsCandidatesToTen(t *testing.T) {
	cfg := config.DefaultConfig()
	var out bytes.Buffer
	count, err := importHistory(
		bufio.NewReader(strings.NewReader("10\n")),
		&out,
		&cfg,
		"config.json",
		func(config.Config) ([]historyscan.Candidate, error) { return makeCandidates(12), nil },
		func(_ string, value config.Config) error { return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || len(cfg.Profiles) != 1 || cfg.Profiles[0].Name != "profile-10" {
		t.Fatalf("cfg=%#v", cfg.Profiles)
	}
	text := out.String()
	if !strings.Contains(text, "Показано кандидатов: 10") || !strings.Contains(text, "остальные 2 не показаны") {
		t.Fatalf("нет ограничения: %s", text)
	}
	if strings.Contains(text, "profile-11") {
		t.Fatalf("показан 11-й кандидат: %s", text)
	}
}

func TestImportHistoryNoCandidatesShowsManualSetup(t *testing.T) {
	cfg := config.DefaultConfig()
	var out bytes.Buffer
	count, err := importHistory(
		bufio.NewReader(strings.NewReader("")), &out, &cfg, "config.json",
		func(config.Config) ([]historyscan.Candidate, error) { return nil, nil },
		func(string, config.Config) error { t.Fatal("save не должен вызываться"); return nil },
	)
	if err != nil || count != 0 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	if !strings.Contains(out.String(), "Настройка вручную") || !strings.Contains(out.String(), "tunnelctl doctor") {
		t.Fatalf("нет инструкции: %s", out.String())
	}
}

func TestRunDoesNotOfferShortcutsForEmptyConfig(t *testing.T) {
	var out bytes.Buffer
	shortcutCalls := 0
	err := run(
		bufio.NewReader(strings.NewReader("n\n")), &out, "config.json",
		dependencies{
			ensure: func(string) (config.Config, bool, error) { return config.DefaultConfig(), true, nil },
			save:   func(string, config.Config) error { return nil },
			scan: func(config.Config) ([]historyscan.Candidate, error) {
				return nil, errors.New("scan не должен вызываться")
			},
			shortcuts: func(*bufio.Reader, config.Config) error { shortcutCalls++; return nil },
			goos:      "test",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if shortcutCalls != 0 {
		t.Fatalf("ярлыки вызваны %d раз", shortcutCalls)
	}
	if strings.Contains(out.String(), "Создать ярлыки") {
		t.Fatalf("предложены ярлыки: %s", out.String())
	}
	if !strings.Contains(out.String(), "Настройка вручную") {
		t.Fatalf("нет ручной инструкции: %s", out.String())
	}
}

func makeCandidates(n int) []historyscan.Candidate {
	out := make([]historyscan.Candidate, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, historyscan.Candidate{Profile: config.Profile{
			Name: "profile-" + strconv.Itoa(i), Alias: "alias-" + strconv.Itoa(i), User: "user", Host: "host" + strconv.Itoa(i), Port: 22, Listen: "127.0.0.1:1080",
		}})
	}
	return out
}
