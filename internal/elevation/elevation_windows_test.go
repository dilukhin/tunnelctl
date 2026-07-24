//go:build windows

package elevation

import "testing"

func TestMaybeRelaunchDoesNotElevateSupportedWindowsCommands(t *testing.T) {
	tests := [][]string{
		{"autostart", "install", "auto"},
		{"autostart", "install", "auto", "--dry-run"},
		{"autostart", "remove"},
		{"autostart", "install", "--help"},
	}
	for _, args := range tests {
		handled, exitCode, err := MaybeRelaunch(args)
		if err != nil {
			t.Fatalf("MaybeRelaunch(%q) вернул ошибку: %v", args, err)
		}
		if handled || exitCode != 0 {
			t.Fatalf("MaybeRelaunch(%q) handled=%v exitCode=%d", args, handled, exitCode)
		}
	}
}
