package historyscan

import "testing"

func TestParseExamples(t *testing.T) {
	cases := []struct {
		cmd    string
		user   string
		host   string
		key    string
		listen string
		port   int
	}{
		{
			cmd:    "ssh -i ~/.ssh/id_ed25519 -v -D 1080 -N tunneluser@198.51.100.20",
			user:   "tunneluser",
			host:   "198.51.100.20",
			key:    "~/.ssh/id_ed25519",
			listen: "127.0.0.1:1080",
			port:   22,
		},
		{
			cmd:    "ssh -D 1080 -N -f -i ~/.ssh/id_yandex_vm user@192.0.2.10",
			user:   "user",
			host:   "192.0.2.10",
			key:    "~/.ssh/id_yandex_vm",
			listen: "127.0.0.1:1080",
			port:   22,
		},
		{
			cmd:    "autossh -M 0 -D 127.0.0.1:1080 -p 2222 -i ~/.ssh/key user@example.com",
			user:   "user",
			host:   "example.com",
			key:    "~/.ssh/key",
			listen: "127.0.0.1:1080",
			port:   2222,
		},
		{
			cmd:    "ssh -D127.0.0.1:2080 -i '~/.ssh/key with spaces' user@example.org",
			user:   "user",
			host:   "example.org",
			key:    "~/.ssh/key with spaces",
			listen: "127.0.0.1:2080",
			port:   22,
		},
		{
			cmd:    "ssh -o User=alice -o HostName=host.example -o Port=2200 -o IdentityFile=~/.ssh/custom -D :1080 bastion-alias",
			user:   "alice",
			host:   "host.example",
			key:    "~/.ssh/custom",
			listen: "127.0.0.1:1080",
			port:   2200,
		},
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
