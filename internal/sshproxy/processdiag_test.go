package sshproxy

import (
	"strings"
	"testing"
)

func TestBoundedBufferKeepsLatestStderr(t *testing.T) {
	buffer := newBoundedBuffer(5)
	_, _ = buffer.Write([]byte("123"))
	_, _ = buffer.Write([]byte("4567"))
	if got := buffer.String(); got != "34567" {
		t.Fatalf("got=%q", got)
	}
}

func TestMaskArgsHidesIdentityAndUser(t *testing.T) {
	args := []string{"-D", "127.0.0.1:1080", "-i", "/secret/key", "alice@example.com"}
	masked := strings.Join(maskArgs(args), " ")
	for _, forbidden := range []string{"/secret/key", "alice@example.com"} {
		if strings.Contains(masked, forbidden) {
			t.Fatalf("не замаскировано %q: %s", forbidden, masked)
		}
	}
	if !strings.Contains(masked, "<пользователь>@example.com") {
		t.Fatalf("хост должен остаться видимым: %s", masked)
	}
}

func TestUniqueNonEmpty(t *testing.T) {
	got := uniqueNonEmpty([]string{"", "a", "a", " b "})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got=%v", got)
	}
}
