//go:build linux

package agent

import (
	"strings"
	"testing"
)

func TestUserSystemdUnitIsPerUser(t *testing.T) {
	if strings.Contains(userSystemdScript, "multi-user.target") {
		t.Fatal("user unit must not be WantedBy=multi-user.target")
	}
	if !strings.Contains(userSystemdScript, "WantedBy=default.target") {
		t.Fatal(userSystemdScript)
	}
	if strings.Contains(userSystemdScript, "/etc/sysconfig") {
		t.Fatal("user unit must not read a system EnvironmentFile")
	}
	if !strings.Contains(userSystemdScript, "Restart=on-failure") {
		t.Fatal(userSystemdScript)
	}
	cfg, err := newUserService(serviceSpec{
		Name:        ServiceName,
		DisplayName: "Tailfile Agent",
		Description: "per user",
		Executable:  "/usr/bin/tailfile-agent",
		Arguments:   []string{"-net", "system"},
		StateDir:    t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Platform() == "" {
		t.Fatal("empty platform")
	}
	// The option is applied by newUserService. Re-read it through the
	// string form only; do not Install (that would call systemctl --user).
	text := cfg.String()
	if text == "" {
		t.Fatal("empty service")
	}
}
