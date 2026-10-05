package versioninfo

import "testing"

func TestSetRejectsUnsafeVersion(t *testing.T) {
	oldCommit := buildCommit
	buildCommit = ""
	t.Cleanup(func() {
		buildCommit = oldCommit
		Set("unknown")
	})

	Set("1.2.3\r\nInjected: value")
	if Current() != "unknown" {
		t.Fatalf("небезопасная версия не отброшена: %q", Current())
	}
	Set(" 1.2.3 ")
	if Current() != "1.2.3.dev" {
		t.Fatalf("локальная версия не нормализована: %q", Current())
	}
}

func TestBuildIdentityUsesExactCommit(t *testing.T) {
	oldCommit := buildCommit
	buildCommit = "0123456789ABCDEF0123456789ABCDEF01234567"
	t.Cleanup(func() {
		buildCommit = oldCommit
		Set("unknown")
	})

	Set("1.2.3")
	if Current() != "1.2.3.01234567" {
		t.Fatalf("неверный идентификатор сборки: %q", Current())
	}
	if BuildCommit() != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("неверный полный SHA: %q", BuildCommit())
	}
}

func TestBuildCommitRejectsInvalidValue(t *testing.T) {
	oldCommit := buildCommit
	buildCommit = "not-a-commit"
	t.Cleanup(func() { buildCommit = oldCommit })
	if BuildCommit() != "unknown" {
		t.Fatalf("некорректный SHA не отброшен: %q", BuildCommit())
	}
}
