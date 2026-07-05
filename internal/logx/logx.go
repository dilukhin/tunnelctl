package logx

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"tunnelctl/internal/paths"
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
	logger = log.New(io.MultiWriter(f), "", 0)
	mu.Unlock()
	Info("логирование включено: %s", p)
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

func Info(format string, args ...any) {
	write("ИНФО", format, args...)
}

func Warn(format string, args ...any) {
	write("ПРЕДУПРЕЖДЕНИЕ", format, args...)
}

func Error(format string, args ...any) {
	write("ОШИБКА", format, args...)
}

func write(level, format string, args ...any) {
	mu.Lock()
	defer mu.Unlock()
	line := fmt.Sprintf(format, args...)
	line = fmt.Sprintf("%s [%s] %s", time.Now().Format("2006-01-02 15:04:05"), level, line)
	if logger != nil {
		logger.Println(line)
	}
}
