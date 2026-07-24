//go:build !windows

package autostart

import (
	"strings"
)

func decodeCommandOutput(raw []byte) string {
	return strings.TrimSpace(string(raw))
}
