package historyscan

import (
	"testing"
	"time"

	"tunnelctl/internal/config"
)

func TestParseExamples(t *testing.T) {
	cases := []struct {
		cmd    string
		user   string
		host   string
		key    string
		listen string
		port   int
	}{
		{cmd: "ssh -i ~/.ssh/id_ed25519 -v -D 1080 -N tunneluser@198.51.100.20", user: "tunneluser", host: "198.51.100.20", key: "~/.ssh/id_ed25519", listen: "127.0.0.1:1080", port: 22},
		{cmd: "ssh -D 1080 -N -f -i ~/.ssh/id_yandex_vm user@192.0.2.10", user: "user", host: "192.0.2.10", key: "~/.ssh/id_yandex_vm", listen: "127.0.0.1:1080", port: 22},
		{cmd: "autossh -M 0 -D 127.0.0.1:1080 -p 2222 -i ~/.ssh/key user@example.com", user: "user", host: "example.com", key: "~/.ssh/key", listen: "127.0.0.1:1080", port: 2222},
		{cmd: "ssh -D127.0.0.1:2080 -i '~/.ssh/key with spaces' user@example.org", user: "user", host: "example.org", key: "~/.ssh/key with spaces", listen: "127.0.0.1:2080", port: 22},
		{cmd: "ssh -o User=alice -o HostName=host.example -o Port=2200 -o IdentityFile=~/.ssh/custom -D :1080 bastion-alias", user: "alice", host: "host.example", key: "~/.ssh/custom", listen: "127.0.0.1:1080", port: 2200},
	}
	for _, c := range cases {
		p, ok := ParseSSHCommand(c.cmd)
		if !ok {
			t.Fatalf("команда не распознана: %s", c.cmd)
		}
		if p.User != c.user || p.Host != c.host || p.Key != c.key || p.Listen != c.listen || p.Port != c.port {
			t.Fatalf("неверный разбор\ncmd=%s\nполучено: user=%s host=%s key=%s listen=%s port=%d", c.cmd, p.User, p.Host, p.Key, p.Listen, p.Port)
		}
	}
}

func TestParseRejectsNonDynamicSSH(t *testing.T) {
	if _, ok := ParseSSHCommand("ssh user@example.com"); ok {
		t.Fatal("команда без -D не должна импортироваться")
	}
}

func TestSortCandidatesNewestFirstAndStable(t *testing.T) {
	older := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)
	candidates := []Candidate{
		{Source: "b", Line: 5, SourceModified: older, Profile: config.Profile{User: "u", Host: "old", Port: 22, Listen: "127.0.0.1:1080"}},
		{Source: "a", Line: 2, SourceModified: newer, Profile: config.Profile{User: "u", Host: "new-2", Port: 22, Listen: "127.0.0.1:1080"}},
		{Source: "a", Line: 9, SourceModified: newer, Profile: config.Profile{User: "u", Host: "new-9", Port: 22, Listen: "127.0.0.1:1080"}},
		{Source: "b", Line: 1, SourceModified: newer, Profile: config.Profile{User: "u", Host: "new-b", Port: 22, Listen: "127.0.0.1:1080"}},
	}
	sortCandidates(candidates)
	got := []string{candidates[0].Profile.Host, candidates[1].Profile.Host, candidates[2].Profile.Host, candidates[3].Profile.Host}
	want := []string{"new-9", "new-2", "new-b", "old"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got=%v want=%v", got, want)
		}
	}
}

func TestOccurrenceNewerPrefersHigherLineInSameFile(t *testing.T) {
	when := time.Now()
	old := Candidate{Source: "history", Line: 10, SourceModified: when}
	if !occurrenceNewer(when, "history", 11, old) {
		t.Fatal("более новая строка должна побеждать")
	}
	if occurrenceNewer(when, "history", 9, old) {
		t.Fatal("более старая строка не должна побеждать")
	}
}
