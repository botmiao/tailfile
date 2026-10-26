package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	defaultChunkSize = 1 << 20
	maxChunkSize     = 8 << 20
)

// Uploads stores resumable partial files beside their destination.
type Uploads struct {
	ChunkSize int

	mu    sync.Mutex
	items map[string]*upload
}

type upload struct {
	mu     sync.Mutex
	meta   Upload
	data   string
	metaP  string
	file   *os.File
	closed bool
}

func NewUploads(chunk int) *Uploads {
	if chunk <= 0 {
		chunk = defaultChunkSize
	}
	if chunk > maxChunkSize {
		chunk = maxChunkSize
	}
	return &Uploads{ChunkSize: chunk, items: map[string]*upload{}}
}

type uploadMeta struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	Offset    int64  `json:"offset"`
	ChunkSize int    `json:"chunkSize"`
	MtimeUnix int64  `json:"mtimeUnix,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
}

func partialPaths(dest string) (data, meta string) {
	return dest + ".tailfile-partial", dest + ".tailfile-meta"
}

// Start opens or resumes an upload to path. size is the full object size.
// mtimeUnix is the source modification time in Unix nanoseconds. A later Start
// with a different non-zero mtime, or a different size, drops the partial and
// starts over. resumeID reattaches a known session when it still matches.
func (u *Uploads) Start(roots Roots, path string, size int64, mtimeUnix int64, resumeID string) (Upload, error) {
	if size < 0 {
		return Upload{}, statusErr(400, "size must be >= 0")
	}
	dest, err := roots.Resolve(path, true)
	if err != nil {
		return Upload{}, err
	}
	if _, err := os.Stat(dest); err == nil {
		return Upload{}, statusErr(409, "%s", errExists.Error())
	} else if !errors.Is(err, os.ErrNotExist) {
		return Upload{}, err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return Upload{}, err
	}
	// The parent directory created above must itself stay inside the write root.
	if parent, err := roots.Resolve(filepath.Dir(dest), true); err != nil {
		return Upload{}, err
	} else if _, err := os.Stat(parent); err != nil {
		return Upload{}, err
	}

	dataPath, metaPath := partialPaths(dest)
	u.mu.Lock()
	defer u.mu.Unlock()

	for _, existing := range u.items {
		if uploadMatches(existing.meta, dest, size, mtimeUnix, resumeID) {
			return existing.snapshot(), nil
		}
	}
	if raw, err := os.ReadFile(metaPath); err == nil {
		var m uploadMeta
		if json.Unmarshal(raw, &m) == nil && uploadMatches(Upload{
			ID: m.ID, Path: m.Path, Size: m.Size, MtimeUnix: m.MtimeUnix,
		}, dest, size, mtimeUnix, resumeID) {
			up, err := u.reopen(m, dataPath, metaPath)
			if err != nil {
				return Upload{}, err
			}
			u.items[m.ID] = up
			return up.snapshot(), nil
		}
	}
	// Stale partial (different size or source mtime): drop it.
	u.discardPath(dest, dataPath, metaPath)

	id := resumeID
	if id == "" {
		id, err = randID()
		if err != nil {
			return Upload{}, err
		}
	}
	f, err := os.OpenFile(dataPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return Upload{}, err
	}
	up := &upload{
		meta: Upload{
			ID: id, Path: dest, Size: size, Offset: 0, ChunkSize: u.ChunkSize,
			MtimeUnix: mtimeUnix,
		},
		data:  dataPath,
		metaP: metaPath,
		file:  f,
	}
	if err := up.persist(); err != nil {
		f.Close()
		os.Remove(dataPath)
		return Upload{}, err
	}
	u.items[id] = up
	return up.snapshot(), nil
}

func (u *Uploads) reopen(m uploadMeta, dataPath, metaPath string) (*upload, error) {
	f, err := os.OpenFile(dataPath, os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	offset := st.Size()
	if m.Offset < offset {
		offset = m.Offset
	}
	if err := f.Truncate(offset); err != nil {
		f.Close()
		return nil, err
	}
	up := &upload{
		meta: Upload{
			ID: m.ID, Path: m.Path, Size: m.Size, Offset: offset,
			ChunkSize: m.ChunkSize, MtimeUnix: m.MtimeUnix, SHA256: m.SHA256,
		},
		data: dataPath, metaP: metaPath, file: f,
	}
	if up.meta.ChunkSize <= 0 {
		up.meta.ChunkSize = u.ChunkSize
	}
	return up, nil
}

// Put writes one chunk at the current offset after checking its SHA-256.
// A retry of a chunk that is already on disk (offset+len <= current) succeeds.
func (u *Uploads) Put(id string, offset int64, data []byte, sumHex string) (int64, error) {
	if len(data) > maxChunkSize {
		return 0, statusErr(400, "chunk exceeds %d bytes", maxChunkSize)
	}
	got := sha256.Sum256(data)
	if !stringsEqualFoldHex(hex.EncodeToString(got[:]), sumHex) {
		return 0, statusErr(400, "chunk sha256 mismatch")
	}
	u.mu.Lock()
	up := u.items[id]
	u.mu.Unlock()
	if up == nil {
		return 0, statusErr(404, "upload not found")
	}
	up.mu.Lock()
	defer up.mu.Unlock()
	if up.meta.Complete {
		return 0, statusErr(409, "upload already finished")
	}
	if offset != up.meta.Offset {
		if offset >= 0 && offset+int64(len(data)) <= up.meta.Offset {
			return up.meta.Offset, nil
		}
		return up.meta.Offset, statusErr(409, "offset %d does not match %d", offset, up.meta.Offset)
	}
	if _, err := up.file.Seek(offset, io.SeekStart); err != nil {
		return 0, err
	}
	if _, err := up.file.Write(data); err != nil {
		return 0, err
	}
	up.meta.Offset += int64(len(data))
	if up.meta.Size >= 0 && up.meta.Offset > up.meta.Size {
		return 0, statusErr(400, "upload exceeded declared size")
	}
	if err := up.persist(); err != nil {
		return 0, err
	}
	return up.meta.Offset, nil
}

// Status returns the upload session.
func (u *Uploads) Status(id string) (Upload, error) {
	u.mu.Lock()
	up := u.items[id]
	u.mu.Unlock()
	if up == nil {
		return Upload{}, statusErr(404, "upload not found")
	}
	return up.snapshot(), nil
}

// Finish checks the whole-file SHA-256, fsyncs the partial, and renames it
// onto the destination. A mismatch deletes the partial.
func (u *Uploads) Finish(id string, sha256hex string) (Upload, error) {
	want := strings.TrimSpace(sha256hex)
	if !fullSHA256(want) {
		return Upload{}, statusErr(400, "whole-file sha256 required")
	}
	u.mu.Lock()
	up := u.items[id]
	u.mu.Unlock()
	if up == nil {
		return Upload{}, statusErr(404, "upload not found")
	}
	up.mu.Lock()
	defer up.mu.Unlock()
	if up.meta.Complete {
		return up.meta, nil
	}
	if up.meta.Size >= 0 && up.meta.Offset != up.meta.Size {
		return Upload{}, statusErr(409, "have %d of %d bytes", up.meta.Offset, up.meta.Size)
	}
	if _, err := up.file.Seek(0, io.SeekStart); err != nil {
		return Upload{}, err
	}
	sum, err := hashReader(up.file)
	if err != nil {
		return Upload{}, err
	}
	if !stringsEqualFoldHex(sum, want) {
		if up.file != nil && !up.closed {
			up.file.Close()
			up.closed = true
		}
		os.Remove(up.data)
		os.Remove(up.metaP)
		id := up.meta.ID
		u.mu.Lock()
		delete(u.items, id)
		u.mu.Unlock()
		return Upload{}, statusErr(400, "whole-file sha256 mismatch")
	}
	up.meta.SHA256 = strings.ToLower(want)
	if err := up.persist(); err != nil {
		return Upload{}, err
	}
	if err := up.file.Sync(); err != nil {
		return Upload{}, err
	}
	if err := up.file.Close(); err != nil {
		return Upload{}, err
	}
	up.closed = true
	if err := os.Rename(up.data, up.meta.Path); err != nil {
		return Upload{}, err
	}
	os.Remove(up.metaP)
	up.meta.Complete = true
	u.mu.Lock()
	delete(u.items, id)
	u.mu.Unlock()
	return up.meta, nil
}

// Abort deletes a partial upload.
func (u *Uploads) Abort(id string) error {
	u.mu.Lock()
	up := u.items[id]
	delete(u.items, id)
	u.mu.Unlock()
	if up == nil {
		return statusErr(404, "upload not found")
	}
	up.mu.Lock()
	defer up.mu.Unlock()
	if up.file != nil && !up.closed {
		up.file.Close()
	}
	os.Remove(up.data)
	os.Remove(up.metaP)
	return nil
}

func (up *upload) snapshot() Upload {
	up.mu.Lock()
	defer up.mu.Unlock()
	return up.meta
}

func (up *upload) persist() error {
	m := uploadMeta{
		ID: up.meta.ID, Path: up.meta.Path, Size: up.meta.Size,
		Offset: up.meta.Offset, ChunkSize: up.meta.ChunkSize,
		MtimeUnix: up.meta.MtimeUnix, SHA256: up.meta.SHA256,
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return writeSecret(up.metaP, raw)
}

func uploadMatches(m Upload, dest string, size, mtime int64, resumeID string) bool {
	if m.Complete || m.Path != dest || m.Size != size {
		return false
	}
	if resumeID != "" && resumeID != m.ID {
		return false
	}
	if mtime != 0 && m.MtimeUnix != mtime {
		return false
	}
	return true
}

func (u *Uploads) discardPath(dest, dataPath, metaPath string) {
	for id, existing := range u.items {
		if existing.meta.Path != dest || existing.meta.Complete {
			continue
		}
		if existing.file != nil && !existing.closed {
			existing.file.Close()
			existing.closed = true
		}
		delete(u.items, id)
	}
	os.Remove(dataPath)
	os.Remove(metaPath)
}

func fullSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

func stringsEqualFoldHex(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'F' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'F' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
