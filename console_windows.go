//go:build windows

package main

import (
	"log"
	"os"

	"golang.org/x/sys/windows"
)

const attachParentProcess = ^uint32(0) // ATTACH_PARENT_PROCESS ((DWORD)-1)

var procAttachConsole = windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")

// ensureConsole attaches the parent console ONLY when stdout has no valid pipe,
// disk, or console handle (for GUI-subsystem builds launched from cmd/pwsh),
// while preserving captured stdout/stderr pipes in tests and shell redirections.
func ensureConsole() {
	if hasValidStdHandle(windows.STD_OUTPUT_HANDLE) || hasValidStdHandle(windows.STD_ERROR_HANDLE) {
		return
	}
	r1, _, _ := procAttachConsole.Call(uintptr(attachParentProcess))
	if r1 == 0 {
		return
	}
	if out, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
		os.Stdout = out
		os.Stderr = out
		log.SetOutput(os.Stderr)
	}
	if in, err := os.OpenFile("CONIN$", os.O_RDONLY, 0); err == nil {
		os.Stdin = in
	}
}

func hasValidStdHandle(stdHandle uint32) bool {
	h, err := windows.GetStdHandle(stdHandle)
	if err != nil || h == 0 || h == windows.InvalidHandle {
		return false
	}
	ft, err := windows.GetFileType(h)
	if err != nil {
		return false
	}
	return ft != windows.FILE_TYPE_UNKNOWN
}
