package console

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
)

func TestCopyTimestampedLinesAddsTimestampToEveryLine(t *testing.T) {
	var out bytes.Buffer
	copyTimestampedLines(&out, strings.NewReader("первая\nвторая\n"))
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("ожидались две строки: %q", out.String())
	}
	pattern := regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3} \[ИНФО\] `)
	for _, line := range lines {
		if !pattern.MatchString(line) {
			t.Fatalf("строка не получила ожидаемый префикс: %q", line)
		}
	}
}
