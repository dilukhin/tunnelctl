package autostart

import (
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestEncodeUTF16LEWithBOM(t *testing.T) {
	const input = `<?xml version="1.0" encoding="UTF-16"?><Description>Проверка</Description>`
	encoded := encodeUTF16LEWithBOM(input)
	if len(encoded) < 4 || encoded[0] != 0xff || encoded[1] != 0xfe {
		t.Fatalf("нет UTF-16 LE BOM: % x", encoded[:min(len(encoded), 4)])
	}
	units := make([]uint16, (len(encoded)-2)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(encoded[2+i*2:])
	}
	decoded := string(utf16.Decode(units))
	if decoded != input {
		t.Fatalf("декодированное значение отличается:\nполучено: %q\nожидалось: %q", decoded, input)
	}
}

func TestRenderWindowsTaskUsesSupportedRestartSettings(t *testing.T) {
	xml := renderWindowsTask(Spec{
		Target:     "auto",
		Executable: `C:\Program Files\tunnelctl\tunnelctl.exe`,
		ConfigPath: `C:\Users\Dima\AppData\Roaming\tunnelctl\tunnelctl.json`,
	})
	for _, expected := range []string{
		`encoding="UTF-16"`,
		`<Interval>PT1M</Interval>`,
		`<Count>255</Count>`,
	} {
		if !strings.Contains(xml, expected) {
			t.Fatalf("XML не содержит %q", expected)
		}
	}
	for _, forbidden := range []string{"PT15S", "<Count>999</Count>", `encoding="UTF-8"`} {
		if strings.Contains(xml, forbidden) {
			t.Fatalf("XML содержит устаревшее значение %q", forbidden)
		}
	}
}
