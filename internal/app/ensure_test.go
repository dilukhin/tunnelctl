package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureRejectsNoWatchBeforeConfigLoad(t *testing.T) {
	if err := cmdEnsure([]string{"auto", "--no-watch"}); err == nil || err.Error() != "ensure требует --watch" {
		t.Fatalf("ожидался отказ --no-watch: %v", err)
	}
}

func TestEnsureHelpDoesNotCreateState(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("LOCALAPPDATA", dir)
	if err := Run([]string{"ensure", "--help"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("справка создала состояние: %v", err)
	}
}
