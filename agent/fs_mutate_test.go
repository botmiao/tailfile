package agent

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMkdirRenameSecurityAndSuccess(t *testing.T) {
	_, ts, dir := newLocal(t, true)
	defer ts.Close()
	c := localClient(ts)

	nested := filepath.Join(dir, "a", "b")
	if err := c.Mkdir(context.Background(), "", nested); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(nested)
	if err != nil || !st.IsDir() {
		t.Fatalf("mkdir: %v %#v", err, st)
	}

	outside := filepath.Join(filepath.Dir(dir), "mkdir-escape")
	t.Cleanup(func() { _ = os.RemoveAll(outside) })
	evil := filepath.Join(dir, "..", filepath.Base(outside))
	if err := c.Mkdir(context.Background(), "", evil); err == nil {
		t.Fatal("mkdir traversal should fail")
	}
	if _, err := os.Stat(outside); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outside must not exist: %v", err)
	}

	src := filepath.Join(dir, "n.txt")
	if err := os.WriteFile(src, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(nested, "n.txt")
	if err := c.Rename(context.Background(), "", src, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatal(err)
	}

	// rename into existing -> 409
	if err := os.WriteFile(src, []byte("2"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.Rename(context.Background(), "", src, dst); err == nil || !strings.Contains(err.Error(), "exists") {
		t.Fatalf("want exists conflict, got %v", err)
	}

	// readonly
	_, roTS, roDir := newLocal(t, false)
	defer roTS.Close()
	ro := localClient(roTS)
	if err := ro.Mkdir(context.Background(), "", filepath.Join(roDir, "nope")); err == nil || !strings.Contains(strings.ToLower(err.Error()), "write") {
		t.Fatalf("readonly mkdir: %v", err)
	}
	file := filepath.Join(roDir, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ro.Rename(context.Background(), "", file, filepath.Join(roDir, "g.txt")); err == nil || !strings.Contains(strings.ToLower(err.Error()), "write") {
		t.Fatalf("readonly rename: %v", err)
	}
}

func TestMkdirRenameRequireAuth(t *testing.T) {
	_, ts, dir := newLocal(t, true)
	defer ts.Close()
	body := strings.NewReader(`{"path":"` + filepath.Join(dir, "x") + `"}`)
	resp, err := httpPostNoAuth(ts.URL+"/v1/fs/mkdir", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("mkdir unauth %d", resp.StatusCode)
	}
}

// httpPostNoAuth is a tiny helper for auth-required checks.
func httpPostNoAuth(url string, body *strings.Reader) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return http.DefaultClient.Do(req)
}

func TestMkdirRenameSymlinkEscapeAndAuth(t *testing.T) {
	_, ts, dir := newLocal(t, true)
	defer ts.Close()
	c := localClient(ts)

	outside := filepath.Join(filepath.Dir(dir), "mutate-secret")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })

	link := filepath.Join(dir, "out-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip("symlink not available")
	}
	evilMk := filepath.Join(link, "nested")
	if err := c.Mkdir(context.Background(), "", evilMk); err == nil {
		t.Fatal("mkdir through outside symlink should fail")
	}

	src := filepath.Join(dir, "move-me.txt")
	if err := os.WriteFile(src, []byte("m"), 0o644); err != nil {
		t.Fatal(err)
	}
	evilDst := filepath.Join(link, "stolen.txt")
	if err := c.Rename(context.Background(), "", src, evilDst); err == nil {
		t.Fatal("rename into outside symlink should fail")
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("src should remain: %v", err)
	}

	body := strings.NewReader(`{"from":"` + filepath.Join(dir, "a") + `","to":"` + filepath.Join(dir, "b") + `"}`)
	resp, err := httpPostNoAuth(ts.URL+"/v1/fs/rename", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("rename unauth %d", resp.StatusCode)
	}
}

func TestDeleteRemovesSymlinkNotTarget(t *testing.T) {
	_, ts, dir := newLocal(t, true)
	defer ts.Close()
	c := localClient(ts)

	target := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(target, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "alias.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlink not available")
	}
	if err := c.Delete(context.Background(), "", link); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink should be gone: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("target must remain: %v", err)
	}
}
