//go:build windows

package historyscan

import (
	"strings"
	"syscall"
	"unsafe"
)

const (
	hkeyCurrentUser  = uintptr(0x80000001)
	hkeyLocalMachine = uintptr(0x80000002)
	keyQueryValue    = uintptr(0x0001)
	keyWOW64_64Key   = uintptr(0x0100)
	keyWOW64_32Key   = uintptr(0x0200)
	regSZ            = uint32(1)
	regExpandSZ      = uint32(2)
)

var (
	advapi32History = syscall.NewLazyDLL("advapi32.dll")
	procRegOpenKey  = advapi32History.NewProc("RegOpenKeyExW")
	procRegQuery    = advapi32History.NewProc("RegQueryValueExW")
	procRegCloseKey = advapi32History.NewProc("RegCloseKey")
)

func totalCommanderRegistryValues(valueName string) []string {
	values := []string{}
	for _, root := range []uintptr{hkeyCurrentUser, hkeyLocalMachine} {
		for _, view := range []uintptr{0, keyWOW64_64Key, keyWOW64_32Key} {
			for _, subkey := range []string{
				`Software\Ghisler\Total Commander`,
				`Software\WOW6432Node\Ghisler\Total Commander`,
			} {
				if value, ok := readRegistryString(root, subkey, valueName, view); ok {
					values = append(values, value)
				}
			}
		}
	}
	return uniqueStrings(values)
}

func readRegistryString(root uintptr, subkey, valueName string, view uintptr) (string, bool) {
	keyName, err := syscall.UTF16PtrFromString(subkey)
	if err != nil {
		return "", false
	}
	var key syscall.Handle
	code, _, _ := procRegOpenKey.Call(
		root,
		uintptr(unsafe.Pointer(keyName)),
		0,
		keyQueryValue|view,
		uintptr(unsafe.Pointer(&key)),
	)
	if code != 0 {
		return "", false
	}
	defer procRegCloseKey.Call(uintptr(key))

	name, err := syscall.UTF16PtrFromString(valueName)
	if err != nil {
		return "", false
	}
	var valueType uint32
	var size uint32
	code, _, _ = procRegQuery.Call(
		uintptr(key),
		uintptr(unsafe.Pointer(name)),
		0,
		uintptr(unsafe.Pointer(&valueType)),
		0,
		uintptr(unsafe.Pointer(&size)),
	)
	if code != 0 || size < 2 || (valueType != regSZ && valueType != regExpandSZ) {
		return "", false
	}
	buffer := make([]uint16, (size+1)/2)
	code, _, _ = procRegQuery.Call(
		uintptr(key),
		uintptr(unsafe.Pointer(name)),
		0,
		uintptr(unsafe.Pointer(&valueType)),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if code != 0 {
		return "", false
	}
	value := strings.TrimSpace(syscall.UTF16ToString(buffer))
	return value, value != ""
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if value == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
	}
	return result
}
