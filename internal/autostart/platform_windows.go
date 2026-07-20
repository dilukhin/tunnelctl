//go:build windows

package autostart

func currentBackend(fs FileSystem, runner CommandRunner) Backend {
	return newWindowsBackend(fs, runner)
}
