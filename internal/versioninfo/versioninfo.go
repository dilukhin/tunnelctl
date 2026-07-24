package versioninfo

import (
	"strings"
	"sync/atomic"
)

var current atomic.Value

func init() {
	current.Store("unknown")
}

// Set задаёт версию текущего бинарника для журналов, state и внутренних запросов.
func Set(version string) {
	version = strings.TrimSpace(version)
	if version == "" || strings.ContainsAny(version, "\r\n") {
		version = "unknown"
	}
	current.Store(version)
}

// Current возвращает версию текущего бинарника.
func Current() string {
	return current.Load().(string)
}
