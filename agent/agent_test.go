package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestListStatSymlinkAndHidePartials(t *testing.T) {
	srv, ts, dir := newLocal(t, true)
	defer ts.Close()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "leak")); err != nil {
		t.Fatal(err)
	}
	// A partial must not show up in the listing.
	if err := os.WriteFile(filepath.Join(dir, "a.txt.tailfile-partial"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := localClient(ts)
	listing, err := c.List(context.Background(), "", dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Entries) != 3 {
		t.Fatalf("entries: %+v", listing.Entries)
	}
	if !listing.Entries[0].IsDir || listing.Entries[0].Name != "sub" {
		t.Fatalf("dirs should sort first, got %+v", listing.Entries[0])
	}
	st, err := c.Stat(context.Background(), "", filepath.Join(dir, "a.txt"))
	if err != nil || st.Size != 5 {
		t.Fatalf("stat %+v %v", st, err)
	}
	if _, err := c.Open(context.Background(), "", filepath.Join(dir, "leak"), 0); err == nil {
		t.Fatal("symlink escape should be forbidden")
	}
	_ = srv
}

func TestRangeRead(t *testing.T) {
	_, ts, dir := newLocal(t, false)
	defer ts.Close()
	body := []byte("0123456789abcdef")
	path := filepath.Join(dir, "nums.txt")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		rng  string
		want string
		code int
	}{
		{"", "0123456789abcdef", 200},
		{"bytes=0-3", "0123", 206},
		{"bytes=4-", "456789abcdef", 206},
		{"bytes=-4", "cdef", 206},
		{"bytes=100-200", "", 416},
	}
	for _, tc := range cases {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/fs/read?path="+url.QueryEscape(path), nil)
		req.Header.Set("Authorization", "Bearer "+testGUIToken)
		if tc.rng != "" {
			req.Header.Set("Range", tc.rng)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := readAll(resp)
		resp.Body.Close()
		if resp.StatusCode != tc.code {
			t.Fatalf("range %q status %d body %s", tc.rng, resp.StatusCode, got)
		}
		if tc.code < 300 && string(got) != tc.want {
			t.Fatalf("range %q got %q want %q", tc.rng, got, tc.want)
		}
	}
}

func TestResumableWriteAndBadHash(t *testing.T) {
	_, ts, dir := newLocal(t, true)
	defer ts.Close()
	dest := filepath.Join(dir, "out.bin")
	payload := bytes.Repeat([]byte("xyz"), 1500) // 4500 bytes, chunks of 1024
	c := &httpAPI{base: ts.URL, http: http.DefaultClient, token: testGUIToken}
	ctx := context.Background()
	up, err := c.StartUpload(ctx, dest, int64(len(payload)), 0, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.PutChunk(ctx, up.ID, 0, payload[:100], "deadbeef"); err == nil {
		t.Fatal("bad hash should fail")
	}
	st, err := c.Stat(ctx, dest)
	if err == nil {
		t.Fatalf("dest should not exist yet: %+v", st)
	}
	// Write two chunks, then resume the same upload id from the server offset.
	off := int64(0)
	for off < 2048 {
		n := 1024
		sum := sha256.Sum256(payload[off : off+int64(n)])
		off, err = c.PutChunk(ctx, up.ID, off, payload[off:off+int64(n)], hex.EncodeToString(sum[:]))
		if err != nil {
			t.Fatal(err)
		}
	}
	again, err := c.StartUpload(ctx, dest, int64(len(payload)), 0, up.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if again.Offset != 2048 || again.ID != up.ID {
		t.Fatalf("resume upload: %+v", again)
	}
	for again.Offset < int64(len(payload)) {
		end := again.Offset + 1024
		if end > int64(len(payload)) {
			end = int64(len(payload))
		}
		sum := sha256.Sum256(payload[again.Offset:end])
		next, err := c.PutChunk(ctx, again.ID, again.Offset, payload[again.Offset:end], hex.EncodeToString(sum[:]))
		if err != nil {
			t.Fatal(err)
		}
		again.Offset = next
	}
	sumAll := sha256.Sum256(payload)
	if err := c.FinishUpload(ctx, again.ID, hex.EncodeToString(sumAll[:])); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("len %d vs %d", len(got), len(payload))
	}
}

func TestWriteRequiresWriteRoot(t *testing.T) {
	_, ts, dir := newLocal(t, false)
	defer ts.Close()
	c := &httpAPI{base: ts.URL, http: http.DefaultClient, token: testGUIToken}
	_, err := c.StartUpload(context.Background(), filepath.Join(dir, "nope.txt"), 3, 0, "", false)
	if err == nil || !strings.Contains(err.Error(), "write") {
		t.Fatalf("expected read-only error, got %v", err)
	}
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/fs/list?path="+url.QueryEscape(dir), nil)
	req.Header.Set("Authorization", "Bearer "+testGUIToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("read should still work: %d", resp.StatusCode)
	}
}

func TestAuthzSameUserOnly(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newServer("n", "loopback", "ada@example.com", Roots{Read: []string{dir}}, 1024)
	s.allowPeerHeader = true
	s.peers = map[string]Identity{
		"ada": {Login: "ada@example.com", NodeName: "ada"},
		"bob": {Login: "bob@example.com", NodeName: "bob"},
	}
	s.whois = s.loopbackWhoIs
	ts := httptest.NewServer(s.TailnetHandler())
	defer ts.Close()
	for _, tc := range []struct {
		peer string
		code int
	}{
		{"bob", 403},
		{"ada", 200},
		{"", 401},
	} {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/fs/list?path="+url.QueryEscape(dir), nil)
		if tc.peer != "" {
			req.Header.Set("X-Tailfile-Loopback-Peer", tc.peer)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.code {
			t.Fatalf("peer %q status %d, want %d", tc.peer, resp.StatusCode, tc.code)
		}
	}
}

func TestPushCopyIsDirect(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	payload := bytes.Repeat([]byte("0123456789"), 800) // 8000
	src := filepath.Join(a, "big.bin")
	dst := filepath.Join(b, "big.bin")
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	lb := newPair(t, a, b, 1000)
	// Model a slow runner. The console's byte count must not depend on how long the copy takes.
	lb.lb.Node("alpha").jobs.hooks.AfterChunk = func(int) { time.Sleep(15 * time.Millisecond) }
	tr := mustTransfer(t, lb, TransferRequest{
		Mode: "push", FromNode: "alpha", FromPath: src, ToNode: "beta", ToPath: dst,
	})
	// Polling the console would add a job-status response to its byte count every
	// 15ms, so a slow copy looked like a relayed one. Wait on the runner instead,
	// then check the console view once.
	waitRunnerJob(t, lb, tr, jobCompleted)
	waitTransfer(t, lb.client, tr.ID, jobCompleted)
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("copied %d bytes", len(got))
	}
	if lb.lb.BytesFrom("console") >= int64(len(payload)/2) {
		t.Fatalf("orchestrator forwarded %d bytes; copy should be alpha->beta", lb.lb.BytesFrom("console"))
	}
	if lb.lb.BytesFrom("alpha") < int64(len(payload)) {
		t.Fatalf("alpha sent only %d bytes, want at least %d", lb.lb.BytesFrom("alpha"), len(payload))
	}
}

func TestPullCopy(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	payload := bytes.Repeat([]byte("pulled-by-destination"), 200)
	src := filepath.Join(a, "note.txt")
	dst := filepath.Join(b, "note.txt")
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	lb := newPair(t, a, b, 8)
	tr := mustTransfer(t, lb, TransferRequest{
		Mode: "pull", FromNode: "alpha", FromPath: src, ToNode: "beta", ToPath: dst,
	})
	waitTransfer(t, lb.client, tr.ID, jobCompleted)
	got, err := os.ReadFile(dst)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("got %q %v", got, err)
	}
	if lb.lb.BytesFrom("beta") < int64(len(payload)) {
		t.Fatalf("puller sent/received too little: %d", lb.lb.BytesFrom("beta"))
	}
}

func TestCopyResumeAfterInjectedFailure(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	payload := bytes.Repeat([]byte("R"), 5000)
	src := filepath.Join(a, "r.bin")
	dst := filepath.Join(b, "r.bin")
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	lb := newPair(t, a, b, 1000)
	lb.lb.Node("alpha").jobs.hooks.Retries = 0
	lb.lb.FailNthPatch("alpha", 2)
	tr := mustTransfer(t, lb, TransferRequest{
		Mode: "push", FromNode: "alpha", FromPath: src, ToNode: "beta", ToPath: dst,
	})
	failed := waitTransfer(t, lb.client, tr.ID, jobError)
	if failed.Job.BytesDone <= 0 || failed.Job.BytesDone >= int64(len(payload)) {
		t.Fatalf("expected a partial copy, got %+v", failed.Job)
	}
	if _, err := lb.client.ResumeTransfer(context.Background(), tr.ID); err != nil {
		t.Fatal(err)
	}
	waitTransfer(t, lb.client, tr.ID, jobCompleted)
	got, err := os.ReadFile(dst)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("resumed file %d %v", len(got), err)
	}
}

func TestCopyCancelAndResume(t *testing.T) {
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
	if _, err := lb.client.ResumeTransfer(context.Background(), tr.ID); err != nil {
		t.Fatal(err)
	}
	waitTransfer(t, lb.client, tr.ID, jobCompleted)
	got, err := os.ReadFile(dst)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("after resume %d %v", len(got), err)
	}
}

func TestDirectoryCopy(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(a, "dir", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a, "dir", "a.txt"), []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a, "dir", "sub", "b.txt"), []byte("BB"), 0o644); err != nil {
		t.Fatal(err)
	}
	lb := newPair(t, a, b, 64)
	tr := mustTransfer(t, lb, TransferRequest{
		Mode: "push", FromNode: "alpha", FromPath: filepath.Join(a, "dir"),
		ToNode: "beta", ToPath: filepath.Join(b, "dir"),
	})
	waitTransfer(t, lb.client, tr.ID, jobCompleted)
	got, err := os.ReadFile(filepath.Join(b, "dir", "sub", "b.txt"))
	if err != nil || string(got) != "BB" {
		t.Fatalf("tree copy: %q %v", got, err)
	}
}

func TestReadOnlyDestination(t *testing.T) {
	a, g := t.TempDir(), t.TempDir()
	src := filepath.Join(a, "a.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	lb, err := StartLoopback("ada@example.com", 128, []LoopSpec{
		{ID: "alpha", Read: []string{a}, Write: []string{a}},
		{ID: "gamma", Read: []string{g}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(lb.Close)
	ts := httptest.NewServer(lb.Handler())
	t.Cleanup(ts.Close)
	c := NewClient(ts.URL)
	c.Token = lb.Token
	tr, err := c.StartTransfer(context.Background(), TransferRequest{
		Mode: "push", FromNode: "alpha", FromPath: src,
		ToNode: "gamma", ToPath: filepath.Join(g, "a.txt"),
	})
	if err != nil {
		t.Fatal(err)
	}
	got := waitTransfer(t, c, tr.ID, jobError)
	if !strings.Contains(strings.ToLower(got.Job.Error), "write") && !strings.Contains(strings.ToLower(got.Job.Error), "read-only") {
		t.Fatalf("error %q", got.Job.Error)
	}
}

func TestMkdirRename(t *testing.T) {
	_, ts, dir := newLocal(t, true)
	defer ts.Close()
	c := localClient(ts)
	nested := filepath.Join(dir, "n", "m")
	if err := c.Mkdir(context.Background(), "", nested); err != nil {
		t.Fatal(err)
	}
	from := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(from, []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	to := filepath.Join(nested, "a.txt")
	if err := c.Rename(context.Background(), "", from, to); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(to); err != nil {
		t.Fatal(err)
	}
}

func TestPDFExtract(t *testing.T) {
	raw := simplePDF("Tailfile PDF preview")
	text := ExtractPDFText(raw, 1000)
	if !strings.Contains(text, "Tailfile PDF preview") {
		t.Fatalf("extracted %q", text)
	}
	if PreviewKind("a.PDF") != "pdf" || PreviewKind("a.png") != "image" || PreviewKind("a.md") != "text" {
		t.Fatal(PreviewKind("a.PDF"), PreviewKind("a.png"), PreviewKind("a.md"))
	}
}

func TestJoinRemote(t *testing.T) {
	if got := JoinRemote(`C:\Users\ada`, "file.txt"); got != "C:/Users/ada/file.txt" {
		t.Fatal(got)
	}
	if got := JoinRemote("/home/ada", "dir/file.txt"); got != "/home/ada/dir/file.txt" {
		t.Fatal(got)
	}
}

type pair struct {
	lb     *Loopback
	client *Client
}

func newPair(t *testing.T, a, b string, chunk int) pair {
	t.Helper()
	lb, err := StartLoopback("ada@example.com", chunk, []LoopSpec{
		{ID: "alpha", Read: []string{a}, Write: []string{a}},
		{ID: "beta", Read: []string{b}, Write: []string{b}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(lb.Close)
	ts := httptest.NewServer(lb.Handler())
	t.Cleanup(ts.Close)
	c := NewClient(ts.URL)
	c.Token = lb.Token
	return pair{lb: lb, client: c}
}

func mustTransfer(t *testing.T, p pair, req TransferRequest) Transfer {
	t.Helper()
	tr, err := p.client.StartTransfer(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func waitTransfer(t *testing.T, c *Client, id string, want ...string) Transfer {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	var last Transfer
	for time.Now().Before(deadline) {
		trs, err := c.Transfers(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, tr := range trs {
			if tr.ID != id {
				continue
			}
			last = tr
			for _, status := range want {
				if tr.Job.Status == status {
					return tr
				}
			}
			switch tr.Job.Status {
			case jobError, jobCanceled, jobCompleted, jobConflict:
				t.Fatalf("transfer %s stuck at %s (%s), want %v", id, tr.Job.Status, tr.Job.Error, want)
			}
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("transfer %s stuck at %s (%s), want %v", id, last.Job.Status, last.Job.Error, want)
	return last
}

// waitRunnerJob waits on the runner node's job directly, without traffic through the console.
func waitRunnerJob(t *testing.T, p pair, tr Transfer, want string) Job {
	t.Helper()
	node := p.lb.Node(tr.RunnerNode)
	if node == nil {
		t.Fatalf("unknown runner %q", tr.RunnerNode)
	}
	deadline := time.Now().Add(8 * time.Second)
	var last Job
	for time.Now().Before(deadline) {
		job, err := node.jobs.get(tr.Job.ID)
		if err != nil {
			t.Fatal(err)
		}
		last = job
		if job.Status == want {
			return job
		}
		switch job.Status {
		case jobError, jobCanceled, jobCompleted, jobConflict:
			t.Fatalf("job %s stuck at %s (%s), want %s", job.ID, job.Status, job.Error, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %s stuck at %s (%s), want %s", last.ID, last.Status, last.Error, want)
	return last
}

func newLocal(t *testing.T, write bool) (*Server, *httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	roots := Roots{Read: []string{dir}}
	if write {
		roots.Write = []string{dir}
	}
	s := newServer("local", "test", "ada@example.com", roots, 1024)
	s.guiToken = testGUIToken
	ts := httptest.NewServer(s.LocalHandler())
	return s, ts, dir
}

const testGUIToken = "test-gui-token"

func localClient(ts *httptest.Server) *Client {
	c := NewClient(ts.URL)
	c.Token = testGUIToken
	return c
}

func readAll(resp *http.Response) ([]byte, error) {
	return io.ReadAll(resp.Body)
}
