//go:build windows

package agent

import (
	"bytes"
	"errors"
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
	return applyWindowsTask(action, WindowsTaskName, spec)
}

func applyWindowsTask(action, taskName string, spec serviceSpec) (string, error) {
	switch action {
	case "install":
		return installWindowsTask(taskName, spec)
	case "uninstall":
		if err := runSchtasks("/Delete", "/TN", taskName, "/F"); err != nil {
			if taskMissing(err) {
				return "", errServiceNotInstalled
			}
			return "", err
		}
		return "uninstalled per-user logon task " + taskName, nil
	case "start":
		if err := runSchtasks("/Run", "/TN", taskName); err != nil {
			return "", err
		}
		return "started " + taskName, nil
	case "stop":
		if err := runSchtasks("/End", "/TN", taskName); err != nil {
			return "", err
		}
		return "stopped " + taskName, nil
	case "status":
		out, err := schtasksOutput("/Query", "/TN", taskName, "/FO", "LIST", "/V")
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

func uninstallLegacyService(spec serviceSpec) (string, bool, error) {
	if _, err := applyWindowsTask("status", LegacyWindowsTaskName, spec); errors.Is(err, errServiceNotInstalled) {
		return "", false, nil
	} else if err != nil {
		return "", false, err
	}
	_, _ = applyWindowsTask("stop", LegacyWindowsTaskName, spec)
	msg, err := applyWindowsTask("uninstall", LegacyWindowsTaskName, spec)
	if errors.Is(err, errServiceNotInstalled) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return msg, true, nil
}

func installWindowsTask(taskName string, spec serviceSpec) (string, error) {
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
	if err := runSchtasks("/Create", "/TN", taskName, "/XML", path, "/F"); err != nil {
		return "", err
	}
	return "installed per-user logon task " + taskName + " (no administrator)", nil
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
