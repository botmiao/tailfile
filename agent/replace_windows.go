//go:build windows

package agent

import "golang.org/x/sys/windows"

// replaceFile replaces dst with src. os.Rename on Windows fails when dst
// exists, so this uses MoveFileEx with MOVEFILE_REPLACE_EXISTING, which
// replaces a file on the same volume in one call. The destination is not
// deleted first.
func replaceFile(src, dst string) error {
	from, err := windows.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
