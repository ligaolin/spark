//go:build !windows

package secure

// machineGuid is not available on non-Windows platforms; the fallback
// machine ID (hostname + user home) is used instead.
func machineGuid() (string, error) {
	return "", nil
}
