package supervisor

import "errors"

// ErrAlreadyRunning означает, что управляющий канал уже принадлежит другому экземпляру tunnelctl.
var ErrAlreadyRunning = errors.New("управляемый tunnelctl уже запущен")
