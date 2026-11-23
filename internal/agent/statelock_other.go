//go:build !unix && !windows

package agent

import "os"

func openAndLockFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
}

func unlockAndCloseFile(f *os.File) error {
	return f.Close()
}
