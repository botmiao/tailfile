package agent

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var (
	errBadPath   = errors.New("absolute path required")
	errForbidden = errors.New("path is outside the allowed roots")
	errReadOnly  = errors.New("write is not enabled for this path")
	errPartial   = errors.New("refusing to touch a transfer partial")
	errExists    = errors.New("destination already exists")
	errNotFile   = errors.New("not a regular file")
	errIsRoot    = errors.New("refusing to delete a configured root")
)

// Roots is the set of directories the agent may read or write.
// Writes are allowed only under Write. Reads are allowed under Read or Write.
// An empty Write list means the node is read-only.
type Roots struct {
	Read  []string
	Write []string
}

func (r Roots) ReadOnly() bool { return len(r.Write) == 0 }

func (r Roots) canRead(path string) bool {
	return withinAny(path, r.Read) || withinAny(path, r.Write)
}

func (r Roots) canWrite(path string) bool {
	return withinAny(path, r.Write)
}

// Resolve checks path and returns the cleaned native path.
// Missing final components are allowed so uploads can create files.
// Symlinks are resolved; the target must stay inside the roots.
func (r Roots) Resolve(path string, write bool) (string, error) {
	clean, err := cleanAbs(path)
	if err != nil {
		return "", err
	}
	if isPartialName(filepath.Base(clean)) {
		return "", errPartial
	}
	resolved, err := resolveExisting(clean)
	if err != nil {
		return "", err
	}
	if write {
		if !r.canWrite(resolved) {
			if r.canRead(resolved) {
				return "", errReadOnly
			}
			return "", errForbidden
		}
		return clean, nil
	}
	if !r.canRead(resolved) {
		return "", errForbidden
	}
	return clean, nil
}

func cleanAbs(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errBadPath
	}
	path = filepath.Clean(filepath.FromSlash(path))
	if !filepath.IsAbs(path) {
		return "", errBadPath
	}
	return path, nil
}

// resolveExisting EvalSymlinks the longest existing prefix and rejoins the rest.
func resolveExisting(path string) (string, error) {
	var missing []string
	cur := path
	for {
		_, err := os.Lstat(cur)
		if err == nil {
			resolved, err := filepath.EvalSymlinks(cur)
			if err != nil {
				return "", err
			}
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", err
		}
		missing = append(missing, filepath.Base(cur))
		cur = parent
	}
}

func withinAny(path string, roots []string) bool {
	for _, root := range roots {
		if within(root, path) {
			return true
		}
	}
	return false
}

func within(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

func isPartialName(name string) bool {
	return strings.HasSuffix(name, ".tailfile-partial") || strings.HasSuffix(name, ".tailfile-meta")
}

// JoinRemote joins a directory and a name using forward slashes so a node
// running on one OS can name a path on a node running on another.
// The receiving agent converts the result with filepath.FromSlash.
func JoinRemote(dir, name string) string {
	dir = strings.TrimRight(strings.ReplaceAll(dir, "\\", "/"), "/")
	name = strings.Trim(strings.ReplaceAll(name, "\\", "/"), "/")
	if name == "" {
		if dir == "" {
			return "/"
		}
		return dir
	}
	if dir == "" {
		return name
	}
	// Keep Windows drive roots ("C:") from becoming "C:name".
	if strings.HasSuffix(dir, ":") {
		return dir + "/" + name
	}
	return dir + "/" + name
}

// BaseRemote is the last element of a remote path.
func BaseRemote(p string) string {
	p = strings.TrimRight(strings.ReplaceAll(p, "\\", "/"), "/")
	if p == "" {
		return ""
	}
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func copyFileAtomic(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tailfile-rename"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil {
		os.Remove(tmp)
		return copyErr
	}
	if syncErr != nil {
		os.Remove(tmp)
		return syncErr
	}
	if closeErr != nil {
		os.Remove(tmp)
		return closeErr
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func statusErr(code int, format string, args ...any) error {
	return &StatusError{Code: code, Msg: fmt.Sprintf(format, args...)}
}

// StatusError is an HTTP-aware error.
type StatusError struct {
	Code int
	Msg  string
}

func (e *StatusError) Error() string { return e.Msg }
