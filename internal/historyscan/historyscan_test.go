package historyscan

import "testing"

func TestParseExamples(t *testing.T) {
	cases := []struct {
		cmd    string
		user   string
		host   string
		key    string
		listen string
	}{
		{
			cmd:    "ssh -i ~/.ssh/id_ed25519 -v -D 1080 -N tunneluser@45.10.164.145",
			user:   "tunneluser",
			host:   "45.10.164.145",
			key:    "~/.ssh/id_ed25519",
			listen: "127.0.0.1:1080",
		},
		{
			cmd:    "ssh -D 1080 -N -f -i ~/.ssh/id_yandex_vm dilukhin@158.160.79.19",
			user:   "dilukhin",
			host:   "158.160.79.19",
			key:    "~/.ssh/id_yandex_vm",
			listen: "127.0.0.1:1080",
		},
		{
			cmd:    "autossh -M 0 -D 127.0.0.1:1080 -p 2222 -i ~/.ssh/key user@example.com",
			user:   "user",
			host:   "example.com",
			key:    "~/.ssh/key",
			listen: "127.0.0.1:1080",
		},
	}
	for _, c := range cases {
		p, ok := ParseSSHCommand(c.cmd)
		if !ok {
			t.Fatalf("команда не распознана: %s", c.cmd)
		}
		if p.User != c.user || p.Host != c.host || p.Key != c.key || p.Listen != c.listen {
			t.Fatalf("неверный разбор\ncmd=%s\nполучено: user=%s host=%s key=%s listen=%s", c.cmd, p.User, p.Host, p.Key, p.Listen)
		}
	}
}
