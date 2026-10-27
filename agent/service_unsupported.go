//go:build !linux && !darwin && !windows

package agent

import "fmt"

func applyPlatformService(action string, spec serviceSpec) (string, error) {
	return "", fmt.Errorf("per-user service %s is not implemented on this OS", action)
}
