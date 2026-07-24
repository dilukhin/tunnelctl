package historyscan

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"os"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

type iniEntry struct {
	Section string
	Key     string
	Value   string
	Line    int
}

func readINI(path string) ([]iniEntry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := decodeText(raw)
	return parseINI(text), nil
}

func parseINI(text string) []iniEntry {
	var entries []iniEntry
	section := ""
	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		entries = append(entries, iniEntry{
			Section: section,
			Key:     strings.ToLower(strings.TrimSpace(key)),
			Value:   strings.TrimSpace(value),
			Line:    lineNo,
		})
	}
	return entries
}

func iniValue(entries []iniEntry, section, key string) string {
	section = strings.ToLower(section)
	key = strings.ToLower(key)
	for _, entry := range entries {
		if entry.Section == section && entry.Key == key {
			return entry.Value
		}
	}
	return ""
}

func decodeText(raw []byte) string {
	switch {
	case bytes.HasPrefix(raw, []byte{0xef, 0xbb, 0xbf}):
		return string(raw[3:])
	case bytes.HasPrefix(raw, []byte{0xff, 0xfe}):
		return decodeUTF16(raw[2:], binary.LittleEndian)
	case bytes.HasPrefix(raw, []byte{0xfe, 0xff}):
		return decodeUTF16(raw[2:], binary.BigEndian)
	case utf8.Valid(raw):
		return string(raw)
	default:
		return decodeLegacyText(raw)
	}
}

func decodeUTF16(raw []byte, order binary.ByteOrder) string {
	if len(raw)%2 != 0 {
		raw = raw[:len(raw)-1]
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = order.Uint16(raw[i*2:])
	}
	return string(utf16.Decode(units))
}

func parseNumericSuffix(key, prefix string) (int, bool) {
	if !strings.HasPrefix(key, prefix) {
		return 0, false
	}
	number, err := strconv.Atoi(strings.TrimPrefix(key, prefix))
	return number, err == nil && number >= 0
}
