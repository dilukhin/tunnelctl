//go:build windows

package historyscan

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

const (
	sqliteOK   = 0
	sqliteRow  = 100
	sqliteDone = 101
)

var (
	winSQLite             = syscall.NewLazyDLL("winsqlite3.dll")
	procSQLiteOpen        = winSQLite.NewProc("winsqlite3_open")
	procSQLiteClose       = winSQLite.NewProc("winsqlite3_close")
	procSQLitePrepareV2   = winSQLite.NewProc("winsqlite3_prepare_v2")
	procSQLiteStep        = winSQLite.NewProc("winsqlite3_step")
	procSQLiteFinalize    = winSQLite.NewProc("winsqlite3_finalize")
	procSQLiteColumnText  = winSQLite.NewProc("winsqlite3_column_text")
	procSQLiteColumnBytes = winSQLite.NewProc("winsqlite3_column_bytes")
	procSQLiteColumnInt64 = winSQLite.NewProc("winsqlite3_column_int64")
	procSQLiteErrmsg      = winSQLite.NewProc("winsqlite3_errmsg")
)

type farHistoryRecord struct {
	Command string
	Time    int64
}

func readFarHistoryDB(path string) ([]farHistoryRecord, error) {
	copyPath, cleanup, err := copySQLiteSnapshot(path)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	if err := winSQLite.Load(); err != nil {
		return nil, fmt.Errorf("winsqlite3.dll недоступна: %w", err)
	}
	filename, err := syscall.BytePtrFromString(copyPath)
	if err != nil {
		return nil, err
	}
	var database uintptr
	code, _, _ := procSQLiteOpen.Call(uintptr(unsafe.Pointer(filename)), uintptr(unsafe.Pointer(&database)))
	if int32(code) != sqliteOK {
		message := sqliteError(database)
		if database != 0 {
			procSQLiteClose.Call(database)
		}
		return nil, fmt.Errorf("не удалось открыть копию Far history.db: %s", message)
	}
	defer procSQLiteClose.Call(database)

	query, _ := syscall.BytePtrFromString("SELECT name, time FROM history WHERE kind=0 AND key='' ORDER BY time ASC;")
	var statement uintptr
	code, _, _ = procSQLitePrepareV2.Call(
		database,
		uintptr(unsafe.Pointer(query)),
		^uintptr(0),
		uintptr(unsafe.Pointer(&statement)),
		0,
	)
	if int32(code) != sqliteOK {
		return nil, fmt.Errorf("не удалось подготовить запрос Far history.db: %s", sqliteError(database))
	}
	defer procSQLiteFinalize.Call(statement)

	var records []farHistoryRecord
	for {
		code, _, _ = procSQLiteStep.Call(statement)
		switch int32(code) {
		case sqliteRow:
			records = append(records, farHistoryRecord{
				Command: sqliteColumnString(statement, 0),
				Time:    sqliteColumnInteger(statement, 1),
			})
		case sqliteDone:
			return records, nil
		default:
			return nil, fmt.Errorf("ошибка чтения Far history.db: %s", sqliteError(database))
		}
	}
}

func copySQLiteSnapshot(path string) (string, func(), error) {
	tempDir, err := os.MkdirTemp("", "tunnelctl-far-history-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(tempDir) }
	base := filepath.Base(path)
	copyPath := filepath.Join(tempDir, base)
	if err := copyFile(path, copyPath); err != nil {
		cleanup()
		return "", func() {}, err
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		source := path + suffix
		if _, err := os.Stat(source); err != nil {
			continue
		}
		if err := copyFile(source, copyPath+suffix); err != nil {
			cleanup()
			return "", func() {}, err
		}
	}
	return copyPath, cleanup, nil
}

func copyFile(source, destination string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(destination, data, 0o600)
}

func sqliteColumnString(statement uintptr, column int) string {
	pointer, _, _ := procSQLiteColumnText.Call(statement, uintptr(column))
	if pointer == 0 {
		return ""
	}
	length, _, _ := procSQLiteColumnBytes.Call(statement, uintptr(column))
	if length == 0 {
		return ""
	}
	return string(unsafe.Slice((*byte)(unsafe.Pointer(pointer)), int(length)))
}

func sqliteColumnInteger(statement uintptr, column int) int64 {
	value, _, _ := procSQLiteColumnInt64.Call(statement, uintptr(column))
	return int64(value)
}

func sqliteError(database uintptr) string {
	if database == 0 {
		return "неизвестная ошибка SQLite"
	}
	pointer, _, _ := procSQLiteErrmsg.Call(database)
	if pointer == 0 {
		return "неизвестная ошибка SQLite"
	}
	const maxMessage = 4096
	bytes := unsafe.Slice((*byte)(unsafe.Pointer(pointer)), maxMessage)
	for index, value := range bytes {
		if value == 0 {
			return string(bytes[:index])
		}
	}
	return string(bytes)
}

func isFarSQLiteUnavailable(err error) bool {
	return errors.Is(err, syscall.ERROR_MOD_NOT_FOUND) || errors.Is(err, syscall.ERROR_PROC_NOT_FOUND)
}
