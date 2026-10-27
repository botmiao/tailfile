//go:build linux || darwin

package agent

import (
	"errors"
	"fmt"

	"github.com/kardianos/service"
)

// userSystemdScript is a user unit. kardianos's default script is a system
// unit (WantedBy=multi-user.target and an /etc/sysconfig EnvironmentFile).
// A per-user agent must join default.target and must not look like a
// machine-wide service.
const userSystemdScript = `[Unit]
Description={{.Description}}
ConditionFileIsExecutable={{.Path|cmdEscape}}

[Service]
StartLimitInterval=5
StartLimitBurst=10
ExecStart={{.Path|cmdEscape}}{{range .Arguments}} {{.|cmd}}{{end}}
Restart=on-failure
RestartSec=2

[Install]
WantedBy=default.target
`

func applyPlatformService(action string, spec serviceSpec) (string, error) {
	svc, err := newUserService(spec)
	if err != nil {
		return "", err
	}
	switch action {
	case "install":
		if err := svc.Install(); err != nil {
			return "", err
		}
		return "installed per-user service " + spec.Name + " for the current user", nil
	case "uninstall":
		if err := svc.Uninstall(); err != nil {
			return "", err
		}
		return "uninstalled per-user service " + spec.Name, nil
	case "start":
		if err := svc.Start(); err != nil {
			return "", err
		}
		return "started " + spec.Name, nil
	case "stop":
		if err := svc.Stop(); err != nil {
			return "", err
		}
		return "stopped " + spec.Name, nil
	case "status":
		st, err := svc.Status()
		if errors.Is(err, service.ErrNotInstalled) {
			return "", errServiceNotInstalled
		}
		if err != nil {
			// A failed systemd unit is installed but not running.
			if st == service.StatusUnknown && !errors.Is(err, service.ErrNotInstalled) {
				return "stopped", nil
			}
			return "", err
		}
		switch st {
		case service.StatusRunning:
			return "running", nil
		case service.StatusStopped:
			return "stopped", nil
		default:
			return "", errServiceNotInstalled
		}
	default:
		return "", fmt.Errorf("unknown service action %q", action)
	}
}

func newUserService(spec serviceSpec) (service.Service, error) {
	cfg := &service.Config{
		Name:        spec.Name,
		DisplayName: spec.DisplayName,
		Description: spec.Description,
		Executable:  spec.Executable,
		Arguments:   append([]string(nil), spec.Arguments...),
		Option: service.KeyValue{
			// POSIX: systemd --user, or ~/Library/LaunchAgents. This does
			// not request root. Windows ignores UserService; that OS uses
			// service_windows.go instead of this file.
			"UserService":   true,
			"RunAtLoad":     true,
			"KeepAlive":     true,
			"SystemdScript": userSystemdScript,
			"LogDirectory":  spec.StateDir,
		},
	}
	// The running agent is a normal process (systemd and launchd exec it).
	// This program value is only here because kardianos requires one; control
	// commands do not call Start or Stop on it.
	return service.New(noopServiceProgram{}, cfg)
}

type noopServiceProgram struct{}

func (noopServiceProgram) Start(service.Service) error { return nil }
func (noopServiceProgram) Stop(service.Service) error  { return nil }
