package agent

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareServiceArgsOmitsAuthKeyAndRejectsSystem(t *testing.T) {
	secret := "tskey-auth-super-secret"
	_, _, err := PrepareServiceArgs([]string{"-net", "tsnet", "--system"})
	if !errorsIs(err, ErrUserServiceOnly) {
		t.Fatalf("system scope: %v", err)
	}
	args, key, err := PrepareServiceArgs([]string{
		"-net", "tsnet", "-authkey", secret, "-read", "/home/ada/Tailfile", "--authkey=" + secret,
	})
	if err != nil {
		t.Fatal(err)
	}
	if key != secret {
		t.Fatalf("key not captured")
	}
	joined := strings.Join(args, "\n")
	if strings.Contains(joined, secret) || strings.Contains(joined, "authkey") {
		t.Fatalf("service args leaked the key: %q", args)
	}
	if !strings.Contains(joined, "-net") || !strings.Contains(joined, "/home/ada/Tailfile") {
		t.Fatalf("args %q", args)
	}
}

func TestStableServiceArgsAndConfig(t *testing.T) {
	state := t.TempDir()
	cfg := Config{
		Hostname:  "tailfile-box",
		StateDir:  state,
		Net:       "system",
		ReadRoots: []string{filepath.Join(state, "share")},
		Demo:      true,
		Port:      8644,
		LocalAddr: "127.0.0.1:8645",
	}
	args := stableServiceArgs([]string{"-write", cfg.ReadRoots[0]}, cfg)
	joined := strings.Join(args, "\n")
	for _, want := range []string{"-hostname", "tailfile-box", "-state", state, "-net", "system", "-read", "-write"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s in %q", want, args)
		}
	}
	spec := serviceSpec{
		Name: ServiceName, Executable: "/usr/bin/tailfile-agent", Arguments: args,
		StateDir: state, ConfigPath: filepath.Join(state, serviceConfigFile),
		Description: "Tailfile agent for the current user",
	}
	if err := writeServiceConfig(spec); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(spec.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("service.json mode %o", st.Mode().Perm())
	}
	raw, err := os.ReadFile(spec.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `"scope": "user"`) {
		t.Fatalf("config %s", text)
	}
	if strings.Contains(text, "tskey") || strings.Contains(text, "system-wide") {
		t.Fatalf("config %s", text)
	}
}

func TestWindowsLogonTaskDoesNotElevate(t *testing.T) {
	secret := "tskey-auth-do-not-embed"
	spec := serviceSpec{
		Description: "Tailfile agent for the current user",
		Executable:  `C:\Program Files\Tailfile\tailfile-agent.exe`,
		Arguments:   []string{"-net", "system", "-read", `C:\Users\Ada\My Tailfile`, "-hostname", "box"},
	}
	xml := windowsTaskXML(spec)
	for _, want := range []string{"LogonTrigger", "LeastPrivilege", "InteractiveToken", "PT0S", spec.Executable, "My Tailfile"} {
		if !strings.Contains(xml, want) {
			t.Fatalf("xml missing %s\n%s", want, xml)
		}
	}
	for _, bad := range []string{"HighestAvailable", "RunLevel>Highest", secret, "tskey", "S4U"} {
		if strings.Contains(xml, bad) {
			t.Fatalf("xml contains %s", bad)
		}
	}
	if strings.Contains(xml, "<UserId>") {
		t.Fatal("task must not name a different user")
	}
	quoted := windowsTaskArguments(spec.Arguments)
	if !strings.Contains(quoted, `"C:\Users\Ada\My Tailfile"`) {
		t.Fatalf("quoting %s", quoted)
	}
}

func TestServicePlanStoresAuthKeyOutsideTheUnit(t *testing.T) {
	secret := "tskey-auth-plan-secret"
	state := t.TempDir()
	var logBuf strings.Builder
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	args, key, err := PrepareServiceArgs([]string{"-net", "tsnet", "-authkey", secret, "-hostname", "box"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{Hostname: "box", StateDir: state, Net: "tsnet", Demo: true, Port: 8644, LocalAddr: "127.0.0.1:8645"}
	args = stableServiceArgs(args, cfg)
	if err := storeAuthKey(state, key); err != nil {
		t.Fatal(err)
	}
	spec := serviceSpec{
		Executable: "/usr/bin/tailfile-agent", Arguments: args,
		StateDir: state, ConfigPath: filepath.Join(state, serviceConfigFile),
		Description: "Tailfile agent for the current user",
	}
	if err := writeServiceConfig(spec); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(state, AuthKeyFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != secret {
		t.Fatal("stored key mismatch")
	}
	st, err := os.Stat(filepath.Join(state, AuthKeyFile))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("ts-authkey mode %o", st.Mode().Perm())
	}
	cfgRaw, err := os.ReadFile(spec.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cfgRaw), secret) || strings.Contains(strings.Join(args, " "), secret) {
		t.Fatal("service definition contains the auth key")
	}
	if strings.Contains(logBuf.String(), secret) {
		t.Fatalf("log contained the auth key: %s", logBuf.String())
	}
	got, source := resolveAuthKey(Config{StateDir: state})
	if got != secret || source != "file" {
		t.Fatalf("resolve %q %q", got, source)
	}
	t.Setenv("TS_AUTHKEY", "from-env")
	got, source = resolveAuthKey(Config{StateDir: state, AuthKey: "from-flag"})
	if got != "from-env" || source != "TS_AUTHKEY" {
		t.Fatalf("env precedence %q %q", got, source)
	}
	os.Unsetenv("TS_AUTHKEY")
	got, source = resolveAuthKey(Config{StateDir: state, AuthKey: "from-flag"})
	if got != "from-flag" || source != "-authkey" {
		t.Fatalf("flag precedence %q %q", got, source)
	}
}

func errorsIs(err, target error) bool {
	return err != nil && (err == target || strings.Contains(err.Error(), target.Error()))
}
