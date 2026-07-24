package supervisor

import (
	"fmt"
	"os"
	"path/filepath"

	"tunnelctl/internal/logx"
)

type restartRequestedError struct {
	executable string
}

func (e *restartRequestedError) Error() string {
	return "запрошен перезапуск tunnelctl"
}

var replaceProcess = platformReplaceCurrentProcess

func validateRestartExecutable(candidate string) (string, error) {
	if candidate == "" {
		var err error
		candidate, err = os.Executable()
		if err != nil {
			return "", fmt.Errorf("не удалось определить текущий бинарник: %w", err)
		}
	}
	absolute, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("не удалось определить абсолютный путь нового бинарника: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", fmt.Errorf("новый бинарник недоступен: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("путь нового бинарника не указывает на обычный файл")
	}
	if err := platformValidateRestartExecutable(info); err != nil {
		return "", err
	}
	return absolute, nil
}

func restartArgs(opts Options) []string {
	args := []string{"connect", opts.Target}
	if opts.Watch {
		args = append(args, "--watch")
	} else {
		args = append(args, "--no-watch")
	}
	args = append(args, "--config", opts.ConfigPath)
	return args
}

func replaceCurrentProcess(executable string, opts Options) error {
	args := restartArgs(opts)
	logx.Info("перезапуск tunnelctl; цель: %s", opts.Target)
	if err := replaceProcess(executable, args); err != nil {
		return fmt.Errorf("не удалось запустить новый экземпляр tunnelctl: %w", err)
	}
	return nil
}
