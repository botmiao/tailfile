package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config is the process configuration for tailfile-agent and for GUIs that embed it.
type Config struct {
	Hostname   string
	StateDir   string
	Port       int
	LocalAddr  string
	Owner      string
	ReadRoots  []string
	WriteRoots []string
	Demo       bool
	AuthKey    string
	ChunkSize  int
	Ephemeral  bool
}

func (c *Config) applyDefaults() error {
	if c.Hostname == "" {
		h, err := os.Hostname()
		if err != nil || strings.TrimSpace(h) == "" {
			h = "node"
		}
		c.Hostname = "tailfile-" + sanitize(h)
	}
	if c.Port == 0 {
		c.Port = 8644
	}
	if c.LocalAddr == "" {
		c.LocalAddr = "127.0.0.1:8645"
	}
	if c.ChunkSize == 0 {
		c.ChunkSize = defaultChunkSize
	}
	if c.AuthKey == "" {
		c.AuthKey = os.Getenv("TS_AUTHKEY")
	}
	if c.Owner == "" {
		c.Owner = os.Getenv("TAILFILE_OWNER")
	}
	if c.StateDir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			base = os.TempDir()
		}
		c.StateDir = filepath.Join(base, "tailfile", c.Hostname)
	}
	if err := os.MkdirAll(c.StateDir, 0o700); err != nil {
		return err
	}
	if !c.Demo && len(c.ReadRoots) == 0 {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("no -read root and no home directory: %w", err)
		}
		c.ReadRoots = []string{home}
		log.Printf("tailfile: read root defaulting to %s (read-only until -write is set)", home)
	}
	return nil
}

// Run blocks until ctx is canceled or a listener fails.
func Run(ctx context.Context, cfg Config) error {
	if err := cfg.applyDefaults(); err != nil {
		return err
	}
	if cfg.Demo {
		return runDemo(ctx, cfg)
	}
	return runTailnet(ctx, cfg)
}

func serveLocal(ctx context.Context, addr string, h http.Handler) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
		return nil
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
