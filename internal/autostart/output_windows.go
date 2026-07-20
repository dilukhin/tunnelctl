//go:build windows

package autostart

import (
	"strings"
	"syscall"
	"unicode/utf8"
	"unsafe"
)

var (
	kernel32Output          = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleOutputCP  = kernel32Output.NewProc("GetConsoleOutputCP")
	procGetOEMCP            = kernel32Output.NewProc("GetOEMCP")
	procMultiByteToWideChar = kernel32Output.NewProc("MultiByteToWideChar")
)

func decodeCommandOutput(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	if utf8.Valid(raw) {
		return strings.TrimSpace(string(raw))
	}
	codePage, _, _ := procGetConsoleOutputCP.Call()
	if codePage == 0 {
		codePage, _, _ = procGetOEMCP.Call()
	}
	decoded, ok := decodeWindowsCodePage(raw, uint32(codePage))
	if !ok {
		return strings.TrimSpace(string(raw))
	}
	return strings.TrimSpace(decoded)
}

func decodeWindowsCodePage(raw []byte, codePage uint32) (string, bool) {
	if len(raw) == 0 {
		return "", true
	}
	if codePage == 0 {
		return "", false
	}
	count, _, _ := procMultiByteToWideChar.Call(
		uintptr(codePage),
		0,
		uintptr(unsafe.Pointer(&raw[0])),
		uintptr(len(raw)),
		0,
		0,
	)
	if count == 0 {
		return "", false
	}
	wide := make([]uint16, int(count))
	written, _, _ := procMultiByteToWideChar.Call(
		uintptr(codePage),
		0,
		uintptr(unsafe.Pointer(&raw[0])),
		uintptr(len(raw)),
		uintptr(unsafe.Pointer(&wide[0])),
		count,
	)
	if written == 0 {
		return "", false
	}
	return syscall.UTF16ToString(wide[:int(written)]), true
}
