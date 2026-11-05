package agent

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func randID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func (s *Server) Info() Info {
	s.mu.Lock()
	owner := s.owner
	mode := s.mode
	s.mu.Unlock()
	return Info{
		Name:       s.name,
		Version:    Version,
		Owner:      owner,
		ReadOnly:   s.roots.ReadOnly(),
		ReadRoots:  append([]string(nil), s.roots.Read...),
		WriteRoots: append([]string(nil), s.roots.Write...),
		Mode:       mode,
	}
}

func (s *Server) List(path string) (Listing, error) {
	return s.list(s.roots, path)
}

func (s *Server) list(roots Roots, path string) (Listing, error) {
	clean, err := roots.Resolve(path, false)
	if err != nil {
		return Listing{}, err
	}
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return Listing{}, err
	}
	if !roots.canRead(resolved) {
		return Listing{}, errForbidden
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return Listing{}, err
	}
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if isPartialName(e.Name()) {
			continue
		}
		full := filepath.Join(clean, e.Name())
		info, lerr := os.Lstat(full)
		if lerr != nil {
			continue
		}
		ent := Entry{
			Name:    e.Name(),
			Path:    full,
			Size:    info.Size(),
			ModTime: info.ModTime(),
			Mode:    info.Mode().String(),
		}
		if info.Mode()&os.ModeSymlink != 0 {
			ent.IsSymlink = true
			if target, err := os.Readlink(full); err == nil {
				ent.LinkTarget = target
			}
			if resolved, err := filepath.EvalSymlinks(full); err == nil && roots.canRead(resolved) {
				if st, err := os.Stat(resolved); err == nil && st.IsDir() {
					ent.IsDir = true
					ent.Size = st.Size()
				}
			}
		} else {
			ent.IsDir = info.IsDir()
		}
		out = append(out, ent)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return Listing{Path: clean, Entries: out}, nil
}

func (s *Server) Stat(path string) (Entry, error) {
	return s.stat(s.roots, path)
}

func (s *Server) stat(roots Roots, path string) (Entry, error) {
	clean, err := roots.Resolve(path, false)
	if err != nil {
		return Entry{}, err
	}
	info, err := os.Lstat(clean)
	if err != nil {
		return Entry{}, err
	}
	ent := Entry{
		Name:    filepath.Base(clean),
		Path:    clean,
		Size:    info.Size(),
		ModTime: info.ModTime(),
		Mode:    info.Mode().String(),
		IsDir:   info.IsDir(),
	}
	if info.Mode()&os.ModeSymlink != 0 {
		ent.IsSymlink = true
		ent.LinkTarget, _ = os.Readlink(clean)
		ent.IsDir = false
		if resolved, err := filepath.EvalSymlinks(clean); err == nil && roots.canRead(resolved) {
			if st, err := os.Stat(resolved); err == nil {
				ent.IsDir = st.IsDir()
				ent.Size = st.Size()
				ent.ModTime = st.ModTime()
			}
		}
	}
	return ent, nil
}

// Open returns a reader for the byte range [start, end] inclusive.
// end < 0 means through EOF. The caller must close the reader.
func (s *Server) Open(path string, start, end int64) (io.ReadCloser, int64, int64, error) {
	return s.open(s.roots, path, start, end)
}

func (s *Server) open(roots Roots, path string, start, end int64) (io.ReadCloser, int64, int64, error) {
	clean, err := roots.Resolve(path, false)
	if err != nil {
		return nil, 0, 0, err
	}
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return nil, 0, 0, err
	}
	if !roots.canRead(resolved) {
		return nil, 0, 0, errForbidden
	}
	f, err := os.Open(resolved)
	if err != nil {
		return nil, 0, 0, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, 0, err
	}
	if !st.Mode().IsRegular() {
		f.Close()
		return nil, 0, 0, errNotFile
	}
	size := st.Size()
	if start < 0 || start > size {
		f.Close()
		return nil, 0, 0, statusErr(416, "range start out of bounds")
	}
	if end < 0 || end >= size {
		end = size - 1
	}
	if size == 0 {
		f.Close()
		return io.NopCloser(strings.NewReader("")), 0, 0, nil
	}
	if end < start {
		f.Close()
		return nil, 0, 0, statusErr(416, "empty range")
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		f.Close()
		return nil, 0, 0, err
	}
	return struct {
		io.Reader
		io.Closer
	}{io.LimitReader(f, end-start+1), f}, size, end, nil
}

func (s *Server) Mkdir(path string) error {
	return s.mkdir(s.roots, path)
}

func (s *Server) mkdir(roots Roots, path string) error {
	clean, err := roots.Resolve(path, true)
	if err != nil {
		return err
	}
	return os.MkdirAll(clean, 0o755)
}

func (s *Server) Rename(from, to string) error {
	return s.rename(s.roots, from, to)
}

func (s *Server) rename(roots Roots, from, to string) error {
	src, err := roots.Resolve(from, true)
	if err != nil {
		return err
	}
	dst, err := roots.Resolve(to, true)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		return statusErr(409, "%s", errExists.Error())
	}
	return os.Rename(src, dst)
}


func (s *Server) Delete(path string) error {
	return s.deletePath(s.roots, path)
}

func (s *Server) deletePath(roots Roots, path string) error {
	clean, err := roots.Resolve(path, true)
	if err != nil {
		return err
	}
	resolved, err := resolveExisting(clean)
	if err != nil {
		return err
	}
	if isConfiguredRoot(roots, resolved) || isConfiguredRoot(roots, clean) {
		return errIsRoot
	}
	st, err := os.Lstat(clean)
	if err != nil {
		return err
	}
	if st.IsDir() {
		return os.RemoveAll(clean)
	}
	return os.Remove(clean)
}

func isConfiguredRoot(roots Roots, path string) bool {
	path = filepath.Clean(path)
	for _, root := range append(append([]string{}, roots.Read...), roots.Write...) {
		if filepath.Clean(root) == path {
			return true
		}
	}
	return false
}

func (s *Server) ownerLogin() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.owner
}

func (s *Server) setOwner(login string) {
	s.mu.Lock()
	s.owner = login
	s.mu.Unlock()
}

func mimeByName(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".txt", ".log", ".csv", ".md":
		return "text/plain; charset=utf-8"
	case ".json":
		return "application/json"
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	case ".pdf":
		return "application/pdf"
	case ".svg":
		return "image/svg+xml"
	default:
		return "application/octet-stream"
	}
}

// PreviewKind classifies a file name for the preview pane.
func PreviewKind(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".txt", ".md", ".log", ".csv", ".json", ".yml", ".yaml", ".xml",
		".html", ".htm", ".css", ".js", ".ts", ".tsx", ".jsx", ".go", ".py",
		".rs", ".toml", ".ini", ".sh", ".mod", ".sum":
		return "text"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return "image"
	case ".pdf":
		return "pdf"
	default:
		return "other"
	}
}

// ExtractPDFText pulls literal strings out of a PDF's content streams.
// It is a preview aid for simple documents, not a PDF renderer.
func ExtractPDFText(data []byte, max int) string {
	if max <= 0 {
		max = 16 << 10
	}
	if len(data) < 5 || string(data[:5]) != "%PDF-" {
		return ""
	}
	var b strings.Builder
	rest := data
	for len(b.String()) < max {
		i := indexFold(rest, "stream")
		if i < 0 {
			break
		}
		dictStart := i - 300
		if dictStart < 0 {
			dictStart = 0
		}
		dict := rest[dictStart:i]
		payload := rest[i+len("stream"):]
		if len(payload) > 0 && payload[0] == '\r' {
			payload = payload[1:]
		}
		if len(payload) > 0 && payload[0] == '\n' {
			payload = payload[1:]
		}
		end := indexFold(payload, "endstream")
		if end < 0 {
			break
		}
		chunk := payload[:end]
		if len(chunk) > 0 && (chunk[len(chunk)-1] == '\n' || chunk[len(chunk)-1] == '\r') {
			chunk = chunk[:len(chunk)-1]
		}
		if bytesContainsFold(dict, "FlateDecode") {
			if dec, err := inflate(chunk); err == nil {
				chunk = dec
			}
		}
		if bytesContainsFold(chunk, "Tj") || bytesContainsFold(chunk, "TJ") {
			b.WriteString(pdfLiterals(chunk))
			b.WriteByte('\n')
		}
		rest = payload[end:]
	}
	out := strings.TrimSpace(b.String())
	if len(out) > max {
		out = out[:max]
	}
	return out
}

func pdfLiterals(chunk []byte) string {
	var b strings.Builder
	for i := 0; i < len(chunk); i++ {
		if chunk[i] != '(' {
			continue
		}
		var lit strings.Builder
		esc := false
		for j := i + 1; j < len(chunk); j++ {
			c := chunk[j]
			if esc {
				switch c {
				case 'n':
					lit.WriteByte('\n')
				case 'r':
					lit.WriteByte('\r')
				case 't':
					lit.WriteByte('\t')
				default:
					lit.WriteByte(c)
				}
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == ')' {
				if lit.Len() > 0 {
					if b.Len() > 0 {
						b.WriteByte(' ')
					}
					b.WriteString(lit.String())
				}
				i = j
				break
			}
			lit.WriteByte(c)
		}
	}
	return b.String()
}

func indexFold(b []byte, needle string) int {
	if len(needle) == 0 || len(b) < len(needle) {
		return -1
	}
	n := strings.ToLower(needle)
	for i := 0; i+len(n) <= len(b); i++ {
		if strings.ToLower(string(b[i:i+len(n)])) == n {
			return i
		}
	}
	return -1
}

func bytesContainsFold(b []byte, needle string) bool {
	return indexFold(b, needle) >= 0
}

func inflate(b []byte) ([]byte, error) {
	r, err := zlibOrFlate(b)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, 8<<20))
}
