package logx

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRotateIfNeededCompressesOversizedLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tunnelctl.log")
	content := strings.Repeat("diagnostic line\n", 20)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 22, 10, 30, 0, 0, time.UTC)
	if err := rotateIfNeeded(path, 10, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("основной лог не удалён: %v", err)
	}
	archives, err := filepath.Glob(path + ".*.gz")
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 1 {
		t.Fatalf("архивов: %v", archives)
	}
	file, err := os.Open(archives[0])
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Fatalf("архив содержит неверные данные")
	}
}

func TestCleanupArchivesRemovesOldAndKeepsThreeNewest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tunnelctl.log")
	now := time.Date(2026, 7, 22, 10, 30, 0, 0, time.UTC)
	createArchive := func(name string, mod time.Time) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
	createArchive("tunnelctl.log.old.gz", now.AddDate(0, -4, 0))
	for i := 1; i <= 5; i++ {
		createArchive("tunnelctl.log.new-"+string(rune('0'+i))+".gz", now.Add(-time.Duration(i)*time.Hour))
	}
	createArchive("other.log.1.gz", now)
	if err := cleanupArchives(path, now); err != nil {
		t.Fatal(err)
	}
	archives, err := filepath.Glob(path + ".*.gz")
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 3 {
		t.Fatalf("ожидалось 3 архива, получено %v", archives)
	}
	if _, err := os.Stat(filepath.Join(dir, "other.log.1.gz")); err != nil {
		t.Fatalf("чужой архив удалён: %v", err)
	}
}

func TestRotateDoesNotTriggerAtExactLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tunnelctl.log")
	if err := os.WriteFile(path, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := rotateIfNeeded(path, 5, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("лог на границе не должен ротироваться: %v", err)
	}
}
