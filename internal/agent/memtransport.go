package agent

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
)

// memRoundTripper executes HTTP requests directly against an in-process
// http.Handler using io.Pipe streaming, avoiding loopback TCP sockets and
// disk gui-token files in embedded desktop and mobile modes.
type memRoundTripper struct {
	handler http.Handler
}

func (m *memRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.RemoteAddr = "127.0.0.1:0"
	if clone.Host == "" && clone.URL != nil {
		clone.Host = clone.URL.Host
	}
	if clone.RequestURI == "" && clone.URL != nil {
		clone.RequestURI = clone.URL.RequestURI()
	}
	if clone.Body == nil {
		clone.Body = http.NoBody
	}

	pr, pw := io.Pipe()
	rw := &pipeResponseWriter{
		header:      make(http.Header),
		pw:          pw,
		headerReady: make(chan struct{}),
	}

	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				rw.fail(fmt.Errorf("handler panic: %v", rec))
				return
			}
			rw.finish()
		}()
		m.handler.ServeHTTP(rw, clone)
	}()

	select {
	case <-rw.headerReady:
	case <-req.Context().Done():
		_ = pr.Close()
		return nil, req.Context().Err()
	}

	rw.mu.Lock()
	code := rw.code
	hdr := rw.snapshot
	err := rw.err
	rw.mu.Unlock()
	if err != nil {
		_ = pr.Close()
		return nil, err
	}
	if code == 0 {
		code = http.StatusOK
	}
	contentLen := int64(-1)
	if cl := hdr.Get("Content-Length"); cl != "" {
		if n, parseErr := strconv.ParseInt(cl, 10, 64); parseErr == nil {
			contentLen = n
		}
	}
	return &http.Response{
		StatusCode:    code,
		Status:        fmt.Sprintf("%d %s", code, http.StatusText(code)),
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        hdr,
		Body:          pr,
		ContentLength: contentLen,
		Request:       req,
	}, nil
}

type pipeResponseWriter struct {
	mu          sync.Mutex
	header      http.Header
	snapshot    http.Header
	code        int
	wroteHeader bool
	err         error
	pw          *io.PipeWriter
	headerReady chan struct{}
	headerOnce  sync.Once
}

func (w *pipeResponseWriter) Header() http.Header {
	return w.header
}

func (w *pipeResponseWriter) WriteHeader(status int) {
	w.mu.Lock()
	if !w.wroteHeader {
		w.wroteHeader = true
		w.code = status
		w.snapshot = w.header.Clone()
	}
	w.mu.Unlock()
	w.headerOnce.Do(func() { close(w.headerReady) })
}

func (w *pipeResponseWriter) Write(p []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	return w.pw.Write(p)
}

func (w *pipeResponseWriter) Flush() {
	w.WriteHeader(http.StatusOK)
}

func (w *pipeResponseWriter) finish() {
	w.WriteHeader(http.StatusOK)
	_ = w.pw.Close()
}

func (w *pipeResponseWriter) fail(err error) {
	w.mu.Lock()
	w.err = err
	w.mu.Unlock()
	w.headerOnce.Do(func() { close(w.headerReady) })
	_ = w.pw.CloseWithError(err)
}
