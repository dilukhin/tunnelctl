package supervisor

import (
	"testing"

	"tunnelctl/internal/versioninfo"
)

func TestStateUsesApplicationBuildIdentity(t *testing.T) {
	versioninfo.Set("1.2.3")
	state := newStateStore("/tmp/config.json", "first", "profile").snapshot()
	if state.ApplicationVersion != versioninfo.Current() {
		t.Fatalf("state содержит %q вместо %q", state.ApplicationVersion, versioninfo.Current())
	}
}
