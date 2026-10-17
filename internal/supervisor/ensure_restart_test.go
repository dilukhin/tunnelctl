package supervisor

import "testing"

func TestRestartPreservesEnsureMode(t *testing.T) {
	for _, ensure := range []bool{false, true} {
		args := restartArgs(Options{Target: "auto", ConfigPath: "/config/test.json", Watch: true, Ensure: ensure})
		want := "connect"
		if ensure {
			want = "ensure"
		}
		if args[0] != want || args[1] != "auto" || args[2] != "--watch" {
			t.Fatalf("режим перезапуска потерян: %v", args)
		}
	}
}
