//go:build !windows

package historyscan

func decodeLegacyText(raw []byte) string { return string(raw) }
