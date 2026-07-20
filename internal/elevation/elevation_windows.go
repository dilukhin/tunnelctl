//go:build windows

package elevation

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

const (
	tokenQuery                          = 0x0008
	tokenElevationClass                 = 20
	seeMaskNoCloseProcess               = 0x00000040
	swShowNormal                        = 1
	infinite                            = 0xffffffff
	errorCancelled        syscall.Errno = 1223
)

var (
	advapi32                 = syscall.NewLazyDLL("advapi32.dll")
	procOpenProcessToken     = advapi32.NewProc("OpenProcessToken")
	procGetTokenInformation = advapi32.NewProc("GetTokenInformation")
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	procGetCurrentProcess    = kernel32.NewProc("GetCurrentProcess")
	procWaitForSingleObject  = kernel32.NewProc("WaitForSingleObject")
	procGetExitCodeProcess   = kernel32.NewProc("GetExitCodeProcess")
	procCloseHandle          = kernel32.NewProc("CloseHandle")
	shell32                  = syscall.NewLazyDLL("shell32.dll")
	procShellExecuteExW      = shell32.NewProc("ShellExecuteExW")
)

type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         uintptr
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     uintptr
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    uintptr
	dwHotKey     uint32
	hIcon        uintptr
	hProcess     uintptr
}

func MaybeRelaunch(args []string) (bool, int, error) {
	if !needsElevation(args) {
		return false, 0, nil
	}
	elevated, err := isElevated()
	if err != nil {
		return false, 0, fmt.Errorf("не удалось определить уровень привилегий: %w", err)
	}
	if elevated {
		return false, 0, nil
	}

	executable, err := os.Executable()
	if err != nil {
		return false, 0, fmt.Errorf("не удалось определить путь к tunnelctl: %w", err)
	}
	workingDir, err := os.Getwd()
	if err != nil {
		return false, 0, fmt.Errorf("не удалось определить рабочий каталог: %w", err)
	}
	exitCode, err := runElevated(executable, args, workingDir)
	if err != nil {
		if errors.Is(err, errorCancelled) {
			return false, 0, errors.New("повышение привилегий отменено пользователем")
		}
		return false, 0, err
	}
	return true, int(exitCode), nil
}

func needsElevation(args []string) bool {
	if len(args) < 2 || args[0] != "autostart" {
		return false
	}
	return args[1] == "install" || args[1] == "remove" || args[1] == "uninstall"
}

func isElevated() (bool, error) {
	process, _, _ := procGetCurrentProcess.Call()
	var token syscall.Handle
	r1, _, callErr := procOpenProcessToken.Call(process, tokenQuery, uintptr(unsafe.Pointer(&token)))
	if r1 == 0 {
		return false, callErr
	}
	defer procCloseHandle.Call(uintptr(token))

	var elevation uint32
	var returned uint32
	r1, _, callErr = procGetTokenInformation.Call(
		uintptr(token),
		tokenElevationClass,
		uintptr(unsafe.Pointer(&elevation)),
		unsafe.Sizeof(elevation),
		uintptr(unsafe.Pointer(&returned)),
	)
	if r1 == 0 {
		return false, callErr
	}
	return elevation != 0, nil
}

func runElevated(executable string, args []string, workingDir string) (uint32, error) {
	verb, _ := syscall.UTF16PtrFromString("runas")
	file, err := syscall.UTF16PtrFromString(executable)
	if err != nil {
		return 0, err
	}
	parameters, err := syscall.UTF16PtrFromString(quoteCommandLine(args))
	if err != nil {
		return 0, err
	}
	directory, err := syscall.UTF16PtrFromString(workingDir)
	if err != nil {
		return 0, err
	}

	info := shellExecuteInfo{
		fMask:        seeMaskNoCloseProcess,
		lpVerb:       verb,
		lpFile:       file,
		lpParameters: parameters,
		lpDirectory:  directory,
		nShow:        swShowNormal,
	}
	info.cbSize = uint32(unsafe.Sizeof(info))
	r1, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	if r1 == 0 {
		return 0, callErr
	}
	if info.hProcess == 0 {
		return 0, errors.New("ShellExecuteExW не вернул дескриптор повышенного процесса")
	}
	defer procCloseHandle.Call(info.hProcess)

	procWaitForSingleObject.Call(info.hProcess, infinite)
	var exitCode uint32
	r1, _, callErr = procGetExitCodeProcess.Call(info.hProcess, uintptr(unsafe.Pointer(&exitCode)))
	if r1 == 0 {
		return 0, callErr
	}
	return exitCode, nil
}

func quoteCommandLine(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = quoteArg(arg)
	}
	return strings.Join(quoted, " ")
}

func quoteArg(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t\n\v\"") {
		return value
	}
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for _, r := range value {
		switch r {
		case '\\':
			slashes++
		case '"':
			b.WriteString(strings.Repeat("\\", slashes*2+1))
			b.WriteRune(r)
			slashes = 0
		default:
			b.WriteString(strings.Repeat("\\", slashes))
			slashes = 0
			b.WriteRune(r)
		}
	}
	b.WriteString(strings.Repeat("\\", slashes*2))
	b.WriteByte('"')
	return b.String()
}
