//go:build windows

package historyscan

import (
	"path/filepath"
	"syscall"
	"testing"
	"unsafe"
)

func TestReadFarHistoryDBWithActiveWAL(t *testing.T) {
	if err := winSQLite.Load(); err != nil {
		t.Skipf("winsqlite3.dll недоступна: %v", err)
	}
	path := filepath.Join(t.TempDir(), "history.db")
	database := openTestSQLite(t, path)
	defer procSQLiteClose.Call(database)

	execTestSQL(t, database, "PRAGMA journal_mode=WAL;")
	execTestSQL(t, database, "PRAGMA wal_autocheckpoint=0;")
	execTestSQL(t, database, "CREATE TABLE history(id INTEGER PRIMARY KEY, kind INTEGER NOT NULL, key TEXT NOT NULL, type INTEGER NOT NULL, lock INTEGER NOT NULL, name TEXT NOT NULL, time INTEGER NOT NULL, guid TEXT NOT NULL, file TEXT NOT NULL, data TEXT NOT NULL);")
	execTestSQL(t, database, "INSERT INTO history VALUES (NULL,0,'',0,0,'ssh -D 1080 -N far@far.example',134130240000000000,'','','');")

	records, err := readFarHistoryDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Command != "ssh -D 1080 -N far@far.example" {
		t.Fatalf("прочитаны неверные записи: %#v", records)
	}
}

func openTestSQLite(t *testing.T, path string) uintptr {
	t.Helper()
	filename, err := syscall.BytePtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	var database uintptr
	code, _, _ := procSQLiteOpen.Call(uintptr(unsafe.Pointer(filename)), uintptr(unsafe.Pointer(&database)))
	if int32(code) != sqliteOK {
		t.Fatalf("не удалось открыть тестовую SQLite: %s", sqliteError(database))
	}
	return database
}

func execTestSQL(t *testing.T, database uintptr, query string) {
	t.Helper()
	text, err := syscall.BytePtrFromString(query)
	if err != nil {
		t.Fatal(err)
	}
	var statement uintptr
	code, _, _ := procSQLitePrepareV2.Call(
		database,
		uintptr(unsafe.Pointer(text)),
		^uintptr(0),
		uintptr(unsafe.Pointer(&statement)),
		0,
	)
	if int32(code) != sqliteOK {
		t.Fatalf("не удалось подготовить SQL %q: %s", query, sqliteError(database))
	}
	defer procSQLiteFinalize.Call(statement)
	for {
		code, _, _ = procSQLiteStep.Call(statement)
		switch int32(code) {
		case sqliteRow:
			continue
		case sqliteDone:
			return
		default:
			t.Fatalf("не удалось выполнить SQL %q: %s", query, sqliteError(database))
		}
	}
}
