//go:build windows

package historyscan

import (
	"syscall"
	"unsafe"
)

var (
	kernel32Text        = syscall.NewLazyDLL("kernel32.dll")
	procMultiByteToWide = kernel32Text.NewProc("MultiByteToWideChar")
)

func decodeLegacyText(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	const cpACP = 0
	count, _, _ := procMultiByteToWide.Call(
		cpACP,
		0,
		uintptr(unsafe.Pointer(&raw[0])),
		uintptr(len(raw)),
		0,
		0,
	)
	if count == 0 {
		return string(raw)
	}
	wide := make([]uint16, int(count))
	written, _, _ := procMultiByteToWide.Call(
		cpACP,
		0,
		uintptr(unsafe.Pointer(&raw[0])),
		uintptr(len(raw)),
		uintptr(unsafe.Pointer(&wide[0])),
		count,
	)
	if written == 0 {
		return string(raw)
	}
	return syscall.UTF16ToString(wide[:int(written)])
}
