//go:build !windows

package autostart

import (
	"os"
	"runtime"
	"strings"
)

func currentBackend(fs FileSystem, runner CommandRunner) Backend {
	if runtime.GOOS == "android" || strings.Contains(os.Getenv("PREFIX"), "com.termux") {
		return newTermuxBackend(fs, runner)
	}
	return newLinuxBackend(fs, runner)
}
