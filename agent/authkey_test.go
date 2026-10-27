package agent

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthKeyAPIStoresAndDoesNotEcho(t *testing.T) {
	secret := "tskey-auth-not-for-logs-or-json"
	dir := t.TempDir()
	state := t.TempDir()
	s := newServer("local", "tsnet", "ada@example.com", Roots{Read: []string{dir}, Write: []string{dir}}, 1024)
	s.stateDir = state
	s.guiToken = testGUIToken
	s.noteNet("auto", "tsnet")
	ts := httptest.NewServer(s.LocalHandler())
	defer ts.Close()
	c := localClient(ts)

	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	st, err := c.Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Net != "tsnet" || st.NetRequested != "auto" || st.AuthKeyConfigured {
		t.Fatalf("settings %+v", st)
	}

	status, err := c.SetAuthKey(context.Background(), secret)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Configured || !status.RestartRequired {
		t.Fatalf("status %+v", status)
	}
	raw, err := os.ReadFile(filepath.Join(state, AuthKeyFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != secret {
		t.Fatal("file does not hold the key")
	}
	info, err := os.Stat(filepath.Join(state, AuthKeyFile))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}

	body := settingsBody(t, ts.URL)
	if strings.Contains(body, secret) {
		t.Fatalf("settings response echoed the key: %s", body)
	}
	if strings.Contains(logs.String(), secret) {
		t.Fatalf("log echoed the key: %s", logs.String())
	}
	st, err = c.Settings(context.Background())
	if err != nil || !st.AuthKeyConfigured {
		t.Fatalf("configured %+v %v", st, err)
	}

	cleared, err := c.ClearAuthKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Configured || !cleared.RestartRequired {
		t.Fatalf("clear %+v", cleared)
	}
	if _, err := os.Stat(filepath.Join(state, AuthKeyFile)); !os.IsNotExist(err) {
		t.Fatalf("key file still there: %v", err)
	}

	req, err := http.NewRequest(http.MethodPut, ts.URL+"/v1/local/settings/authkey", strings.NewReader(`{"authKey":"`+secret+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing token: %d", resp.StatusCode)
	}

	tail := httptest.NewRequest(http.MethodGet, "/v1/local/settings", nil)
	tail.RemoteAddr = "100.64.0.2:1234"
	rr := httptest.NewRecorder()
	s.TailnetHandler().ServeHTTP(rr, tail)
	if rr.Code == http.StatusOK || strings.Contains(rr.Body.String(), secret) {
		t.Fatalf("tailnet settings %d %s", rr.Code, rr.Body.String())
	}
}

func TestAuthKeyRejectsEmptyAndMultiline(t *testing.T) {
	state := t.TempDir()
	if err := storeAuthKey(state, "  "); err == nil {
		t.Fatal("empty key")
	}
	if err := storeAuthKey(state, "abc\ndef"); err == nil {
		t.Fatal("multiline key")
	}
	if _, err := os.Stat(filepath.Join(state, AuthKeyFile)); !os.IsNotExist(err) {
		t.Fatal("rejected key was stored")
	}
}

func settingsBody(t *testing.T, base string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+"/v1/local/settings", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testGUIToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("settings %d %s", resp.StatusCode, b)
	}
	return string(b)
}
