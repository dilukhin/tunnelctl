package logx

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"tunnelctl/internal/console"
	"tunnelctl/internal/paths"
	"tunnelctl/internal/versioninfo"
)

var (
	mu     sync.Mutex
	logger *log.Logger
	file   *os.File
)

// Init включает логирование в файл. Все сообщения — на русском.
func Init() error {
	p := paths.LogPath()
	mu.Lock()
	if logger != nil {
		mu.Unlock()
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		mu.Unlock()
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		mu.Unlock()
		return err
	}
	file = f
	logger = log.New(f, "", 0)
	mu.Unlock()
	Info("запуск tunnelctl версии %s; логирование включено: %s", versioninfo.Current(), p)
	return nil
}

func Close() {
	mu.Lock()
	defer mu.Unlock()
	if file != nil {
		_ = file.Close()
		file = nil
		logger = nil
	}
}

func Info(format string, args ...any)  { write("ИНФО", format, args...) }
func Warn(format string, args ...any)  { write("ПРЕДУПРЕЖДЕНИЕ", format, args...) }
func Error(format string, args ...any) { write("ОШИБКА", format, args...) }

func write(level, format string, args ...any) {
	mu.Lock()
	defer mu.Unlock()
	line := fmt.Sprintf(format, args...)
	line = fmt.Sprintf("%s [%s] %s", console.Timestamp(), level, line)
	if logger != nil {
		logger.Println(line)
	}
}
