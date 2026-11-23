package agent

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDocFileLocalReadWriteAndEmpty(t *testing.T) {
	_, ts, dir := newLocal(t, true)
	defer ts.Close()
	c := localClient(ts)
	ctx := context.Background()

	p := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(p, []byte("initial"), 0o644); err != nil {
		t.Fatal(err)
	}

	st, raw, err := c.ReadFile(ctx, "", p, MaxEditableFileBytes)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "initial" || st.Size != 7 {
		t.Fatalf("read = %q (%+v)", raw, st)
	}

	// Exceed chunkSize (1024) in newLocal to exercise multi-chunk upload.
	updatedPayload := bytes.Repeat([]byte("line-0123456789\n"), 200)
	updated, err := c.WriteFile(ctx, WriteFileRequest{
		Path:              p,
		Data:              updatedPayload,
		ExpectedMtimeUnix: st.ModTime.UnixNano(),
		ExpectedSize:      st.Size,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Size != int64(len(updatedPayload)) {
		t.Fatalf("updated size = %d, want %d", updated.Size, len(updatedPayload))
	}
	onDisk, err := os.ReadFile(p)
	if err != nil || !bytes.Equal(onDisk, updatedPayload) {
		t.Fatalf("disk mismatch after write: err=%v len=%d", err, len(onDisk))
	}
	assertNoPartials(t, dir)

	// Truncate to empty, then edit back from 0 bytes using ExpectedSize == 0.
	emptyEnt, err := c.WriteFile(ctx, WriteFileRequest{
		Path:              p,
		Data:              nil,
		ExpectedMtimeUnix: updated.ModTime.UnixNano(),
		ExpectedSize:      updated.Size,
	})
	if err != nil {
		t.Fatal(err)
	}
	if emptyEnt.Size != 0 {
		t.Fatalf("empty size = %d, want 0", emptyEnt.Size)
	}
	if _, err := c.WriteFile(ctx, WriteFileRequest{
		Path:              p,
		Data:              []byte("after-empty"),
		ExpectedMtimeUnix: emptyEnt.ModTime.UnixNano(),
		ExpectedSize:      0,
	}); err != nil {
		t.Fatal(err)
	}
	onDisk, err = os.ReadFile(p)
	if err != nil || string(onDisk) != "after-empty" {
		t.Fatalf("after-empty = %q (%v)", onDisk, err)
	}
	assertNoPartials(t, dir)
}

func TestDocFileConflictDetectionAndForce(t *testing.T) {
	_, ts, dir := newLocal(t, true)
	defer ts.Close()
	c := localClient(ts)
	ctx := context.Background()

	p := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(p, []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, _, err := c.ReadFile(ctx, "", p, MaxEditableFileBytes)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate an external writer changing content and advancing mtime beyond coarse FS granularity.
	if err := os.WriteFile(p, []byte("external-v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := st.ModTime.Add(2 * time.Second)
	if err := os.Chtimes(p, future, future); err != nil {
		t.Fatal(err)
	}

	_, err = c.WriteFile(ctx, WriteFileRequest{
		Path:              p,
		Data:              []byte("editor-v2\n"),
		ExpectedMtimeUnix: st.ModTime.UnixNano(),
		ExpectedSize:      st.Size,
	})
	var ce *WriteConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("expected WriteConflictError, got %T: %v", err, err)
	}
	if ce.Code != "modified" || ce.Current.Size != int64(len("external-v2\n")) {
		t.Fatalf("unexpected conflict: %+v", ce)
	}
	onDisk, _ := os.ReadFile(p)
	if string(onDisk) != "external-v2\n" {
		t.Fatalf("conflict must not clobber disk: got %q", onDisk)
	}

	if _, err := c.WriteFile(ctx, WriteFileRequest{
		Path:  p,
		Data:  []byte("editor-v2\n"),
		Force: true,
	}); err != nil {
		t.Fatalf("force write failed: %v", err)
	}
	onDisk, _ = os.ReadFile(p)
	if string(onDisk) != "editor-v2\n" {
		t.Fatalf("force write mismatch: got %q", onDisk)
	}

	// Deleting the remote file after open must also surface as a conflict when preconditions are set.
	st2, _, err := c.ReadFile(ctx, "", p, MaxEditableFileBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	_, err = c.WriteFile(ctx, WriteFileRequest{
		Path:              p,
		Data:              []byte("resurrect"),
		ExpectedMtimeUnix: st2.ModTime.UnixNano(),
		ExpectedSize:      st2.Size,
	})
	ce = nil
	if !errors.As(err, &ce) || ce.Code != "deleted" {
		t.Fatalf("expected deleted WriteConflictError, got %+v (%v)", ce, err)
	}
	assertNoPartials(t, dir)
}

func TestDocFileCrossNodeSandboxAndReadOnly(t *testing.T) {
	alphaDir := t.TempDir()
	gammaDir := t.TempDir()
	outsideDir := t.TempDir()
	alphaFile := filepath.Join(alphaDir, "hello.txt")
	gammaFile := filepath.Join(gammaDir, "readme.txt")
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(alphaFile, []byte("from-alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gammaFile, []byte("from-gamma"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	lb, err := StartLoopback("ada@example.com", 512, []LoopSpec{
		{ID: "alpha", Read: []string{alphaDir}, Write: []string{alphaDir}, OS: "macos"},
		{ID: "gamma", Read: []string{gammaDir}, OS: "linux"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer lb.Close()

	ts := httptest.NewServer(lb.Handler())
	defer ts.Close()
	c := NewClient(ts.URL)
	c.Token = lb.Token
	ctx := context.Background()

	st, raw, err := c.ReadFile(ctx, "alpha", alphaFile, MaxEditableFileBytes)
	if err != nil || string(raw) != "from-alpha" {
		t.Fatalf("remote read = %q (%v)", raw, err)
	}
	if _, err := c.WriteFile(ctx, WriteFileRequest{
		Node:              "alpha",
		Path:              alphaFile,
		Data:              []byte("edited-remote"),
		ExpectedMtimeUnix: st.ModTime.UnixNano(),
		ExpectedSize:      st.Size,
	}); err != nil {
		t.Fatalf("remote write failed: %v", err)
	}
	got, _ := os.ReadFile(alphaFile)
	if string(got) != "edited-remote" {
		t.Fatalf("alpha disk = %q", got)
	}

	// Read-only node gamma refuses write with 403.
	gSt, _, err := c.ReadFile(ctx, "gamma", gammaFile, MaxEditableFileBytes)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.WriteFile(ctx, WriteFileRequest{
		Node:              "gamma",
		Path:              gammaFile,
		Data:              []byte("hacked"),
		ExpectedMtimeUnix: gSt.ModTime.UnixNano(),
		ExpectedSize:      gSt.Size,
	})
	var se *StatusError
	if !errors.As(err, &se) || se.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on read-only node, got %v", err)
	}
	if gDisk, _ := os.ReadFile(gammaFile); string(gDisk) != "from-gamma" {
		t.Fatalf("gamma disk modified: %q", gDisk)
	}

	// Outside root and symlink escape both fail with 403 and leave outsideFile intact.
	if _, err := c.WriteFile(ctx, WriteFileRequest{
		Node:         "alpha",
		Path:         outsideFile,
		Data:         []byte("pwned"),
		ExpectedSize: -1,
	}); !errors.As(err, &se) || se.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for outside path, got %v", err)
	}
	link := filepath.Join(alphaDir, "escape-link")
	if err := os.Symlink(outsideFile, link); err == nil {
		if _, err := c.WriteFile(ctx, WriteFileRequest{
			Node:         "alpha",
			Path:         link,
			Data:         []byte("pwned"),
			ExpectedSize: -1,
			Force:        true,
		}); !errors.As(err, &se) || se.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for symlink escape, got %v", err)
		}
	}
	if sec, _ := os.ReadFile(outsideFile); string(sec) != "secret" {
		t.Fatalf("outside file mutated: %q", sec)
	}

	// TailnetHandler must not expose the local-only /v1/fs/write route.
	peerURL, err := lb.Orch.resolver.DialURL("alpha")
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPut, peerURL+"/v1/fs/write?path="+alphaFile, strings.NewReader("x"))
	req.Header.Set("X-Tailfile-Loopback-Peer", "console")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("tailnet handler exposed /v1/fs/write with status %d", resp.StatusCode)
	}
}

func TestDocFileSizeLimitDirectoryAndPermissions(t *testing.T) {
	_, ts, dir := newLocal(t, true)
	defer ts.Close()
	c := localClient(ts)
	ctx := context.Background()

	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.ReadFile(ctx, "", sub, MaxEditableFileBytes); err == nil {
		t.Fatal("expected error reading directory")
	}
	if _, err := c.WriteFile(ctx, WriteFileRequest{Path: sub, Data: []byte("x"), ExpectedSize: -1, Force: true}); err == nil {
		t.Fatal("expected error writing directory")
	}

	bigPath := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(bigPath, bytes.Repeat([]byte("a"), int(MaxEditableFileBytes)+1), 0o644); err != nil {
		t.Fatal(err)
	}
	var se *StatusError
	if _, _, err := c.ReadFile(ctx, "", bigPath, MaxEditableFileBytes); !errors.As(err, &se) || se.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 on ReadFile, got %v", err)
	}
	if _, err := c.WriteFile(ctx, WriteFileRequest{
		Path:         filepath.Join(dir, "too-big.txt"),
		Data:         bytes.Repeat([]byte("b"), int(MaxEditableFileBytes)+1),
		ExpectedSize: -1,
		Force:        true,
	}); !errors.As(err, &se) || se.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 on WriteFile, got %v", err)
	}

	if runtime.GOOS != "windows" {
		for _, mode := range []os.FileMode{0o755, 0o600} {
			p := filepath.Join(dir, "mode.sh")
			if err := os.WriteFile(p, []byte("echo 1\n"), mode); err != nil {
				t.Fatal(err)
			}
			if _, err := c.WriteFile(ctx, WriteFileRequest{
				Path:         p,
				Data:         []byte("echo 2\n"),
				ExpectedSize: -1,
				Force:        true,
			}); err != nil {
				t.Fatal(err)
			}
			st, err := os.Stat(p)
			if err != nil {
				t.Fatal(err)
			}
			if got := st.Mode().Perm(); got != mode {
				t.Fatalf("mode after overwrite = %o, want %o", got, mode)
			}
			_ = os.Remove(p)
		}
	}
}

func assertNoPartials(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if isPartialName(e.Name()) {
			t.Fatalf("stray partial file left behind: %s", e.Name())
		}
	}
}
