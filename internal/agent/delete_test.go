package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeleteFileAndDir(t *testing.T) {
	_, ts, dir := newLocal(t, true)
	defer ts.Close()
	c := localClient(ts)

	file := filepath.Join(dir, "gone.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(dir, "box", "inner.txt")
	if err := os.MkdirAll(filepath.Dir(nested), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nested, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := c.Delete(context.Background(), "", file); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file still there: %v", err)
	}
	if err := c.Delete(context.Background(), "", filepath.Join(dir, "box")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "box")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dir still there: %v", err)
	}
}

func TestDeleteRefusesRootReadonlyTraversalAndAuth(t *testing.T) {
	s, ts, dir := newLocal(t, true)
	defer ts.Close()
	c := localClient(ts)

	if err := c.Delete(context.Background(), "", dir); err == nil || !strings.Contains(err.Error(), "configured root") {
		t.Fatalf("want root refusal, got %v", err)
	}

	// readonly server
	ro, roTS, roDir := newLocal(t, false)
	defer roTS.Close()
	_ = ro
	roClient := localClient(roTS)
	target := filepath.Join(roDir, "nope.txt")
	if err := os.WriteFile(target, []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := roClient.Delete(context.Background(), "", target); err == nil || !strings.Contains(strings.ToLower(err.Error()), "write") {
		t.Fatalf("want readonly, got %v", err)
	}

	// traversal
	outside := filepath.Join(filepath.Dir(dir), "outside-secret")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })
	evil := filepath.Join(dir, "..", filepath.Base(outside))
	if err := c.Delete(context.Background(), "", evil); err == nil {
		t.Fatal("traversal delete should fail")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside file should remain: %v", err)
	}

	// symlink escape
	escape := filepath.Join(dir, "link-out")
	if err := os.Symlink(outside, escape); err != nil {
		t.Skip("symlink not available")
	}
	if err := c.Delete(context.Background(), "", escape); err == nil {
		// Resolve may treat symlink target outside as forbidden before delete
		t.Fatal("symlink escape delete should fail")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("symlink target must remain: %v", err)
	}

	// auth required
	body, _ := json.Marshal(map[string]string{"path": filepath.Join(dir, "x")})
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/fs/delete", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("unauth status %d (%s)", resp.StatusCode, b)
	}
	_ = s
}

func TestDeleteViaNodeProxy(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	src := filepath.Join(a, "kill-me.txt")
	if err := os.WriteFile(src, []byte("bye"), 0o644); err != nil {
		t.Fatal(err)
	}
	lb := newPair(t, a, b, 1024)
	if err := lb.client.Delete(context.Background(), "alpha", src); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("still there: %v", err)
	}
}
