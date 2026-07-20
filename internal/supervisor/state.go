package supervisor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"tunnelctl/internal/paths"
)

type stateStore struct {
	mu   sync.RWMutex
	path string
	data State
}

func newStateStore(configPath, target, targetType string) *stateStore {
	return &stateStore{
		path: paths.StatePath(),
		data: State{
			Version:        protocolVersion,
			PID:            os.Getpid(),
			StartedAt:      now(),
			OriginalTarget: target,
			OriginalType:   targetType,
			LogPath:        paths.LogPath(),
			ConfigPath:     configPath,
			Status:         "запуск",
		},
	}
}

func (s *stateStore) update(fn func(*State)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.data)
	return s.writeLocked()
}

func (s *stateStore) snapshot() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data
}

func (s *stateStore) writeLocked() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("не удалось ограничить доступ к каталогу состояния: %w", err)
	}
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("не удалось обновить state-файл: %w", err)
	}
	return nil
}

func (s *stateStore) remove() {
	_ = os.Remove(s.path)
}
