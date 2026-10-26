package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
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
}

func partialPaths(dest string) (data, meta string) {
	return dest + ".tailfile-partial", dest + ".tailfile-meta"
}

// Start opens or resumes an upload to path. size is the full object size.
// resumeID reattaches a known session when it still matches path and size.
func (u *Uploads) Start(roots Roots, path string, size int64, resumeID string) (Upload, error) {
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

	if resumeID != "" {
		if existing, ok := u.items[resumeID]; ok && existing.meta.Path == dest && existing.meta.Size == size && !existing.meta.Complete {
			return existing.snapshot(), nil
		}
	}
	for _, existing := range u.items {
		if existing.meta.Path == dest && existing.meta.Size == size && !existing.meta.Complete {
			return existing.snapshot(), nil
		}
	}
	if raw, err := os.ReadFile(metaPath); err == nil {
		var m uploadMeta
		if json.Unmarshal(raw, &m) == nil && m.Path == dest && m.Size == size && (resumeID == "" || resumeID == m.ID) {
			up, err := u.reopen(m, dataPath, metaPath)
			if err != nil {
				return Upload{}, err
			}
			u.items[m.ID] = up
			return up.snapshot(), nil
		}
	}
	// Stale partial for a different size: drop it.
	os.Remove(dataPath)
	os.Remove(metaPath)

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
			ChunkSize: m.ChunkSize,
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

// Finish fsyncs the partial and renames it onto the destination.
func (u *Uploads) Finish(id string) (Upload, error) {
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
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	tmp := up.metaP + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, up.metaP)
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
