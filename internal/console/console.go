package console

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

const timestampLayout = "2006-01-02 15:04:05.000"

var (
	mu             sync.RWMutex
	originalStdout = os.Stdout
)

// Timestamp возвращает локальную метку времени единого формата.
func Timestamp() string {
	return time.Now().Format(timestampLayout)
}

// WriteLevel выводит одну строку с меткой времени и уровнем.
func WriteLevel(w io.Writer, level, format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	fmt.Fprintf(w, "%s [%s] %s\n", Timestamp(), level, message)
}

// EnableStdoutTimestamps добавляет метку времени к каждой завершённой строке stdout.
// Не следует применять к интерактивным prompt-строкам и выводу генерируемых файлов.
func EnableStdoutTimestamps() (func(), error) {
	mu.Lock()
	if os.Stdout != originalStdout {
		mu.Unlock()
		return func() {}, nil
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		mu.Unlock()
		return nil, err
	}
	original := os.Stdout
	originalStdout = original
	os.Stdout = writer
	mu.Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		copyTimestampedLines(original, reader)
	}()

	var once sync.Once
	return func() {
		once.Do(func() {
			_ = writer.Close()
			<-done
			_ = reader.Close()
			mu.Lock()
			os.Stdout = original
			originalStdout = original
			mu.Unlock()
		})
	}, nil
}

// ProcessStdout возвращает настоящий stdout процесса, не внутренний pipe форматтера.
func ProcessStdout() *os.File {
	mu.RLock()
	defer mu.RUnlock()
	return originalStdout
}

func copyTimestampedLines(dst io.Writer, src io.Reader) {
	reader := bufio.NewReader(src)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			fmt.Fprintf(dst, "%s [ИНФО] %s", Timestamp(), line)
			if line[len(line)-1] != '\n' {
				fmt.Fprintln(dst)
			}
		}
		if err != nil {
			return
		}
	}
}
