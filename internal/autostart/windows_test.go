//go:build windows

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

func TestDecodeWindowsCodePage866(t *testing.T) {
	raw := []byte{142, 152, 136, 129, 138, 128, 58, 32, 145, 168, 225, 226, 165, 172, 165, 32, 173, 165, 32, 227, 164, 160, 165, 226, 225, 239, 32, 173, 160, 169, 226, 168, 32, 227, 170, 160, 167, 160, 173, 173, 235, 169, 32, 228, 160, 169, 171, 46}
	got, ok := decodeWindowsCodePage(raw, 866)
	if !ok {
		t.Fatal("CP866 не декодирована")
	}
	const want = "ОШИБКА: Системе не удается найти указанный файл."
	if got != want {
		t.Fatalf("неверная декодировка CP866:\nполучено: %q\nожидалось: %q", got, want)
	}
	if !isTaskNotFound(got) {
		t.Fatalf("декодированное сообщение не распознано как отсутствие задачи: %q", got)
	}
}
