//go:build windows

package elevation

// MaybeRelaunch не повышает привилегии автоматически. Текущий Windows-backend
// управляет пользовательской задачей Планировщика с уровнем LeastPrivilege,
// поэтому install/remove, справка и dry-run должны выполняться от исходного
// пользователя и использовать его конфигурацию и SSH-окружение.
func MaybeRelaunch([]string) (bool, int, error) {
	return false, 0, nil
}
