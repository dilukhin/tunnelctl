//go:build windows

package supervisor

import (
	"os"
	"os/exec"
	"syscall"

	"tunnelctl/internal/console"
)

const createBreakawayFromJob = 0x01000000

func platformReplaceCurrentProcess(executable string, args []string) error {
	// Планировщик заданий может держать tunnelctl в Job Object. Сначала просим
	// разрешить новому экземпляру пережить завершение старого процесса.
	if err := startReplacementWindows(executable, args, syscall.CREATE_NEW_PROCESS_GROUP|createBreakawayFromJob); err == nil {
		return nil
	}
	// В интерактивном запуске или в Job без права breakaway обычный дочерний
	// процесс остаётся рабочим вариантом; вызывающая команда проверит health-check.
	return startReplacementWindows(executable, args, syscall.CREATE_NEW_PROCESS_GROUP)
}

func startReplacementWindows(executable string, args []string, flags uint32) error {
	cmd := exec.Command(executable, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = console.ProcessStdout()
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func platformValidateRestartExecutable(os.FileInfo) error { return nil }
