package historyscan

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"

	"tunnelctl/internal/config"
)

func TestParseSSHCommandPreservesWindowsPaths(t *testing.T) {
	profile, ok := ParseSSHCommand(`C:\Windows\System32\OpenSSH\ssh.exe -i "C:\Users\d.ilyhin\.ssh\id_ed25519" -D 1080 -N user@example.com`)
	if !ok {
		t.Fatal("Windows-команда не распознана")
	}
	if profile.Key != `C:\Users\d.ilyhin\.ssh\id_ed25519` {
		t.Fatalf("путь к ключу повреждён: %q", profile.Key)
	}
	if profile.Listen != "127.0.0.1:1080" || profile.User != "user" || profile.Host != "example.com" {
		t.Fatalf("неверный профиль: %#v", profile)
	}
}

func TestScanFarMenuIncludingSubmenu(t *testing.T) {
	path := filepath.Join(t.TempDir(), "FarMenu.ini")
	content := "T: Tunnel\n    ssh.exe -D 1080 -N user@first.example\nS: Submenu\n{\nX: Other\n    autossh -M 0 -D 2080 user@second.example\n}\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	dedup := map[string]*Candidate{}
	cfg := config.DefaultConfig()
	if err := scanFarMenu(path, cfg, usedNames(cfg), dedup); err != nil {
		t.Fatal(err)
	}
	if len(dedup) != 2 {
		t.Fatalf("найдено %d туннелей, ожидалось 2", len(dedup))
	}
	assertCandidateHost(t, dedup, "first.example")
	assertCandidateHost(t, dedup, "second.example")
}

func TestScanTotalCommanderHistoryAndStartMenu(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wincmd.ini")
	content := `[Command line history]
0=ssh -D 1080 -N history@history.example
1=echo ignored
[user]
menu1=Tunnel
cmd1=ssh.exe
param1=-D 2080 -N menu@menu.example
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	dedup := map[string]*Candidate{}
	cfg := config.DefaultConfig()
	if err := scanTotalCommanderINI(path, cfg, usedNames(cfg), dedup); err != nil {
		t.Fatal(err)
	}
	if len(dedup) != 2 {
		t.Fatalf("найдено %d туннелей, ожидалось 2", len(dedup))
	}
	assertCandidateHost(t, dedup, "history.example")
	assertCandidateHost(t, dedup, "menu.example")
}

func TestScanTotalCommanderUserCmd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usercmd.ini")
	content := `[em_tunnel]
button=
cmd=ssh.exe
param=-i C:\keys\id_ed25519 -D 3080 -N custom@custom.example
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	dedup := map[string]*Candidate{}
	cfg := config.DefaultConfig()
	if err := scanTotalCommanderUserCmd(path, cfg, usedNames(cfg), dedup); err != nil {
		t.Fatal(err)
	}
	if len(dedup) != 1 {
		t.Fatalf("найдено %d туннелей, ожидался 1", len(dedup))
	}
	for _, candidate := range dedup {
		if candidate.Profile.Key != `C:\keys\id_ed25519` || candidate.Profile.Listen != "127.0.0.1:3080" {
			t.Fatalf("неверный профиль: %#v", candidate.Profile)
		}
	}
}

func TestRepeatedImportSkipsExistingButKeepsDifferentListen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "FarMenu.ini")
	content := "A: Existing\n    ssh -D 1080 -N user@example.com\nB: New port\n    ssh -D 2080 -N user@example.com\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	cfg.Profiles = append(cfg.Profiles, config.Profile{
		Name: "existing", User: "user", Host: "example.com", Port: 22, Listen: "127.0.0.1:1080",
	})
	dedup := map[string]*Candidate{}
	if err := scanFarMenu(path, cfg, usedNames(cfg), dedup); err != nil {
		t.Fatal(err)
	}
	if len(dedup) != 1 {
		t.Fatalf("после фильтрации найдено %d туннелей, ожидался 1", len(dedup))
	}
	for _, candidate := range dedup {
		if candidate.Profile.Listen != "127.0.0.1:2080" {
			t.Fatalf("оставлен неверный кандидат: %#v", candidate.Profile)
		}
	}
}

func TestReadINIUTF16LE(t *testing.T) {
	text := "[Command line history]\r\n0=ssh -D 1080 user@example.com\r\n"
	units := utf16.Encode([]rune(text))
	raw := make([]byte, 2+len(units)*2)
	raw[0], raw[1] = 0xff, 0xfe
	for index, unit := range units {
		binary.LittleEndian.PutUint16(raw[2+index*2:], unit)
	}
	path := filepath.Join(t.TempDir(), "wincmd.ini")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := readINI(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Value != "ssh -D 1080 user@example.com" {
		t.Fatalf("неверный разбор UTF-16: %#v", entries)
	}
}

func TestExpandWindowsEnvironmentCaseInsensitive(t *testing.T) {
	got := expandWindowsEnvironment(`%farhome%\Profile`, map[string]string{"FARHOME": `C:\Far`})
	if got != `C:\Far\Profile` {
		t.Fatalf("получено %q", got)
	}
}

func assertCandidateHost(t *testing.T, candidates map[string]*Candidate, host string) {
	t.Helper()
	for _, candidate := range candidates {
		if candidate.Profile.Host == host {
			return
		}
	}
	t.Fatalf("кандидат %s не найден: %#v", host, candidates)
}
