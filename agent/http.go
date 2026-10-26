package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

func (s *Server) fileRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/health", s.handleHealth)
	mux.HandleFunc("GET /v1/info", s.handleInfo)
	mux.HandleFunc("GET /v1/fs/list", s.handleList)
	mux.HandleFunc("GET /v1/fs/stat", s.handleStat)
	mux.HandleFunc("GET /v1/fs/read", s.handleRead)
	mux.HandleFunc("HEAD /v1/fs/read", s.handleRead)
	mux.HandleFunc("POST /v1/fs/mkdir", s.handleMkdir)
	mux.HandleFunc("POST /v1/fs/rename", s.handleRename)
	mux.HandleFunc("POST /v1/uploads", s.handleUploadStart)
	mux.HandleFunc("PATCH /v1/uploads/{id}", s.handleUploadPatch)
	mux.HandleFunc("GET /v1/uploads/{id}", s.handleUploadStatus)
	mux.HandleFunc("DELETE /v1/uploads/{id}", s.handleUploadAbort)
	mux.HandleFunc("POST /v1/uploads/{id}/finish", s.handleUploadFinish)
	mux.HandleFunc("POST /v1/jobs", s.handleJobStart)
	mux.HandleFunc("GET /v1/jobs", s.handleJobList)
	mux.HandleFunc("GET /v1/jobs/{id}", s.handleJobGet)
	mux.HandleFunc("POST /v1/jobs/{id}/cancel", s.handleJobCancel)
	mux.HandleFunc("POST /v1/jobs/{id}/resume", s.handleJobResume)
}

func (s *Server) localRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/nodes", s.handleNodes)
	mux.HandleFunc("GET /v1/fs/preview", s.handlePreview)
	mux.HandleFunc("POST /v1/transfers", s.handleTransferStart)
	mux.HandleFunc("GET /v1/transfers", s.handleTransferList)
	mux.HandleFunc("POST /v1/transfers/{id}/cancel", s.handleTransferCancel)
	mux.HandleFunc("POST /v1/transfers/{id}/resume", s.handleTransferResume)
	mux.HandleFunc("POST /v1/local/drop-target", s.handleDropTarget)
	mux.HandleFunc("POST /v1/local/import", s.handleImport)
	mux.HandleFunc("POST /v1/local/cache", s.handleCache)
	mux.HandleFunc("POST /v1/local/reveal", s.handleReveal)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "version": Version, "name": s.name, "mode": s.mode,
		"readOnly": s.roots.ReadOnly(),
	})
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	if node := r.URL.Query().Get("node"); node != "" && s.resolver != nil {
		api, err := s.resolver.API(r.Context(), node)
		if err != nil {
			writeErr(w, err)
			return
		}
		info, err := api.Info(r.Context())
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, info)
		return
	}
	writeJSON(w, http.StatusOK, s.Info())
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if node := r.URL.Query().Get("node"); node != "" && s.resolver != nil {
		api, err := s.resolver.API(r.Context(), node)
		if err != nil {
			writeErr(w, err)
			return
		}
		listing, err := api.List(r.Context(), path)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, listing)
		return
	}
	listing, err := s.List(path)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, listing)
}

func (s *Server) handleStat(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if node := r.URL.Query().Get("node"); node != "" && s.resolver != nil {
		api, err := s.resolver.API(r.Context(), node)
		if err != nil {
			writeErr(w, err)
			return
		}
		ent, err := api.Stat(r.Context(), path)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, ent)
		return
	}
	ent, err := s.Stat(path)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ent)
}

func (s *Server) handleRead(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	node := r.URL.Query().Get("node")
	if node != "" && s.resolver != nil {
		s.proxyRead(w, r, node, path)
		return
	}
	st, err := s.Stat(path)
	if err != nil {
		writeErr(w, err)
		return
	}
	if st.IsDir {
		writeErr(w, statusErr(400, "is a directory"))
		return
	}
	s.writeFile(w, r, st, func(start, end int64) (io.ReadCloser, int64, error) {
		rc, size, _, err := s.Open(path, start, end)
		return rc, size, err
	})
}

func (s *Server) proxyRead(w http.ResponseWriter, r *http.Request, node, path string) {
	api, err := s.resolver.API(r.Context(), node)
	if err != nil {
		writeErr(w, err)
		return
	}
	st, err := api.Stat(r.Context(), path)
	if err != nil {
		writeErr(w, err)
		return
	}
	if st.IsDir {
		writeErr(w, statusErr(400, "is a directory"))
		return
	}
	s.writeFile(w, r, st, func(start, end int64) (io.ReadCloser, int64, error) {
		return api.Open(r.Context(), path, start, end)
	})
}

func (s *Server) writeFile(w http.ResponseWriter, r *http.Request, st Entry, open func(start, end int64) (io.ReadCloser, int64, error)) {
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Type", mimeByName(st.Name))
	w.Header().Set("Cache-Control", "no-store")
	size := st.Size
	if size == 0 {
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusOK)
		return
	}
	start, end := int64(0), size-1
	status := http.StatusOK
	if rh := r.Header.Get("Range"); rh != "" {
		var err error
		start, end, err = parseRange(rh, size)
		if err != nil {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
			writeErr(w, err)
			return
		}
		status = http.StatusPartialContent
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
	}
	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	if r.Method == http.MethodHead {
		w.WriteHeader(status)
		return
	}
	rc, _, err := open(start, end)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rc.Close()
	w.WriteHeader(status)
	_, _ = io.Copy(w, rc)
}

func (s *Server) handleMkdir(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Node string `json:"node"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, statusErr(400, "bad json"))
		return
	}
	if body.Node != "" && s.resolver != nil {
		api, err := s.resolver.API(r.Context(), body.Node)
		if err != nil {
			writeErr(w, err)
			return
		}
		if err := api.Mkdir(r.Context(), body.Path); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.Mkdir(body.Path); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRename(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Node string `json:"node"`
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, statusErr(400, "bad json"))
		return
	}
	if body.Node != "" && s.resolver != nil {
		api, err := s.resolver.API(r.Context(), body.Node)
		if err != nil {
			writeErr(w, err)
			return
		}
		if err := api.Rename(r.Context(), body.From, body.To); err != nil {
			writeErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.Rename(body.From, body.To); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUploadStart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path     string `json:"path"`
		Size     int64  `json:"size"`
		ResumeID string `json:"resumeId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, statusErr(400, "bad json"))
		return
	}
	up, err := s.uploads.Start(s.roots, body.Path, body.Size, body.ResumeID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, up)
}

func (s *Server) handleUploadPatch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	off, err := strconv.ParseInt(r.Header.Get("Upload-Offset"), 10, 64)
	if err != nil {
		writeErr(w, statusErr(400, "Upload-Offset required"))
		return
	}
	sum := strings.TrimSpace(r.Header.Get("X-Chunk-SHA256"))
	if sum == "" {
		writeErr(w, statusErr(400, "X-Chunk-SHA256 required"))
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxChunkSize+1))
	if err != nil {
		writeErr(w, err)
		return
	}
	newOff, err := s.uploads.Put(id, off, data, sum)
	if err != nil {
		var se *StatusError
		if errors.As(err, &se) && se.Code == http.StatusConflict {
			writeJSON(w, http.StatusConflict, map[string]any{"error": se.Error(), "offset": newOff})
			return
		}
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"offset": newOff})
}

func (s *Server) handleUploadStatus(w http.ResponseWriter, r *http.Request) {
	up, err := s.uploads.Status(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, up)
}

func (s *Server) handleUploadAbort(w http.ResponseWriter, r *http.Request) {
	if err := s.uploads.Abort(r.PathValue("id")); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUploadFinish(w http.ResponseWriter, r *http.Request) {
	up, err := s.uploads.Finish(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, up)
}

func (s *Server) handleJobStart(w http.ResponseWriter, r *http.Request) {
	var req JobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, statusErr(400, "bad json"))
		return
	}
	job, err := s.jobs.start(req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleJobList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"jobs": s.jobs.list()})
}

func (s *Server) handleJobGet(w http.ResponseWriter, r *http.Request) {
	job, err := s.jobs.get(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleJobCancel(w http.ResponseWriter, r *http.Request) {
	job, err := s.jobs.cancel(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleJobResume(w http.ResponseWriter, r *http.Request) {
	job, err := s.jobs.resume(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	if s.resolver == nil {
		writeJSON(w, http.StatusOK, map[string]any{"nodes": []NodeInfo{}})
		return
	}
	nodes, err := s.resolver.Nodes(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes})
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	node := r.URL.Query().Get("node")
	path := r.URL.Query().Get("path")
	var api NodeAPI
	var err error
	if node != "" && s.resolver != nil {
		api, err = s.resolver.API(r.Context(), node)
		if err != nil {
			writeErr(w, err)
			return
		}
	} else {
		api = directAPI{s: s}
	}
	st, err := api.Stat(r.Context(), path)
	if err != nil {
		writeErr(w, err)
		return
	}
	kind := PreviewKind(st.Name)
	prev := map[string]any{
		"kind": kind, "name": st.Name, "size": st.Size, "mime": mimeByName(st.Name), "path": st.Path,
	}
	limit := int64(0)
	switch kind {
	case "text":
		limit = 512 << 10
	case "pdf":
		limit = 2 << 20
	}
	if limit > 0 && !st.IsDir && st.Size <= 32<<20 {
		rc, _, err := api.Open(r.Context(), path, 0, -1)
		if err == nil {
			defer rc.Close()
			buf, _ := io.ReadAll(io.LimitReader(rc, limit))
			if kind == "pdf" {
				prev["text"] = ExtractPDFText(buf, 16<<10)
			} else {
				prev["text"] = string(buf)
			}
			if st.Size > int64(len(buf)) {
				prev["truncated"] = true
			}
		}
	}
	writeJSON(w, http.StatusOK, prev)
}

func (s *Server) handleTransferStart(w http.ResponseWriter, r *http.Request) {
	var req TransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, statusErr(400, "bad json"))
		return
	}
	tr, err := s.startTransfer(r.Context(), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tr)
}

func (s *Server) handleTransferList(w http.ResponseWriter, r *http.Request) {
	s.trackMu.Lock()
	recs := make([]transferRec, 0, len(s.tracks))
	for _, rec := range s.tracks {
		recs = append(recs, rec)
	}
	s.trackMu.Unlock()
	out := make([]Transfer, 0, len(recs))
	for _, rec := range recs {
		tr, err := s.transferView(r.Context(), rec)
		if err != nil {
			tr = Transfer{
				ID: rec.ID, Mode: rec.Req.Mode, FromNode: rec.Req.FromNode, FromPath: rec.Req.FromPath,
				ToNode: rec.Req.ToNode, ToPath: rec.Req.ToPath, RunnerNode: rec.Runner,
				Job: Job{ID: rec.RemoteID, Status: jobError, Error: err.Error()},
			}
		}
		out = append(out, tr)
	}
	writeJSON(w, http.StatusOK, map[string]any{"transfers": out})
}

func (s *Server) handleTransferCancel(w http.ResponseWriter, r *http.Request) {
	tr, err := s.transferAct(r.Context(), r.PathValue("id"), true)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tr)
}

func (s *Server) handleTransferResume(w http.ResponseWriter, r *http.Request) {
	tr, err := s.transferAct(r.Context(), r.PathValue("id"), false)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tr)
}

func (s *Server) startTransfer(ctx context.Context, req TransferRequest) (Transfer, error) {
	if s.resolver == nil {
		return Transfer{}, statusErr(500, "node directory is not configured")
	}
	req.Mode = strings.ToLower(strings.TrimSpace(req.Mode))
	if req.Mode != "push" && req.Mode != "pull" {
		return Transfer{}, statusErr(400, "mode must be push or pull")
	}
	if req.FromNode == "" || req.ToNode == "" || req.FromPath == "" || req.ToPath == "" {
		return Transfer{}, statusErr(400, "fromNode, fromPath, toNode and toPath are required")
	}
	fromURL, err := s.resolver.DialURL(req.FromNode)
	if err != nil {
		return Transfer{}, err
	}
	toURL, err := s.resolver.DialURL(req.ToNode)
	if err != nil {
		return Transfer{}, err
	}
	var runner string
	var jobReq JobRequest
	if req.Mode == "push" {
		runner = req.FromNode
		jobReq = JobRequest{Mode: "push", SrcPath: req.FromPath, DestPath: req.ToPath, DestBaseURL: toURL}
	} else {
		runner = req.ToNode
		jobReq = JobRequest{Mode: "pull", SrcPath: req.FromPath, DestPath: req.ToPath, SrcBaseURL: fromURL}
	}
	api, err := s.resolver.API(ctx, runner)
	if err != nil {
		return Transfer{}, err
	}
	job, err := api.StartJob(ctx, jobReq)
	if err != nil {
		return Transfer{}, err
	}
	id, err := randID()
	if err != nil {
		return Transfer{}, err
	}
	rec := transferRec{ID: id, Req: req, Runner: runner, RemoteID: job.ID}
	s.trackMu.Lock()
	s.tracks[id] = rec
	s.trackMu.Unlock()
	return Transfer{
		ID: id, Mode: req.Mode, FromNode: req.FromNode, FromPath: req.FromPath,
		ToNode: req.ToNode, ToPath: req.ToPath, RunnerNode: runner, Job: job,
	}, nil
}

func (s *Server) transferView(ctx context.Context, rec transferRec) (Transfer, error) {
	api, err := s.resolver.API(ctx, rec.Runner)
	if err != nil {
		return Transfer{}, err
	}
	job, err := api.GetJob(ctx, rec.RemoteID)
	if err != nil {
		return Transfer{}, err
	}
	return Transfer{
		ID: rec.ID, Mode: rec.Req.Mode, FromNode: rec.Req.FromNode, FromPath: rec.Req.FromPath,
		ToNode: rec.Req.ToNode, ToPath: rec.Req.ToPath, RunnerNode: rec.Runner, Job: job,
	}, nil
}

func (s *Server) transferAct(ctx context.Context, id string, cancel bool) (Transfer, error) {
	s.trackMu.Lock()
	rec, ok := s.tracks[id]
	s.trackMu.Unlock()
	if !ok {
		return Transfer{}, statusErr(404, "transfer not found")
	}
	if s.resolver == nil {
		return Transfer{}, statusErr(500, "node directory is not configured")
	}
	api, err := s.resolver.API(ctx, rec.Runner)
	if err != nil {
		return Transfer{}, err
	}
	if cancel {
		if err := api.CancelJob(ctx, rec.RemoteID); err != nil {
			return Transfer{}, err
		}
	} else {
		if _, err := api.ResumeJob(ctx, rec.RemoteID); err != nil {
			return Transfer{}, err
		}
	}
	return s.transferView(ctx, rec)
}

func (s *Server) handleDropTarget(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Node    string `json:"node"`
		DestDir string `json:"destDir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, statusErr(400, "bad json"))
		return
	}
	s.dropMu.Lock()
	s.dropNode = body.Node
	s.dropDir = body.DestDir
	s.dropMu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	var req ImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, statusErr(400, "bad json"))
		return
	}
	if req.Node == "" || req.DestDir == "" {
		s.dropMu.Lock()
		if req.Node == "" {
			req.Node = s.dropNode
		}
		if req.DestDir == "" {
			req.DestDir = s.dropDir
		}
		s.dropMu.Unlock()
	}
	if s.resolver == nil {
		writeErr(w, statusErr(500, "node directory is not configured"))
		return
	}
	destURL, err := s.resolver.DialURL(req.Node)
	if err != nil {
		writeErr(w, err)
		return
	}
	job, err := s.jobs.startImport(destURL, req.DestDir, req.Paths)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleCache(w http.ResponseWriter, r *http.Request) {
	var req CacheRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, statusErr(400, "bad json"))
		return
	}
	res, err := s.cache(r.Context(), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) cache(ctx context.Context, req CacheRequest) (CacheResult, error) {
	if s.resolver == nil {
		return CacheResult{}, statusErr(500, "node directory is not configured")
	}
	api, err := s.resolver.API(ctx, req.Node)
	if err != nil {
		return CacheResult{}, err
	}
	if _, ok := api.(directAPI); ok {
		st, err := s.Stat(req.Path)
		if err != nil {
			return CacheResult{}, err
		}
		if st.IsDir {
			return CacheResult{}, statusErr(400, "cache a file, not a folder")
		}
		return CacheResult{LocalPath: st.Path, Cached: false}, nil
	}
	st, err := api.Stat(ctx, req.Path)
	if err != nil {
		return CacheResult{}, err
	}
	if st.IsDir {
		return CacheResult{}, statusErr(400, "cache a file, not a folder")
	}
	dir := filepath.Join(s.cacheDir, "tailfile-cache", sanitize(req.Node))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return CacheResult{}, err
	}
	name := BaseRemote(st.Path)
	if name == "" {
		name = "file"
	}
	dest := filepath.Join(dir, name)
	rc, _, err := api.Open(ctx, req.Path, 0, -1)
	if err != nil {
		return CacheResult{}, err
	}
	defer rc.Close()
	tmp := dest + ".partial"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return CacheResult{}, err
	}
	_, copyErr := io.Copy(f, rc)
	closeErr := f.Close()
	if copyErr != nil {
		os.Remove(tmp)
		return CacheResult{}, copyErr
	}
	if closeErr != nil {
		os.Remove(tmp)
		return CacheResult{}, closeErr
	}
	if err := os.Rename(tmp, dest); err != nil {
		return CacheResult{}, err
	}
	return CacheResult{LocalPath: dest, Cached: true}, nil
}

func (s *Server) handleReveal(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Path == "" {
		writeErr(w, statusErr(400, "path required"))
		return
	}
	if err := revealPath(body.Path); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func revealPath(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", "-R", path).Start()
	case "windows":
		return exec.Command("explorer", "/select,"+path).Start()
	default:
		dir := path
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			dir = filepath.Dir(path)
		}
		return exec.Command("xdg-open", dir).Start()
	}
}

func parseRange(h string, size int64) (start, end int64, err error) {
	if size <= 0 {
		return 0, 0, statusErr(416, "empty object")
	}
	h = strings.TrimSpace(h)
	if !strings.HasPrefix(h, "bytes=") {
		return 0, 0, statusErr(416, "only bytes ranges are supported")
	}
	spec := strings.TrimPrefix(h, "bytes=")
	spec, _, _ = strings.Cut(spec, ",")
	spec = strings.TrimSpace(spec)
	left, right, ok := strings.Cut(spec, "-")
	if !ok {
		return 0, 0, statusErr(416, "bad range")
	}
	if left == "" {
		n, err := strconv.ParseInt(right, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, statusErr(416, "bad suffix range")
		}
		if n > size {
			n = size
		}
		return size - n, size - 1, nil
	}
	start, err = strconv.ParseInt(left, 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, statusErr(416, "range not satisfiable")
	}
	if right == "" {
		return start, size - 1, nil
	}
	end, err = strconv.ParseInt(right, 10, 64)
	if err != nil || end < start {
		return 0, 0, statusErr(416, "bad range")
	}
	if end >= size {
		end = size - 1
	}
	return start, end, nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	var se *StatusError
	switch {
	case err == nil:
		code = http.StatusInternalServerError
		err = errors.New("unknown error")
	case errors.As(err, &se):
		code = se.Code
	case errors.Is(err, os.ErrNotExist):
		code = http.StatusNotFound
	case errors.Is(err, errForbidden), errors.Is(err, errReadOnly):
		code = http.StatusForbidden
	case errors.Is(err, errBadPath), errors.Is(err, errNotFile), errors.Is(err, errPartial):
		code = http.StatusBadRequest
	case errors.Is(err, errExists):
		code = http.StatusConflict
	}
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := b.String()
	if out == "" || out == "." || out == ".." {
		return "node"
	}
	return out
}
