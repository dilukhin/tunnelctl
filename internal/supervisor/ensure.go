package supervisor

import (
	"context"
	"errors"
	"path/filepath"
	"time"
)

// ErrAlreadyManaged подтверждает владельца, а не текущую доступность SOCKS.
var ErrAlreadyManaged = errors.New("нужный управляемый tunnelctl уже запущен")

// checkExisting вызывается только после отказа общей блокировки connect/ensure.
// Неотвечающий владелец, другая цель и старый протокол не дают права убивать процесс.
func checkExisting(ctx context.Context, opts Options) error {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	state, err := Status(ctx)
	if err != nil {
		return errors.New("управляющий канал занят, но владелец не подтвердил состояние; повторите позже")
	}
	if state.Version != protocolVersion {
		return errors.New("владелец использует несовместимую версию состояния")
	}
	if filepath.Clean(state.ConfigPath) != opts.ConfigPath || !state.Watch {
		return errors.New("работает другой экземпляр или режим --watch не подтверждён; автоматическая замена запрещена")
	}
	wantType, wantProfiles, err := resolveTarget(opts.Config, opts.Target)
	if err != nil {
		return err
	}
	gotType, gotProfiles, err := resolveTarget(opts.Config, state.OriginalTarget)
	if err != nil || gotType != wantType || state.OriginalType != wantType {
		return errors.New("работающий экземпляр использует другую цель")
	}
	if wantType == "group" {
		want, _ := opts.Config.ResolveGroup(opts.Target)
		got, _ := opts.Config.ResolveGroup(state.OriginalTarget)
		if want.Name != got.Name {
			return errors.New("работающий экземпляр использует другую группу")
		}
	} else if wantProfiles[0].Name != gotProfiles[0].Name {
		return errors.New("работающий экземпляр использует другой профиль")
	}
	return ErrAlreadyManaged
}
