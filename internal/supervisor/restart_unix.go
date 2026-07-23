//go:build !windows

package supervisor

import (
	"fmt"
	"os"
	"syscall"
)

func platformReplaceCurrentProcess(executable string, args []string) error {
	argv := append([]string{executable}, args...)
	return syscall.Exec(executable, argv, os.Environ())
}

func platformValidateRestartExecutable(info os.FileInfo) error {
	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("новый бинарник не имеет права на выполнение")
	}
	return nil
}
