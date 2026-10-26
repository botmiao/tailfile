package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client talks to the local orchestrator (127.0.0.1). GUIs use this
// instead of embedding framework-specific bindings.
type Client struct {
	Base string
	HTTP *http.Client
}

func NewClient(base string) *Client {
	base = strings.TrimRight(base, "/")
	return &Client{
		Base: base,
		HTTP: &http.Client{Timeout: 0},
	}
}

func (c *Client) Health(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/v1/health", nil, nil)
}

func (c *Client) Nodes(ctx context.Context) ([]NodeInfo, error) {
	var out struct {
		Nodes []NodeInfo `json:"nodes"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/nodes", nil, &out); err != nil {
		return nil, err
	}
	return out.Nodes, nil
}

func (c *Client) List(ctx context.Context, node, path string) (Listing, error) {
	var out Listing
	q := url.Values{}
	q.Set("node", node)
	q.Set("path", path)
	err := c.do(ctx, http.MethodGet, "/v1/fs/list?"+q.Encode(), nil, &out)
	return out, err
}

func (c *Client) Stat(ctx context.Context, node, path string) (Entry, error) {
	var out Entry
	q := url.Values{}
	q.Set("node", node)
	q.Set("path", path)
	err := c.do(ctx, http.MethodGet, "/v1/fs/stat?"+q.Encode(), nil, &out)
	return out, err
}

func (c *Client) Info(ctx context.Context, node string) (Info, error) {
	var out Info
	err := c.do(ctx, http.MethodGet, "/v1/info?node="+url.QueryEscape(node), nil, &out)
	return out, err
}

// Preview is the shared text/PDF-text preview payload.
type Preview struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	Mime      string `json:"mime"`
	Path      string `json:"path"`
	Text      string `json:"text,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

func (c *Client) Preview(ctx context.Context, node, path string) (Preview, error) {
	var out Preview
	q := url.Values{}
	q.Set("node", node)
	q.Set("path", path)
	err := c.do(ctx, http.MethodGet, "/v1/fs/preview?"+q.Encode(), nil, &out)
	return out, err
}

// Open fetches file bytes. max <= 0 means the whole file.
func (c *Client) Open(ctx context.Context, node, path string, max int64) ([]byte, error) {
	q := url.Values{}
	q.Set("node", node)
	q.Set("path", path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+"/v1/fs/read?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, readAPIError(resp)
	}
	r := io.Reader(resp.Body)
	if max > 0 {
		r = io.LimitReader(resp.Body, max)
	}
	return io.ReadAll(r)
}

func (c *Client) Mkdir(ctx context.Context, node, path string) error {
	return c.do(ctx, http.MethodPost, "/v1/fs/mkdir", map[string]string{"node": node, "path": path}, nil)
}

func (c *Client) Rename(ctx context.Context, node, from, to string) error {
	return c.do(ctx, http.MethodPost, "/v1/fs/rename", map[string]string{"node": node, "from": from, "to": to}, nil)
}

func (c *Client) StartTransfer(ctx context.Context, req TransferRequest) (Transfer, error) {
	var out Transfer
	err := c.do(ctx, http.MethodPost, "/v1/transfers", req, &out)
	return out, err
}

func (c *Client) Transfers(ctx context.Context) ([]Transfer, error) {
	var out struct {
		Transfers []Transfer `json:"transfers"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/transfers", nil, &out); err != nil {
		return nil, err
	}
	return out.Transfers, nil
}

func (c *Client) CancelTransfer(ctx context.Context, id string) (Transfer, error) {
	var out Transfer
	err := c.do(ctx, http.MethodPost, "/v1/transfers/"+url.PathEscape(id)+"/cancel", nil, &out)
	return out, err
}

func (c *Client) ResumeTransfer(ctx context.Context, id string) (Transfer, error) {
	var out Transfer
	err := c.do(ctx, http.MethodPost, "/v1/transfers/"+url.PathEscape(id)+"/resume", nil, &out)
	return out, err
}

func (c *Client) Jobs(ctx context.Context) ([]Job, error) {
	var out struct {
		Jobs []Job `json:"jobs"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/jobs", nil, &out); err != nil {
		return nil, err
	}
	return out.Jobs, nil
}

func (c *Client) SetDropTarget(ctx context.Context, node, dir string) error {
	return c.do(ctx, http.MethodPost, "/v1/local/drop-target", map[string]string{"node": node, "destDir": dir}, nil)
}

func (c *Client) Import(ctx context.Context, req ImportRequest) (Job, error) {
	var out Job
	err := c.do(ctx, http.MethodPost, "/v1/local/import", req, &out)
	return out, err
}

func (c *Client) Cache(ctx context.Context, node, path string) (CacheResult, error) {
	var out CacheResult
	err := c.do(ctx, http.MethodPost, "/v1/local/cache", CacheRequest{Node: node, Path: path}, &out)
	return out, err
}

func (c *Client) Reveal(ctx context.Context, path string) error {
	return c.do(ctx, http.MethodPost, "/v1/local/reveal", map[string]string{"path": path}, nil)
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) do(ctx context.Context, method, p string, in, out any) error {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
	}
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+p, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http().Do(req)
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
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s: %w", p, err)
	}
	return nil
}
