package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalRequiresTokenAndLoopbackHost(t *testing.T) {
	s, ts, _ := newLocal(t, true)
	defer ts.Close()

	do := func(method, origin, host, token string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(method, ts.URL+"/v1/health", nil)
		if err != nil {
			t.Fatal(err)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if host != "" {
			req.Host = host
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	mustCode := func(resp *http.Response, code int) {
		t.Helper()
		defer resp.Body.Close()
		if resp.StatusCode != code {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("status %d, want %d (%s)", resp.StatusCode, code, bytes.TrimSpace(b))
		}
	}

	mustCode(do(http.MethodGet, "", "", ""), 401)
	mustCode(do(http.MethodGet, "", "", "nope"), 401)
	mustCode(do(http.MethodGet, "http://evil.example", "", testGUIToken), 403)
	mustCode(do(http.MethodGet, "http://127.0.0.1:80", "", testGUIToken), 403)

	ok := do(http.MethodGet, "http://127.0.0.1:17431", "", testGUIToken)
	if got := ok.Header.Get("Access-Control-Allow-Origin"); got != "http://127.0.0.1:17431" {
		t.Fatalf("ACAO %q", got)
	}
	if strings.Contains(ok.Header.Get("Access-Control-Allow-Origin"), "*") {
		t.Fatal("wildcard origin")
	}
	mustCode(ok, 200)

	wails := do(http.MethodGet, "wails://wails", "", testGUIToken)
	if wails.Header.Get("Access-Control-Allow-Origin") != "wails://wails" {
		t.Fatalf("wails ACAO %q", wails.Header.Get("Access-Control-Allow-Origin"))
	}
	mustCode(wails, 200)

	mustCode(do(http.MethodGet, "", "rebind.example", testGUIToken), 403)
	mustCode(do(http.MethodOptions, "http://127.0.0.1:17431", "", ""), 204)
	mustCode(do(http.MethodOptions, "http://evil.example", "", ""), 403)

	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	req.RemoteAddr = "8.8.8.8:1234"
	req.Host = "127.0.0.1:8645"
	req.Header.Set("Authorization", "Bearer "+testGUIToken)
	rr := httptest.NewRecorder()
	s.LocalHandler().ServeHTTP(rr, req)
	if rr.Code != 403 {
		t.Fatalf("non-loopback remote %d", rr.Code)
	}
}

func TestImportRespectsReadRoots(t *testing.T) {
	read := t.TempDir()
	write := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(read, "ok.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "no.txt"), []byte("no"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(read, "leak")); err != nil {
		t.Fatal(err)
	}
	s := newServer("box", "test", "ada@example.com", Roots{Read: []string{read}, Write: []string{write}}, 1024)
	s.guiToken = testGUIToken
	s.remoteOverride = func(string) (NodeAPI, error) { return directAPI{s: s}, nil }
	s.resolver = fixedResolver{id: "box", url: "http://self"}
	ts := httptest.NewServer(s.LocalHandler())
	defer ts.Close()
	c := localClient(ts)

	ctx := context.Background()
	job, err := c.Import(ctx, ImportRequest{
		Node: "box", DestDir: write, Paths: []string{filepath.Join(read, "ok.txt")},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, s, job.ID, jobCompleted)
	got, err := os.ReadFile(filepath.Join(write, "ok.txt"))
	if err != nil || string(got) != "ok" {
		t.Fatalf("imported %q %v", got, err)
	}
	if _, err := c.Import(ctx, ImportRequest{
		Node: "box", DestDir: write, Paths: []string{filepath.Join(outside, "no.txt")},
	}); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("outside import: %v", err)
	}
	if _, err := c.Import(ctx, ImportRequest{
		Node: "box", DestDir: write, Paths: []string{filepath.Join(read, "leak")},
	}); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("symlink import: %v", err)
	}
}

func TestDefaultReadRootIsTailfile(t *testing.T) {
	home := t.TempDir()
	dir, err := ensureShareDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dir) != "Tailfile" {
		t.Fatal(dir)
	}
	st, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o700 {
		t.Fatalf("mode %o", st.Mode().Perm())
	}
	prev := userHomeDir
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = prev })

	cfg := Config{Hostname: "t", StateDir: t.TempDir(), LocalAddr: "127.0.0.1:9"}
	if err := cfg.applyDefaults(); err != nil {
		t.Fatal(err)
	}
	if len(cfg.ReadRoots) != 1 || cfg.ReadRoots[0] != dir {
		t.Fatalf("roots %+v", cfg.ReadRoots)
	}
	demo := Config{Demo: true, Hostname: "t", StateDir: t.TempDir(), LocalAddr: "127.0.0.1:9"}
	if err := demo.applyDefaults(); err != nil {
		t.Fatal(err)
	}
	if len(demo.ReadRoots) != 0 {
		t.Fatalf("demo roots %+v", demo.ReadRoots)
	}
}

func TestListenRejectsNonLoopback(t *testing.T) {
	if err := requireLoopbackAddr("0.0.0.0:8645"); err == nil {
		t.Fatal("0.0.0.0 should be rejected")
	}
	if err := requireLoopbackAddr(":8645"); err == nil {
		t.Fatal("empty host should be rejected")
	}
	if err := requireLoopbackAddr("127.0.0.1:8645"); err != nil {
		t.Fatal(err)
	}
	if err := requireLoopbackAddr("localhost:8645"); err != nil {
		t.Fatal(err)
	}
}

func TestResolveOwnerTaggedDevice(t *testing.T) {
	if _, err := ResolveOwner("tagged-devices", false, ""); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := ResolveOwner("ada@example.com", true, ""); err == nil || !strings.Contains(err.Error(), "-owner") {
		t.Fatalf("tagged node: %v", err)
	}
	got, err := ResolveOwner("tagged-devices", true, "ada@example.com")
	if err != nil || got != "ada@example.com" {
		t.Fatalf("configured owner %q %v", got, err)
	}
	got, err = ResolveOwner("ada@example.com", false, "")
	if err != nil || got != "ada@example.com" {
		t.Fatalf("human owner %q %v", got, err)
	}
}

func TestGrantNarrowsRoots(t *testing.T) {
	parent := t.TempDir()
	sub := filepath.Join(parent, "a")
	sib := filepath.Join(parent, "b")
	outside := t.TempDir()
	for _, dir := range []string{sub, sib, outside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(sub, "f.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sib, "f.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	grant := []byte(fmt.Sprintf(`{"read":[%q],"write":[%q]}`, sub, sub))
	wide := []byte(fmt.Sprintf(`{"read":[%q,%q]}`, sub, outside))
	s := newServer("n", "loopback", "ada@example.com", Roots{
		Read:  []string{parent},
		Write: []string{parent},
	}, 1024)
	s.allowPeerHeader = true
	s.peers = map[string]Identity{
		"ada":  {Login: "ada@example.com", NodeName: "ada"},
		"bob":  {Login: "bob@example.com", NodeName: "bob", GrantPresent: true, GrantRaw: [][]byte{grant}},
		"cara": {Login: "cara@example.com", NodeName: "cara", GrantPresent: true, GrantRaw: [][]byte{wide}},
		"bad":  {Login: "bad@example.com", NodeName: "bad", GrantPresent: true, GrantRaw: [][]byte{[]byte(`{`)}},
	}
	s.whois = s.loopbackWhoIs
	ts := httptest.NewServer(s.TailnetHandler())
	defer ts.Close()

	code := func(peer, path string) int {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/fs/list?path="+path, nil)
		req.Header.Set("X-Tailfile-Loopback-Peer", peer)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code("ada", sib) != 200 {
		t.Fatal("same user without a grant should keep the configured roots")
	}
	if code("bob", sub) != 200 {
		t.Fatal("grant should allow the other user inside the granted root")
	}
	if code("bob", sib) != 403 {
		t.Fatalf("grant should hide the sibling, got %d", code("bob", sib))
	}
	if code("cara", outside) != 403 {
		t.Fatal("a grant must not widen past the node roots")
	}
	if code("cara", sub) != 200 {
		t.Fatal("cara should still read the overlapping root")
	}
	if code("bad", sub) != 400 {
		t.Fatalf("bad grant json: %d", code("bad", sub))
	}

	up, err := (&httpAPI{base: ts.URL, http: &http.Client{Transport: grantRT{peer: "bob"}}}).StartUpload(
		context.Background(), filepath.Join(sub, "new.txt"), 1, 0, "",
	)
	if err != nil || up.ID == "" {
		t.Fatalf("granted write: %+v %v", up, err)
	}
	_, err = (&httpAPI{base: ts.URL, http: &http.Client{Transport: grantRT{peer: "bob"}}}).StartUpload(
		context.Background(), filepath.Join(sib, "new.txt"), 1, 0, "",
	)
	if err == nil {
		t.Fatal("write outside the grant should fail")
	}
}

func TestFinishRejectsBadSHA256(t *testing.T) {
	_, ts, dir := newLocal(t, true)
	defer ts.Close()
	dest := filepath.Join(dir, "out.bin")
	payload := []byte("abcdef")
	c := &httpAPI{base: ts.URL, http: http.DefaultClient, token: testGUIToken}
	ctx := context.Background()
	up, err := c.StartUpload(ctx, dest, int64(len(payload)), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256Hex(payload)
	if _, err := c.PutChunk(ctx, up.ID, 0, payload, sum); err != nil {
		t.Fatal(err)
	}
	err = c.FinishUpload(ctx, up.ID, strings.Repeat("ab", 32))
	if err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("finish: %v", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("dest exists after bad hash: %v", err)
	}
	if _, err := os.Stat(dest + ".tailfile-partial"); !os.IsNotExist(err) {
		t.Fatalf("partial left behind: %v", err)
	}
	up, err = c.StartUpload(ctx, dest, int64(len(payload)), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.PutChunk(ctx, up.ID, 0, payload, sum); err != nil {
		t.Fatal(err)
	}
	if err := c.FinishUpload(ctx, up.ID, sum); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestResumeRestartsWhenSourceChanges(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	payload := bytes.Repeat([]byte("C"), 8000)
	src := filepath.Join(a, "c.bin")
	dst := filepath.Join(b, "c.bin")
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	lb := newPair(t, a, b, 1000)
	lb.lb.Node("alpha").jobs.hooks.AfterChunk = func(n int) {
		if n == 1 {
			for _, job := range lb.lb.Node("alpha").jobs.list() {
				_, _ = lb.lb.Node("alpha").jobs.cancel(job.ID)
			}
		}
	}
	tr := mustTransfer(t, lb, TransferRequest{
		Mode: "push", FromNode: "alpha", FromPath: src, ToNode: "beta", ToPath: dst,
	})
	waitTransfer(t, lb.client, tr.ID, jobCanceled)
	lb.lb.Node("alpha").jobs.hooks.AfterChunk = nil

	changed := bytes.Repeat([]byte("D"), 8000)
	if err := os.WriteFile(src, changed, 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(src, future, future); err != nil {
		t.Fatal(err)
	}
	if _, err := lb.client.ResumeTransfer(context.Background(), tr.ID); err != nil {
		t.Fatal(err)
	}
	waitTransfer(t, lb.client, tr.ID, jobCompleted)
	got, err := os.ReadFile(dst)
	if err != nil || !bytes.Equal(got, changed) {
		t.Fatalf("restarted copy %d bytes, err %v", len(got), err)
	}
}

func TestJobListSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	state := t.TempDir()
	src := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(src, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newServer("n", "test", "ada@example.com", Roots{Read: []string{dir}, Write: []string{dir}}, 1024)
	if err := s.attachState(state); err != nil {
		t.Fatal(err)
	}
	job, err := s.jobs.start(context.Background(), JobRequest{
		Mode: "push", SrcPath: src, DestPath: filepath.Join(dir, "out.txt"),
		DestBaseURL: "http://127.0.0.1:1",
	})
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, s, job.ID, jobError)
	st, err := os.Stat(filepath.Join(state, "jobs.json"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("jobs.json mode %o", st.Mode().Perm())
	}

	s.tracks["t1"] = transferRec{
		ID: "t1", Runner: "n", RemoteID: job.ID,
		Req: TransferRequest{Mode: "push", FromNode: "n", FromPath: src, ToNode: "n", ToPath: filepath.Join(dir, "out.txt")},
	}
	s.saveTracks()

	s2 := newServer("n", "test", "ada@example.com", Roots{Read: []string{dir}, Write: []string{dir}}, 1024)
	if err := s2.attachState(state); err != nil {
		t.Fatal(err)
	}
	got, err := s2.jobs.get(job.ID)
	if err != nil || got.SrcPath != src || got.Status != jobError {
		t.Fatalf("reloaded %+v %v", got, err)
	}
	s2.trackMu.Lock()
	_, ok := s2.tracks["t1"]
	s2.trackMu.Unlock()
	if !ok {
		t.Fatal("transfer record did not survive restart")
	}

	raw, err := os.ReadFile(filepath.Join(state, "jobs.json"))
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.Replace(raw, []byte(`"status": "error"`), []byte(`"status": "running"`), 1)
	if err := os.WriteFile(filepath.Join(state, "jobs.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	s3 := newServer("n", "test", "ada@example.com", Roots{Read: []string{dir}, Write: []string{dir}}, 1024)
	if err := s3.attachState(state); err != nil {
		t.Fatal(err)
	}
	loaded, err := s3.jobs.get(job.ID)
	if err != nil || loaded.Status != jobInterrupted {
		t.Fatalf("interrupted %+v %v", loaded, err)
	}
	if _, err := s3.jobs.resume(job.ID); err != nil {
		t.Fatal(err)
	}
}

func TestCacheKeySeparatesSameNames(t *testing.T) {
	root := t.TempDir()
	left := filepath.Join(root, "left")
	right := filepath.Join(root, "right")
	if err := os.MkdirAll(left, 0o755); err != nil || os.MkdirAll(right, 0o755) != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(left, "note.txt"), []byte("AAA"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(right, "note.txt"), []byte("BBB"), 0o644); err != nil {
		t.Fatal(err)
	}
	remote := newServer("n", "loopback", "ada@example.com", Roots{Read: []string{root}}, 1024)
	remote.whois = func(*http.Request) (Identity, error) {
		return Identity{Login: "ada@example.com"}, nil
	}
	ts := httptest.NewServer(remote.TailnetHandler())
	defer ts.Close()

	s := newServer("local", "test", "ada@example.com", Roots{}, 1024)
	s.cacheDir = t.TempDir()
	s.resolver = fixedResolver{id: "n", url: ts.URL, api: &httpAPI{base: ts.URL}}
	ctx := context.Background()
	one, err := s.cache(ctx, CacheRequest{Node: "n", Path: filepath.Join(left, "note.txt")})
	if err != nil {
		t.Fatal(err)
	}
	two, err := s.cache(ctx, CacheRequest{Node: "n", Path: filepath.Join(right, "note.txt")})
	if err != nil {
		t.Fatal(err)
	}
	if one.LocalPath == two.LocalPath {
		t.Fatalf("same-name files collided at %s", one.LocalPath)
	}
	if filepath.Dir(one.LocalPath) != s.cacheDir || filepath.Dir(two.LocalPath) != s.cacheDir {
		t.Fatalf("cache paths %s %s", one.LocalPath, two.LocalPath)
	}
	for _, res := range []CacheResult{one, two} {
		st, err := os.Stat(res.LocalPath)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("mode %o", st.Mode().Perm())
		}
	}
	abody, _ := os.ReadFile(one.LocalPath)
	bbody, _ := os.ReadFile(two.LocalPath)
	if string(abody) != "AAA" || string(bbody) != "BBB" {
		t.Fatalf("contents %q %q", abody, bbody)
	}
	if st, err := os.Stat(s.cacheDir); err != nil || st.Mode().Perm() != 0o700 {
		t.Fatalf("cache dir mode %v %v", st, err)
	}
}

type fixedResolver struct {
	id  string
	url string
	api NodeAPI
}

func (f fixedResolver) Nodes(context.Context) ([]NodeInfo, error) { return nil, nil }

func (f fixedResolver) API(context.Context, string) (NodeAPI, error) {
	if f.api != nil {
		return f.api, nil
	}
	return nil, statusErr(404, "no api")
}

func (f fixedResolver) DialURL(string) (string, error) { return f.url, nil }

type grantRT struct{ peer string }

func (g grantRT) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("X-Tailfile-Loopback-Peer", g.peer)
	return http.DefaultTransport.RoundTrip(req)
}

func waitJob(t *testing.T, s *Server, id, want string) Job {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	var last Job
	for time.Now().Before(deadline) {
		job, err := s.jobs.get(id)
		if err != nil {
			t.Fatal(err)
		}
		last = job
		if job.Status == want {
			return job
		}
		if job.Status == jobError || job.Status == jobCanceled || job.Status == jobCompleted {
			if job.Status != want {
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("job %s stuck at %s (%s), want %s", id, last.Status, last.Error, want)
	return last
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
