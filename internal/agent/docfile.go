package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// MaxEditableFileBytes caps in-app text reading and writing so large binaries
// cannot exhaust memory or hit the 60s local HTTP client timeout.
const MaxEditableFileBytes int64 = 4 << 20

// WriteFileRequest writes raw bytes to a node path with optional optimistic checks.
// ExpectedMtimeUnix == 0 disables the mtime precondition; ExpectedSize < 0 disables
// the size precondition. Force skips pre-write modification checks.
type WriteFileRequest struct {
	Node              string
	Path              string
	Data              []byte
	ExpectedMtimeUnix int64
	ExpectedSize      int64
	Force             bool
}

// WriteConflictError is returned with HTTP 409 when a file changed or disappeared
// remotely since it was loaded for editing.
type WriteConflictError struct {
	Msg     string `json:"error"`
	Code    string `json:"code"`
	Current Entry  `json:"current,omitempty"`
}

func (e *WriteConflictError) Error() string { return e.Msg }

func (s *Server) handleWriteFile(w http.ResponseWriter, r *http.Request) {
	node := r.URL.Query().Get("node")
	path := r.URL.Query().Get("path")
	if strings.TrimSpace(path) == "" {
		writeErr(w, statusErr(http.StatusBadRequest, "path required"))
		return
	}

	forceHdr := strings.TrimSpace(r.Header.Get("X-Force"))
	force := forceHdr == "1" || strings.EqualFold(forceHdr, "true")

	var hasExpectedMtime bool
	var expectedMtime int64
	if raw := strings.TrimSpace(r.Header.Get("X-Expected-Mtime-Unix")); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeErr(w, statusErr(http.StatusBadRequest, "bad X-Expected-Mtime-Unix"))
			return
		}
		if v != 0 {
			hasExpectedMtime = true
			expectedMtime = v
		}
	}

	var hasExpectedSize bool
	var expectedSize int64
	if raw := strings.TrimSpace(r.Header.Get("X-Expected-Size")); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v < 0 {
			writeErr(w, statusErr(http.StatusBadRequest, "bad X-Expected-Size"))
			return
		}
		hasExpectedSize = true
		expectedSize = v
	}

	data, err := io.ReadAll(io.LimitReader(r.Body, MaxEditableFileBytes+1))
	if err != nil {
		writeErr(w, err)
		return
	}
	if int64(len(data)) > MaxEditableFileBytes {
		writeErr(w, statusErr(http.StatusRequestEntityTooLarge, "file exceeds maximum editable size (%d bytes)", MaxEditableFileBytes))
		return
	}

	var api NodeAPI = directAPI{s: s}
	if node != "" && s.resolver != nil {
		api, err = s.resolver.API(r.Context(), node)
		if err != nil {
			writeErr(w, err)
			return
		}
	}

	if !force {
		cur, statErr := api.Stat(r.Context(), path)
		if statErr == nil {
			if cur.IsDir {
				writeErr(w, statusErr(http.StatusConflict, "destination is a directory"))
				return
			}
			if (hasExpectedMtime && cur.ModTime.UnixNano() != expectedMtime) ||
				(hasExpectedSize && cur.Size != expectedSize) {
				writeJSON(w, http.StatusConflict, WriteConflictError{
					Msg:     "远端文件已被其他操作修改",
					Code:    "modified",
					Current: cur,
				})
				return
			}
		} else if isNotExistErr(statErr) {
			if hasExpectedMtime || hasExpectedSize {
				writeJSON(w, http.StatusConflict, WriteConflictError{
					Msg:  "远端文件已被删除或移动",
					Code: "deleted",
				})
				return
			}
		} else {
			writeErr(w, statErr)
			return
		}
	}

	wholeSum := sha256.Sum256(data)
	wholeHex := hex.EncodeToString(wholeSum[:])

	// Pass a non-zero mtime so uploadMatches never resumes an unrelated stale partial.
	mtimeUnix := time.Now().UnixNano()
	if mtimeUnix <= 0 {
		mtimeUnix = 1
	}

	up, err := api.StartUpload(r.Context(), path, int64(len(data)), mtimeUnix, "", true)
	if err != nil {
		writeErr(w, err)
		return
	}
	finished := false
	defer func() {
		if !finished {
			_ = api.AbortUpload(context.Background(), up.ID)
		}
	}()

	chunkSize := up.ChunkSize
	if chunkSize <= 0 || chunkSize > maxChunkSize {
		chunkSize = defaultChunkSize
	}
	total := int64(len(data))
	for off := int64(0); off < total; {
		end := off + int64(chunkSize)
		if end > total {
			end = total
		}
		chunk := data[off:end]
		chunkSum := sha256.Sum256(chunk)
		if _, err := api.PutChunk(r.Context(), up.ID, off, chunk, hex.EncodeToString(chunkSum[:])); err != nil {
			writeErr(w, err)
			return
		}
		off = end
	}

	if err := api.FinishUpload(r.Context(), up.ID, wholeHex); err != nil {
		writeErr(w, err)
		return
	}
	finished = true

	updated, statErr := api.Stat(r.Context(), path)
	if statErr != nil {
		updated = Entry{
			Name:    BaseRemote(path),
			Path:    path,
			Size:    total,
			ModTime: time.Now(),
		}
	}
	writeJSON(w, http.StatusOK, updated)
}

func isNotExistErr(err error) bool {
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	var se *StatusError
	return errors.As(err, &se) && se.Code == http.StatusNotFound
}
