//go:build !windows

package agent

import "os"

// tightenSecret leaves the file readable and writable by the current user only.
func tightenSecret(path string) error {
	return os.Chmod(path, 0o600)
}
