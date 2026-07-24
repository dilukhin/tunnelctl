package logx

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"tunnelctl/internal/paths"
)

const (
	maxLogSize  = int64(50 << 20)
	maxArchives = 3
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
	if err := rotateIfNeeded(p, maxLogSize, time.Now()); err != nil {
		mu.Unlock()
		return fmt.Errorf("не удалось выполнить ротацию логов: %w", err)
	}
	if err := openLogLocked(p); err != nil {
		mu.Unlock()
		return err
	}
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

func Info(format string, args ...any)  { write("ИНФО", format, args...) }
func Warn(format string, args ...any)  { write("ПРЕДУПРЕЖДЕНИЕ", format, args...) }
func Error(format string, args ...any) { write("ОШИБКА", format, args...) }

func write(level, format string, args ...any) {
	mu.Lock()
	defer mu.Unlock()
	line := fmt.Sprintf(format, args...)
	line = fmt.Sprintf("%s [%s] %s", time.Now().Format("2006-01-02 15:04:05"), level, line)
	if logger == nil {
		return
	}
	logger.Println(line)
	if err := rotateActiveLogIfNeededLocked(maxLogSize, time.Now()); err != nil {
		fmt.Fprintln(os.Stderr, "tunnelctl: не удалось выполнить ротацию активного лога:", err)
	}
}

func openLogLocked(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	file = f
	logger = log.New(io.MultiWriter(f), "", 0)
	return nil
}

func rotateActiveLogIfNeededLocked(sizeLimit int64, now time.Time) error {
	if file == nil {
		return nil
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() <= sizeLimit {
		return nil
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		return err
	}
	file = nil
	logger = nil
	if err := rotateIfNeeded(path, sizeLimit, now); err != nil {
		if reopenErr := openLogLocked(path); reopenErr != nil {
			return fmt.Errorf("%v; повторное открытие лога также не удалось: %w", err, reopenErr)
		}
		return err
	}
	return openLogLocked(path)
}

func rotateIfNeeded(path string, sizeLimit int64, now time.Time) error {
	info, err := os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil && info.Size() > sizeLimit {
		archivePath, err := nextArchivePath(path, now)
		if err != nil {
			return err
		}
		if err := compressFile(path, archivePath); err != nil {
			return err
		}
		if err := os.Remove(path); err != nil {
			_ = os.Remove(archivePath)
			return err
		}
	}
	return cleanupArchives(path, now)
}

func nextArchivePath(path string, now time.Time) (string, error) {
	base := fmt.Sprintf("%s.%s", path, now.Format("20060102-150405"))
	for i := 0; i < 1000; i++ {
		candidate := base + ".gz"
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d.gz", base, i)
		}
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", errors.New("не удалось подобрать имя архива лога")
}

func compressFile(sourcePath, archivePath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	archive, err := os.OpenFile(archivePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	removeArchive := true
	defer func() {
		_ = archive.Close()
		if removeArchive {
			_ = os.Remove(archivePath)
		}
	}()
	writer := gzip.NewWriter(archive)
	if _, err := io.Copy(writer, source); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	if err := archive.Close(); err != nil {
		return err
	}
	removeArchive = false
	return nil
}

type archiveInfo struct {
	path    string
	name    string
	modTime time.Time
}

func cleanupArchives(logPath string, now time.Time) error {
	directory := filepath.Dir(logPath)
	prefix := filepath.Base(logPath) + "."
	entries, err := os.ReadDir(directory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	cutoff := now.AddDate(0, -3, 0)
	archives := make([]archiveInfo, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) || !strings.HasSuffix(entry.Name(), ".gz") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
			continue
		}
		archives = append(archives, archiveInfo{path: path, name: entry.Name(), modTime: info.ModTime()})
	}
	sort.Slice(archives, func(i, j int) bool {
		if !archives[i].modTime.Equal(archives[j].modTime) {
			return archives[i].modTime.After(archives[j].modTime)
		}
		return archives[i].name > archives[j].name
	})
	for _, archive := range archives[maxIndex(maxArchives, len(archives)):] {
		if err := os.Remove(archive.path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func maxIndex(limit, length int) int {
	if length < limit {
		return length
	}
	return limit
}
