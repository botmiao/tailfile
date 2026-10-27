package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// ServiceName is the per-user service id on every OS.
// Linux: ~/.config/systemd/user/tailfile-agent.service
// macOS: ~/Library/LaunchAgents/tailfile-agent.plist
// Windows: a Task Scheduler logon task named TailfileAgent
const ServiceName = "tailfile-agent"

// WindowsTaskName is the Task Scheduler task for the current user.
// kardianos/service on Windows always installs through the Service Control
// Manager, which requires an administrator. This task does not.
const WindowsTaskName = "TailfileAgent"

const serviceConfigFile = "service.json"

// ErrUserServiceOnly is returned for --system. A root or machine-wide unit
// is intentionally not implemented.
//
// TODO: add a --system installer (systemd system unit, LaunchDaemon, Windows
// SCM service) beside this one. It must be a separate code path: it needs
// root or an administrator, and it must not reuse the per-user unit, plist,
// or logon task. Switching which OS user owns the service is uninstall, then
// install while logged in as the other user — there is no in-place identity
// change.
var ErrUserServiceOnly = errors.New("only a per-user service is implemented; --system would install a root or machine-wide unit and is left for later (see TODO on ErrUserServiceOnly). To change accounts, uninstall, then install as the other user")

// serviceSpec is the per-user service definition. It contains no secrets.
type serviceSpec struct {
	Name        string
	DisplayName string
	Description string
	Executable  string
	Arguments   []string
	StateDir    string
	ConfigPath  string
}

type serviceDiskConfig struct {
	Scope      string   `json:"scope"`
	Executable string   `json:"executable"`
	Arguments  []string `json:"arguments"`
	// Scope "system" is reserved. See ErrUserServiceOnly.
}

// ControlService runs install, uninstall, start, stop, or status for the
// current OS user. flagArgs are the agent's own flags (no "service" verb).
// An auth key passed with -authkey is stored in the state directory and is
// not placed in the service command.
func ControlService(action string, cfg Config, flagArgs []string) (string, error) {
	switch action {
	case "install", "uninstall", "start", "stop", "status":
	default:
		return "", fmt.Errorf("unknown service command %q", action)
	}
	args, authKey, err := PrepareServiceArgs(flagArgs)
	if err != nil {
		return "", err
	}
	if err := cfg.applyDefaults(); err != nil {
		return "", err
	}
	args = stableServiceArgs(args, cfg)
	exe, err := currentExecutable()
	if err != nil {
		return "", err
	}
	spec := serviceSpec{
		Name:        ServiceName,
		DisplayName: "Tailfile Agent",
		Description: "Tailfile agent for the current user (no privilege elevation)",
		Executable:  exe,
		Arguments:   args,
		StateDir:    cfg.StateDir,
		ConfigPath:  filepath.Join(cfg.StateDir, serviceConfigFile),
	}
	switch action {
	case "install":
		return installUserService(spec, authKey)
	case "uninstall":
		msg, err := applyPlatformService("uninstall", spec)
		if err != nil {
			return "", err
		}
		if remErr := os.Remove(spec.ConfigPath); remErr != nil && !errors.Is(remErr, os.ErrNotExist) {
			log.Printf("tailfile: remove %s: %v", spec.ConfigPath, remErr)
		}
		return msg, nil
	default:
		return applyPlatformService(action, spec)
	}
}

func installUserService(spec serviceSpec, authKey string) (string, error) {
	if _, err := applyPlatformService("status", spec); err == nil {
		return "", errors.New("service is already installed for this user; run uninstall, then install, to change flags or account")
	} else if !errors.Is(err, errServiceNotInstalled) {
		return "", err
	}
	if strings.TrimSpace(authKey) != "" {
		if err := storeAuthKey(spec.StateDir, authKey); err != nil {
			return "", err
		}
		log.Printf("tailfile: -authkey was stored in the state directory and omitted from the service command (value not logged)")
	}
	if err := writeServiceConfig(spec); err != nil {
		return "", err
	}
	return applyPlatformService("install", spec)
}

// PrepareServiceArgs drops -authkey (returned separately) and rejects --system.
// The returned arguments are safe to put in a unit file, plist, or task.
func PrepareServiceArgs(flagArgs []string) (args []string, authKey string, err error) {
	for i := 0; i < len(flagArgs); i++ {
		a := flagArgs[i]
		if a == "--" {
			args = append(args, flagArgs[i:]...)
			break
		}
		if a == "--system" || a == "-system" {
			return nil, "", ErrUserServiceOnly
		}
		name, val, has := splitRawFlag(a)
		if name == "-authkey" || name == "--authkey" {
			if has {
				authKey = val
				continue
			}
			if i+1 >= len(flagArgs) {
				return nil, "", errors.New("-authkey requires a value")
			}
			i++
			authKey = flagArgs[i]
			continue
		}
		args = append(args, a)
	}
	return args, authKey, nil
}

func stableServiceArgs(args []string, cfg Config) []string {
	if !hasRawFlag(args, "-hostname") && !hasRawFlag(args, "--hostname") && cfg.Hostname != "" {
		args = append(args, "-hostname", cfg.Hostname)
	}
	if !hasRawFlag(args, "-state") && !hasRawFlag(args, "--state") && cfg.StateDir != "" {
		args = append(args, "-state", cfg.StateDir)
	}
	if !hasRawFlag(args, "-net") && !hasRawFlag(args, "--net") && cfg.Net != "" {
		args = append(args, "-net", cfg.Net)
	}
	if !hasRawFlag(args, "-read") && !hasRawFlag(args, "--read") && len(cfg.ReadRoots) > 0 {
		args = append(args, "-read", strings.Join(cfg.ReadRoots, ","))
	}
	return args
}

func hasRawFlag(args []string, name string) bool {
	for _, a := range args {
		if a == name || strings.HasPrefix(a, name+"=") {
			return true
		}
	}
	return false
}

func splitRawFlag(a string) (name, val string, hasVal bool) {
	if !strings.HasPrefix(a, "-") {
		return "", "", false
	}
	name, val, hasVal = strings.Cut(a, "=")
	return name, val, hasVal
}

func writeServiceConfig(spec serviceSpec) error {
	raw, err := json.MarshalIndent(serviceDiskConfig{
		Scope:      "user",
		Executable: spec.Executable,
		Arguments:  spec.Arguments,
	}, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return writeSecret(spec.ConfigPath, raw)
}

func currentExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

// errServiceNotInstalled is the platform-neutral "nothing is installed" error.
var errServiceNotInstalled = errors.New("service is not installed")

// windowsTaskXML is a logon task for the current user at least privilege.
// It is UTF-8 here; the Windows installer writes it as UTF-16 for schtasks.
func windowsTaskXML(spec serviceSpec) string {
	args := windowsTaskArguments(spec.Arguments)
	return `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>` + xmlEscape(spec.Description) + `</Description>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>false</StartWhenAvailable>
    <Enabled>true</Enabled>
    <Hidden>false</Hidden>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Priority>7</Priority>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>` + xmlEscape(spec.Executable) + `</Command>
      <Arguments>` + xmlEscape(args) + `</Arguments>
    </Exec>
  </Actions>
</Task>
`
}

func windowsTaskArguments(args []string) string {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		parts = append(parts, quoteWindowsArg(a))
	}
	return strings.Join(parts, " ")
}

func quoteWindowsArg(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \t\"") {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		if s[i] == '"' {
			b.WriteString(`\"`)
			continue
		}
		b.WriteByte(s[i])
	}
	b.WriteByte('"')
	return b.String()
}

func xmlEscape(s string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
	).Replace(s)
}
