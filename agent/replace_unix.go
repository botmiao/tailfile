//go:build !windows

package agent

import "os"

// replaceFile atomically replaces dst with src on the same directory.
// On Unix, rename replaces an existing file and does not leave a window
// where dst is missing.
func replaceFile(src, dst string) error {
	return os.Rename(src, dst)
}
