package agent

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// CacheDirFile holds a user-chosen cache directory inside the state
// directory. Missing or empty means the default from DefaultCacheDir.
const CacheDirFile = "cache-dir"

// initCacheDir sets the default cache directory and applies a saved override.
// Call it after attachState so the state directory is known.
func (s *Server) initCacheDir(def string) {
	s.mu.Lock()
	s.cacheDefault = def
	s.cacheDir = def
	s.cacheCustom = false
	dir := s.stateDir
	s.mu.Unlock()
	if dir == "" {
		return
	}
	raw, err := os.ReadFile(filepath.Join(dir, CacheDirFile))
	if err != nil {
		return
	}
	custom, err := cleanCacheDir(string(raw))
	if err != nil || custom == "" {
		if err != nil {
			log.Printf("tailfile: ignoring saved cache directory: %v", err)
		}
		return
	}
	s.mu.Lock()
	s.cacheDir = custom
	s.cacheCustom = custom != def
	s.mu.Unlock()
}

// cacheTarget returns the cache directory in effect and whether the user
// chose it. A user-chosen directory keeps its own permissions.
func (s *Server) cacheTarget() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cacheDir, s.cacheCustom
}

func (s *Server) currentCacheDir() string {
	dir, _ := s.cacheTarget()
	return dir
}

// cleanCacheDir validates a cache directory path. "" means use the default.
func cleanCacheDir(raw string) (string, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		return "", nil
	}
	if strings.ContainsAny(p, "\r\n\x00") {
		return "", errors.New("cache directory must be a single line")
	}
	if strings.HasPrefix(p, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.New("cannot resolve ~")
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	if !filepath.IsAbs(p) {
		return "", errors.New("cache directory must be an absolute path")
	}
	return filepath.Clean(p), nil
}

// setCacheDir switches the cache directory and saves the choice.
// "" (or the default itself) restores the default.
func (s *Server) setCacheDir(raw string) (string, error) {
	p, err := cleanCacheDir(raw)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	def := s.cacheDefault
	state := s.stateDir
	s.mu.Unlock()
	if p == "" {
		p = def
	}
	if p == "" {
		return "", errors.New("no default cache directory")
	}
	custom := p != def
	if custom {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return "", errors.New("cache directory is a file")
		}
		if err := os.MkdirAll(p, 0o700); err != nil {
			return "", errors.New("could not create cache directory")
		}
	}
	if state != "" {
		path := filepath.Join(state, CacheDirFile)
		if custom {
			if err := writeSecret(path, []byte(p+"\n")); err != nil {
				return "", errors.New("could not save cache directory")
			}
		} else if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", errors.New("could not save cache directory")
		}
	}
	s.mu.Lock()
	s.cacheDir = p
	s.cacheCustom = custom
	s.mu.Unlock()
	log.Printf("tailfile: drag-out cache now %s", p)
	return p, nil
}

func (s *Server) handleCacheDirPut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CacheDir string `json:"cacheDir"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&body); err != nil {
		writeErr(w, statusErr(400, "bad json"))
		return
	}
	if _, err := s.setCacheDir(body.CacheDir); err != nil {
		writeErr(w, statusErr(400, "%s", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, s.currentSettings())
}

// handleCacheDirOpen opens the cache directory in the system file manager.
// It takes no path, so a caller cannot use it to open anything else.
func (s *Server) handleCacheDirOpen(w http.ResponseWriter, r *http.Request) {
	dir := s.currentCacheDir()
	if dir == "" {
		writeErr(w, statusErr(500, "no cache directory"))
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		writeErr(w, statusErr(500, "could not create cache directory"))
		return
	}
	if err := openDir(dir); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func openDir(dir string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", dir).Start()
	case "windows":
		return exec.Command("explorer", dir).Start()
	default:
		return exec.Command("xdg-open", dir).Start()
	}
}
