package agent

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A root configured through a symlink (macOS /tmp -> /private/tmp, a home
// directory on another mount) must accept paths under it, refuse deleting
// it in either form, and still refuse escapes.
func TestSymlinkedRootResolves(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.MkdirAll(filepath.Join(real, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}

	roots := Roots{Read: []string{link}, Write: []string{link}}
	for _, p := range []string{link, filepath.Join(link, "f.txt"), filepath.Join(link, "new.bin"), filepath.Join(link, "sub")} {
		if _, err := roots.Resolve(p, false); err != nil {
			t.Errorf("read %s: %v", p, err)
		}
		if _, err := roots.Resolve(p, true); err != nil {
			t.Errorf("write %s: %v", p, err)
		}
	}
	escape := filepath.Join(filepath.Dir(base), "escape.txt")
	for _, p := range []string{outside, filepath.Join(link, "..", "outside"), escape} {
		if _, err := roots.Resolve(p, false); !errors.Is(err, errForbidden) {
			t.Errorf("escape %s: got %v, want errForbidden", p, err)
		}
	}

	s := newServer("n", "test", "ada@example.com", roots, 1024)
	for _, p := range []string{link, real} {
		if err := s.deletePath(roots, p); !errors.Is(err, errIsRoot) && !errors.Is(err, errForbidden) {
			t.Errorf("delete root %s: got %v", p, err)
		}
	}
	if err := s.deletePath(roots, link); !errors.Is(err, errIsRoot) {
		t.Errorf("delete link root: got %v, want errIsRoot", err)
	}
	if err := s.deletePath(roots, filepath.Join(link, "f.txt")); err != nil {
		t.Errorf("delete file under link root: %v", err)
	}
	if _, err := os.Stat(real); err != nil {
		t.Errorf("real root removed: %v", err)
	}
}
