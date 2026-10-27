//go:build windows

package agent

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unicode/utf16"
)

// kardianos/service on Windows installs a Service Control Manager service.
// CreateService requires an administrator even when the service account is
// the current user, and the library's UserService option is POSIX-only
// (systemd --user / LaunchAgent). A per-user logon task does not elevate.

func applyPlatformService(action string, spec serviceSpec) (string, error) {
	switch action {
	case "install":
		return installWindowsTask(spec)
	case "uninstall":
		if err := runSchtasks("/Delete", "/TN", WindowsTaskName, "/F"); err != nil {
			if taskMissing(err) {
				return "", errServiceNotInstalled
			}
			return "", err
		}
		return "uninstalled per-user logon task " + WindowsTaskName, nil
	case "start":
		if err := runSchtasks("/Run", "/TN", WindowsTaskName); err != nil {
			return "", err
		}
		return "started " + WindowsTaskName, nil
	case "stop":
		if err := runSchtasks("/End", "/TN", WindowsTaskName); err != nil {
			return "", err
		}
		return "stopped " + WindowsTaskName, nil
	case "status":
		out, err := schtasksOutput("/Query", "/TN", WindowsTaskName, "/FO", "LIST", "/V")
		if err != nil {
			if taskMissing(err) {
				return "", errServiceNotInstalled
			}
			return "", err
		}
		if windowsTaskRunning(out) {
			return "running", nil
		}
		return "stopped", nil
	default:
		return "", fmt.Errorf("unknown service action %q", action)
	}
}

func installWindowsTask(spec serviceSpec) (string, error) {
	xml := utf16LEBOM(windowsTaskXML(spec))
	f, err := os.CreateTemp("", "tailfile-task-*.xml")
	if err != nil {
		return "", err
	}
	path := f.Name()
	defer os.Remove(path)
	if _, err := f.Write(xml); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if err := runSchtasks("/Create", "/TN", WindowsTaskName, "/XML", path, "/F"); err != nil {
		return "", err
	}
	return "installed per-user logon task " + WindowsTaskName + " (no administrator)", nil
}

func runSchtasks(args ...string) error {
	_, err := schtasksOutput(args...)
	return err
}

func schtasksOutput(args ...string) (string, error) {
	cmd := exec.Command("schtasks", args...)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			return "", err
		}
		return text, fmt.Errorf("%w: %s", err, text)
	}
	return text, nil
}

func taskMissing(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "cannot find") ||
		strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "not found")
}

func windowsTaskRunning(query string) bool {
	for _, line := range strings.Split(query, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(line), "status:") {
			continue
		}
		return strings.Contains(strings.ToLower(line), "running")
	}
	return false
}

func utf16LEBOM(s string) []byte {
	u := utf16.Encode([]rune(s))
	var buf bytes.Buffer
	buf.Write([]byte{0xFF, 0xFE})
	for _, r := range u {
		buf.WriteByte(byte(r))
		buf.WriteByte(byte(r >> 8))
	}
	return buf.Bytes()
}
