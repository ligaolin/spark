//go:build windows

package secure

import "golang.org/x/sys/windows/registry"

// machineGuid reads the Windows MachineGuid (stable per installation).
func machineGuid() (string, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Cryptography`, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()
	s, _, err := k.GetStringValue("MachineGuid")
	return s, err
}
