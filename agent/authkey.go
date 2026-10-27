package agent

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Settings is the local GUI's view of this agent. It never includes an auth key.
type Settings struct {
	Net               string `json:"net"`
	NetRequested      string `json:"netRequested,omitempty"`
	AuthKeyConfigured bool   `json:"authKeyConfigured"`
}

// AuthKeyStatus is the only response a key write or delete returns.
type AuthKeyStatus struct {
	Configured      bool `json:"configured"`
	RestartRequired bool `json:"restartRequired"`
}

// AuthKeyFile is the tsnet auth key inside the state directory.
// Mode 0600 (and, on Windows, a DACL for the current user only).
// The value is never logged and never returned by the local API.
const AuthKeyFile = "ts-authkey"

const maxAuthKeyBytes = 4096

// storeAuthKey writes the key. Errors do not include the key.
func storeAuthKey(stateDir, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("auth key is empty")
	}
	if strings.ContainsAny(key, "\r\n") {
		return errors.New("auth key must be a single line")
	}
	if len(key) > maxAuthKeyBytes {
		return errors.New("auth key is too long")
	}
	if strings.TrimSpace(stateDir) == "" {
		return errors.New("no state directory for the auth key")
	}
	path := filepath.Join(stateDir, AuthKeyFile)
	if err := writeSecret(path, []byte(key+"\n")); err != nil {
		return errors.New("could not store auth key")
	}
	log.Printf("tailfile: auth key stored at %s (value not logged)", path)
	return nil
}

func deleteAuthKey(stateDir string) error {
	if strings.TrimSpace(stateDir) == "" {
		return errors.New("no state directory for the auth key")
	}
	path := filepath.Join(stateDir, AuthKeyFile)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.New("could not remove auth key")
	}
	log.Printf("tailfile: auth key removed from %s", path)
	return nil
}

func authKeyConfigured(stateDir string) bool {
	if strings.TrimSpace(stateDir) == "" {
		return false
	}
	b, err := os.ReadFile(filepath.Join(stateDir, AuthKeyFile))
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(b)) != ""
}

// resolveAuthKey chooses the tsnet auth key.
// TS_AUTHKEY wins over -authkey, which wins over the ts-authkey file.
// The key itself is not logged; source is "TS_AUTHKEY", "-authkey", "file", or "".
func resolveAuthKey(cfg Config) (key, source string) {
	if k := strings.TrimSpace(os.Getenv("TS_AUTHKEY")); k != "" {
		return k, "TS_AUTHKEY"
	}
	if k := strings.TrimSpace(cfg.AuthKey); k != "" {
		return k, "-authkey"
	}
	if strings.TrimSpace(cfg.StateDir) == "" {
		return "", ""
	}
	b, err := os.ReadFile(filepath.Join(cfg.StateDir, AuthKeyFile))
	if err != nil {
		return "", ""
	}
	k := strings.TrimSpace(string(b))
	if k == "" {
		return "", ""
	}
	return k, "file"
}

func (s *Server) noteNet(requested, resolved string) {
	s.mu.Lock()
	s.netRequested = requested
	if resolved != "" {
		s.mode = resolved
	}
	s.mu.Unlock()
}

func (s *Server) currentSettings() Settings {
	s.mu.Lock()
	mode := s.mode
	requested := s.netRequested
	dir := s.stateDir
	s.mu.Unlock()
	if mode == "" {
		mode = "unknown"
	}
	return Settings{
		Net:               mode,
		NetRequested:      requested,
		AuthKeyConfigured: authKeyConfigured(dir),
	}
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.currentSettings())
}

func (s *Server) handleAuthKeyPut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AuthKey string `json:"authKey"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, maxAuthKeyBytes+128))
	if err := dec.Decode(&body); err != nil {
		writeErr(w, statusErr(400, "bad json"))
		return
	}
	if s.stateDir == "" {
		writeErr(w, statusErr(500, "no state directory"))
		return
	}
	if err := storeAuthKey(s.stateDir, body.AuthKey); err != nil {
		// storeAuthKey's errors name the problem and never the key.
		writeErr(w, statusErr(400, "%s", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, AuthKeyStatus{Configured: true, RestartRequired: true})
}

func (s *Server) handleAuthKeyDelete(w http.ResponseWriter, r *http.Request) {
	if s.stateDir == "" {
		writeErr(w, statusErr(500, "no state directory"))
		return
	}
	if err := deleteAuthKey(s.stateDir); err != nil {
		writeErr(w, statusErr(400, "%s", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, AuthKeyStatus{Configured: false, RestartRequired: true})
}
