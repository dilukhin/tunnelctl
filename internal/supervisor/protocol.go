package supervisor

import "time"

const protocolVersion = 1

type Request struct {
	Version int    `json:"version"`
	Action  string `json:"action"`
	Target  string `json:"target,omitempty"`
}

type Response struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
	State   *State `json:"state,omitempty"`
}

// State — безопасный диагностический снимок управляемого туннеля.
type State struct {
	Version           int       `json:"version"`
	PID               int       `json:"pid"`
	StartedAt         time.Time `json:"started_at"`
	OriginalTarget    string    `json:"original_target"`
	OriginalType      string    `json:"original_type"`
	ActiveProfile     string    `json:"active_profile,omitempty"`
	Listen            string    `json:"listen,omitempty"`
	LastHealthSuccess time.Time `json:"last_health_success,omitempty"`
	LastHealthError   string    `json:"last_health_error,omitempty"`
	LogPath           string    `json:"log_path"`
	ConfigPath        string    `json:"config_path"`
	Status            string    `json:"status"`
}
