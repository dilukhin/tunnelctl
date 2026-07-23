package versioninfo

import "testing"

func TestSetRejectsUnsafeVersion(t *testing.T) {
	Set("1.2.3\r\nInjected: value")
	if Current() != "unknown" {
		t.Fatalf("небезопасная версия не отброшена: %q", Current())
	}
	Set(" 1.2.3 ")
	if Current() != "1.2.3" {
		t.Fatalf("версия не нормализована: %q", Current())
	}
}
