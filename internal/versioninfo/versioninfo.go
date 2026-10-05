package versioninfo

import (
	"strings"
	"sync/atomic"
)

var (
	current     atomic.Value
	buildCommit string
)

func init() {
	current.Store("unknown")
}

// Set задаёт идентификатор текущего бинарника для журналов, state и внутренних запросов.
func Set(version string) {
	version = strings.TrimSpace(version)
	if version == "" || strings.ContainsAny(version, "\r\n") {
		current.Store("unknown")
		return
	}
	commit := BuildCommit()
	if commit == "unknown" {
		current.Store(version + ".dev")
		return
	}
	current.Store(version + "." + commit[:8])
}

// Current возвращает канонический идентификатор текущего бинарника.
func Current() string {
	return current.Load().(string)
}

// BuildCommit возвращает полный SHA исходного коммита выпускной сборки.
// Для локальной сборки без внедрённого SHA возвращается "unknown".
func BuildCommit() string {
	commit := strings.ToLower(strings.TrimSpace(buildCommit))
	if !validCommit(commit) {
		return "unknown"
	}
	return commit
}

func validCommit(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
