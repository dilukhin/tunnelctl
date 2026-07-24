//go:build !windows

package elevation

// MaybeRelaunch does nothing on platforms without UAC elevation.
func MaybeRelaunch(args []string) (bool, int, error) {
	return false, 0, nil
}
