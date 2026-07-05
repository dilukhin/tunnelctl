package paths

import (
	"os"
	"path/filepath"
	"runtime"
)

const AppName = "tunnelctl"

// ConfigDir возвращает общепринятую папку конфигурации для текущей ОС.
func ConfigDir() string {
	if runtime.GOOS == "windows" {
		if v := os.Getenv("APPDATA"); v != "" {
			return filepath.Join(v, AppName)
		}
		return filepath.Join(homeDir(), "AppData", "Roaming", AppName)
	}
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, AppName)
	}
	return filepath.Join(homeDir(), ".config", AppName)
}

// StateDir возвращает общепринятую папку состояния/логов для текущей ОС.
func StateDir() string {
	if runtime.GOOS == "windows" {
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			return filepath.Join(v, AppName)
		}
		return filepath.Join(homeDir(), "AppData", "Local", AppName)
	}
	if v := os.Getenv("XDG_STATE_HOME"); v != "" {
		return filepath.Join(v, AppName)
	}
	return filepath.Join(homeDir(), ".local", "state", AppName)
}

func ConfigPath() string { return filepath.Join(ConfigDir(), "tunnelctl.json") }
func LogPath() string    { return filepath.Join(StateDir(), "tunnelctl.log") }

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	return "."
}
