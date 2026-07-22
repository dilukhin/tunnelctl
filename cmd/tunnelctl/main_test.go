package main

import "testing"

func TestParseImportArgs(t *testing.T) {
	path, err := parseImportArgs([]string{"--config", `C:\Temp\tunnelctl.json`})
	if err != nil {
		t.Fatal(err)
	}
	if path != `C:\Temp\tunnelctl.json` {
		t.Fatalf("неверный путь: %q", path)
	}

	path, err = parseImportArgs([]string{"--config=/tmp/tunnelctl.json"})
	if err != nil || path != "/tmp/tunnelctl.json" {
		t.Fatalf("path=%q err=%v", path, err)
	}

	if _, err := parseImportArgs([]string{"extra"}); err == nil {
		t.Fatal("лишний аргумент должен отклоняться")
	}
}
