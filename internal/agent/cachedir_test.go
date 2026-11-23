package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func putCacheDir(t *testing.T, base, dir string) (int, Settings) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"cacheDir": dir})
	req, err := http.NewRequest(http.MethodPut, base+"/v1/local/settings/cachedir", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testGUIToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var st Settings
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(raw, &st); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode, st
}

func TestCacheDirSettingPersistsAndResets(t *testing.T) {
	state := t.TempDir()
	def := filepath.Join(t.TempDir(), "default")
	custom := filepath.Join(t.TempDir(), "mine")

	s := newServer("local", "test", "ada@example.com", Roots{}, 1024)
	s.stateDir = state
	s.guiToken = testGUIToken
	s.initCacheDir(def)
	ts := httptest.NewServer(s.LocalHandler())
	defer ts.Close()

	var st Settings
	if err := json.Unmarshal([]byte(settingsBody(t, ts.URL)), &st); err != nil {
		t.Fatal(err)
	}
	if st.CacheDir != def || st.CacheDirDefault != def {
		t.Fatalf("initial settings %+v", st)
	}

	for _, bad := range []string{"relative/dir", "/tmp/a\nb"} {
		if code, _ := putCacheDir(t, ts.URL, bad); code != http.StatusBadRequest {
			t.Fatalf("%q accepted with %d", bad, code)
		}
	}
	afile := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(afile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _ := putCacheDir(t, ts.URL, afile); code != http.StatusBadRequest {
		t.Fatalf("file accepted as cache dir: %d", code)
	}

	code, st := putCacheDir(t, ts.URL, custom+"/")
	if code != http.StatusOK || st.CacheDir != custom || st.CacheDirDefault != def {
		t.Fatalf("set custom %d %+v", code, st)
	}
	if info, err := os.Stat(custom); err != nil || !info.IsDir() {
		t.Fatalf("custom dir not created: %v", err)
	}
	if dir, isCustom := s.cacheTarget(); dir != custom || !isCustom {
		t.Fatalf("target %s %v", dir, isCustom)
	}

	// A restart picks the saved choice back up.
	again := newServer("local", "test", "ada@example.com", Roots{}, 1024)
	again.stateDir = state
	again.initCacheDir(def)
	if got := again.currentCacheDir(); got != custom {
		t.Fatalf("after restart %s", got)
	}

	code, st = putCacheDir(t, ts.URL, "")
	if code != http.StatusOK || st.CacheDir != def {
		t.Fatalf("reset %d %+v", code, st)
	}
	if _, err := os.Stat(filepath.Join(state, CacheDirFile)); !os.IsNotExist(err) {
		t.Fatalf("reset left %s behind: %v", CacheDirFile, err)
	}
}

func TestCacheDirCustomKeepsPermissions(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("AAA"), 0o644); err != nil {
		t.Fatal(err)
	}
	remote := newServer("n", "loopback", "ada@example.com", Roots{Read: []string{root}}, 1024)
	remote.whois = func(*http.Request) (Identity, error) {
		return Identity{Login: "ada@example.com"}, nil
	}
	ts := httptest.NewServer(remote.TailnetHandler())
	defer ts.Close()

	custom := filepath.Join(t.TempDir(), "shared")
	if err := os.MkdirAll(custom, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(custom, 0o755); err != nil {
		t.Fatal(err)
	}
	s := newServer("local", "test", "ada@example.com", Roots{}, 1024)
	s.initCacheDir(filepath.Join(t.TempDir(), "default"))
	if _, err := s.setCacheDir(custom); err != nil {
		t.Fatal(err)
	}
	s.resolver = fixedResolver{id: "n", url: ts.URL, api: &httpAPI{base: ts.URL}}
	res, err := s.cache(t.Context(), CacheRequest{Node: "n", Path: filepath.Join(root, "note.txt")})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(res.LocalPath) != custom {
		t.Fatalf("cached at %s", res.LocalPath)
	}
	if info, err := os.Stat(custom); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o755) {
		t.Fatalf("custom dir mode changed: %v %v", info.Mode().Perm(), err)
	}
}
