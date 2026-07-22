package commanddiag

import (
	"strings"
	"testing"
)

func TestSafeCommandMasksSensitiveArgumentsAndKnownValues(t *testing.T) {
	command := SafeCommand("ssh", []string{"-i", "/home/user/.ssh/id", "user@example.com", "--token=abc"}, "/home/user/.ssh/id", "user")
	for _, forbidden := range []string{"/home/user/.ssh/id", "user@example.com", "abc"} {
		if strings.Contains(command, forbidden) {
			t.Fatalf("секрет %q не замаскирован: %s", forbidden, command)
		}
	}
	if !strings.Contains(command, "<скрыто>@example.com") {
		t.Fatalf("адрес сервера потерян: %s", command)
	}
}

func TestSanitizeSkipsEmptySecret(t *testing.T) {
	if got := Sanitize("value", ""); got != "value" {
		t.Fatalf("got=%q", got)
	}
}
