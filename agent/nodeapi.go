package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// NodeAPI is the file and job API of one agent, either in-process or over HTTP.
type NodeAPI interface {
	Info(ctx context.Context) (Info, error)
	List(ctx context.Context, path string) (Listing, error)
	Stat(ctx context.Context, path string) (Entry, error)
	// Open reads [start, end] inclusive. end < 0 reads through EOF.
	// The returned size is the full object size.
	Open(ctx context.Context, path string, start, end int64) (io.ReadCloser, int64, error)
	Mkdir(ctx context.Context, path string) error
	Rename(ctx context.Context, from, to string) error
	StartUpload(ctx context.Context, path string, size int64, mtimeUnix int64, resumeID string) (Upload, error)
	PutChunk(ctx context.Context, id string, offset int64, data []byte, sumHex string) (int64, error)
	FinishUpload(ctx context.Context, id string, sha256hex string) error
	AbortUpload(ctx context.Context, id string) error
	StartJob(ctx context.Context, req JobRequest) (Job, error)
	GetJob(ctx context.Context, id string) (Job, error)
	CancelJob(ctx context.Context, id string) error
	ResumeJob(ctx context.Context, id string) (Job, error)
	Health(ctx context.Context) error
}

// directAPI calls this process. It is how the local node touches its own disk
// without hairpinning through the tailnet.
type directAPI struct{ s *Server }

func (d directAPI) Info(ctx context.Context) (Info, error) {
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	return d.s.Info(), nil
}

func (d directAPI) List(ctx context.Context, path string) (Listing, error) {
	if err := ctx.Err(); err != nil {
		return Listing{}, err
	}
	return d.s.list(d.s.rootsFor(ctx), path)
}

func (d directAPI) Stat(ctx context.Context, path string) (Entry, error) {
	if err := ctx.Err(); err != nil {
		return Entry{}, err
	}
	return d.s.stat(d.s.rootsFor(ctx), path)
}

func (d directAPI) Open(ctx context.Context, path string, start, end int64) (io.ReadCloser, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	rc, size, _, err := d.s.open(d.s.rootsFor(ctx), path, start, end)
	return rc, size, err
}

func (d directAPI) Mkdir(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return d.s.mkdir(d.s.rootsFor(ctx), path)
}

func (d directAPI) Rename(ctx context.Context, from, to string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return d.s.rename(d.s.rootsFor(ctx), from, to)
}

func (d directAPI) StartUpload(ctx context.Context, path string, size int64, mtimeUnix int64, resumeID string) (Upload, error) {
	if err := ctx.Err(); err != nil {
		return Upload{}, err
	}
	return d.s.uploads.Start(d.s.rootsFor(ctx), path, size, mtimeUnix, resumeID)
}

func (d directAPI) PutChunk(ctx context.Context, id string, offset int64, data []byte, sumHex string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	off, err := d.s.uploads.Put(id, offset, data, sumHex)
	if err != nil {
		var se *StatusError
		if errors.As(err, &se) && se.Code == 409 && strings.Contains(se.Msg, "does not match") {
			// A retried chunk that the server already has returns the current offset
			// only when Put treats it as already written. Anything else is fatal.
			return off, err
		}
	}
	return off, err
}

func (d directAPI) FinishUpload(ctx context.Context, id string, sha256hex string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := d.s.uploads.Finish(id, sha256hex)
	return err
}

func (d directAPI) AbortUpload(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return d.s.uploads.Abort(id)
}

func (d directAPI) StartJob(ctx context.Context, req JobRequest) (Job, error) {
	if err := ctx.Err(); err != nil {
		return Job{}, err
	}
	return d.s.jobs.start(ctx, req)
}

func (d directAPI) GetJob(ctx context.Context, id string) (Job, error) {
	if err := ctx.Err(); err != nil {
		return Job{}, err
	}
	return d.s.jobs.get(id)
}

func (d directAPI) CancelJob(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := d.s.jobs.cancel(id)
	return err
}

func (d directAPI) ResumeJob(ctx context.Context, id string) (Job, error) {
	if err := ctx.Err(); err != nil {
		return Job{}, err
	}
	return d.s.jobs.resume(id)
}

func (d directAPI) Health(context.Context) error { return nil }

// httpAPI calls another node's tailnet (or loopback) HTTP API.
type httpAPI struct {
	base  string
	http  *http.Client
	token string
}

func (h *httpAPI) client() *http.Client {
	if h.http != nil {
		return h.http
	}
	return http.DefaultClient
}

func (h *httpAPI) Info(ctx context.Context) (Info, error) {
	var out Info
	err := h.do(ctx, http.MethodGet, "/v1/info", nil, nil, &out)
	return out, err
}

func (h *httpAPI) List(ctx context.Context, path string) (Listing, error) {
	var out Listing
	err := h.do(ctx, http.MethodGet, "/v1/fs/list?path="+url.QueryEscape(path), nil, nil, &out)
	return out, err
}

func (h *httpAPI) Stat(ctx context.Context, path string) (Entry, error) {
	var out Entry
	err := h.do(ctx, http.MethodGet, "/v1/fs/stat?path="+url.QueryEscape(path), nil, nil, &out)
	return out, err
}

func (h *httpAPI) Open(ctx context.Context, path string, start, end int64) (io.ReadCloser, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.base+"/v1/fs/read?path="+url.QueryEscape(path), nil)
	if err != nil {
		return nil, 0, err
	}
	if start > 0 || end >= 0 {
		if end >= 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
		} else {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", start))
		}
	}
	h.authorize(req)
	resp, err := h.client().Do(req)
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		defer resp.Body.Close()
		return nil, 0, readAPIError(resp)
	}
	size := resp.ContentLength
	if cr := resp.Header.Get("Content-Range"); cr != "" {
		if i := strings.LastIndex(cr, "/"); i >= 0 {
			if n, err := strconv.ParseInt(cr[i+1:], 10, 64); err == nil {
				size = n
			}
		}
	}
	return resp.Body, size, nil
}

func (h *httpAPI) Mkdir(ctx context.Context, path string) error {
	return h.do(ctx, http.MethodPost, "/v1/fs/mkdir", map[string]string{"path": path}, nil, nil)
}

func (h *httpAPI) Rename(ctx context.Context, from, to string) error {
	return h.do(ctx, http.MethodPost, "/v1/fs/rename", map[string]string{"from": from, "to": to}, nil, nil)
}

func (h *httpAPI) StartUpload(ctx context.Context, path string, size int64, mtimeUnix int64, resumeID string) (Upload, error) {
	var out Upload
	err := h.do(ctx, http.MethodPost, "/v1/uploads", map[string]any{
		"path": path, "size": size, "mtimeUnix": mtimeUnix, "resumeId": resumeID,
	}, nil, &out)
	return out, err
}

func (h *httpAPI) PutChunk(ctx context.Context, id string, offset int64, data []byte, sumHex string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, h.base+"/v1/uploads/"+url.PathEscape(id), bytes.NewReader(data))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Upload-Offset", strconv.FormatInt(offset, 10))
	req.Header.Set("X-Chunk-SHA256", sumHex)
	req.Header.Set("Content-Type", "application/octet-stream")
	h.authorize(req)
	resp, err := h.client().Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusConflict {
		var body struct {
			Offset int64  `json:"offset"`
			Error  string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if body.Offset >= offset+int64(len(data)) && body.Offset > 0 {
			return body.Offset, nil
		}
		if body.Error == "" {
			body.Error = "offset conflict"
		}
		return body.Offset, &StatusError{Code: 409, Msg: body.Error}
	}
	if resp.StatusCode >= 300 {
		return 0, readAPIError(resp)
	}
	var out struct {
		Offset int64 `json:"offset"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	return out.Offset, nil
}

func (h *httpAPI) FinishUpload(ctx context.Context, id string, sha256hex string) error {
	return h.do(ctx, http.MethodPost, "/v1/uploads/"+url.PathEscape(id)+"/finish", map[string]string{
		"sha256": sha256hex,
	}, nil, nil)
}

func (h *httpAPI) AbortUpload(ctx context.Context, id string) error {
	return h.do(ctx, http.MethodDelete, "/v1/uploads/"+url.PathEscape(id), nil, nil, nil)
}

func (h *httpAPI) StartJob(ctx context.Context, req JobRequest) (Job, error) {
	var out Job
	err := h.do(ctx, http.MethodPost, "/v1/jobs", req, nil, &out)
	return out, err
}

func (h *httpAPI) GetJob(ctx context.Context, id string) (Job, error) {
	var out Job
	err := h.do(ctx, http.MethodGet, "/v1/jobs/"+url.PathEscape(id), nil, nil, &out)
	return out, err
}

func (h *httpAPI) CancelJob(ctx context.Context, id string) error {
	return h.do(ctx, http.MethodPost, "/v1/jobs/"+url.PathEscape(id)+"/cancel", nil, nil, nil)
}

func (h *httpAPI) ResumeJob(ctx context.Context, id string) (Job, error) {
	var out Job
	err := h.do(ctx, http.MethodPost, "/v1/jobs/"+url.PathEscape(id)+"/resume", nil, nil, &out)
	return out, err
}

func (h *httpAPI) Health(ctx context.Context) error {
	return h.do(ctx, http.MethodGet, "/v1/health", nil, nil, nil)
}

func (h *httpAPI) do(ctx context.Context, method, p string, in any, hdr http.Header, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, h.base+p, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	h.authorize(req)
	for k, v := range hdr {
		req.Header[k] = v
	}
	resp, err := h.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return readAPIError(resp)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (h *httpAPI) authorize(req *http.Request) {
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}
}

func readAPIError(resp *http.Response) error {
	var body struct {
		Error string `json:"error"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = json.Unmarshal(raw, &body)
	if body.Error == "" {
		body.Error = strings.TrimSpace(string(raw))
		if body.Error == "" {
			body.Error = resp.Status
		}
	}
	return &StatusError{Code: resp.StatusCode, Msg: body.Error}
}
