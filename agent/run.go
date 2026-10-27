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

	"tailfile.local/agent/guitoken"
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
	// Net is auto, system, or tsnet. Empty means auto.
	Net string
}

var userHomeDir = os.UserHomeDir

func DefaultHostname() (string, error) {
	return guitoken.DefaultHostname(), nil
}

func DefaultStateDir(hostname string) (string, error) {
	return guitoken.DefaultStateDir(hostname), nil
}

// LoadGUIToken reads the per-start bearer. tokenFile wins. Otherwise the
// file is <state dir>/gui-token, using stateDir or the default state dir.
// The token value is not included in errors.
func LoadGUIToken(tokenFile, stateDir string) (string, error) {
	return guitoken.Load(tokenFile, stateDir)
}

func ensureShareDir(home string) (string, error) {
	if strings.TrimSpace(home) == "" {
		return "", errors.New("no home directory")
	}
	dir := filepath.Join(home, "Tailfile")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func DefaultCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil || strings.TrimSpace(base) == "" {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "tailfile")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func (c *Config) applyDefaults() error {
	if c.Hostname == "" {
		host, err := DefaultHostname()
		if err != nil {
			return err
		}
		c.Hostname = host
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
	// TS_AUTHKEY is not copied onto Config. resolveAuthKey reads it at tsnet
	// start so the environment wins over -authkey and over the ts-authkey file,
	// and so the key is not kept on the config struct just to be logged later.
	if c.Owner == "" {
		c.Owner = os.Getenv("TAILFILE_OWNER")
	}
	switch strings.ToLower(strings.TrimSpace(c.Net)) {
	case "", netAuto:
		c.Net = netAuto
	case netSystem, netTsnet:
		c.Net = strings.ToLower(c.Net)
	default:
		return fmt.Errorf("-net must be auto, system, or tsnet, not %q", c.Net)
	}
	if c.StateDir == "" {
		dir, err := DefaultStateDir(c.Hostname)
		if err != nil {
			return err
		}
		c.StateDir = dir
	}
	if err := os.MkdirAll(c.StateDir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(c.StateDir, 0o700); err != nil {
		return err
	}
	if !c.Demo && len(c.ReadRoots) == 0 {
		home, err := userHomeDir()
		if err != nil {
			return fmt.Errorf("no -read root and no home directory: %w", err)
		}
		dir, err := ensureShareDir(home)
		if err != nil {
			return fmt.Errorf("default read root: %w", err)
		}
		c.ReadRoots = []string{dir}
		log.Printf("tailfile: read root defaulting to %s (read-only until -write is set)", dir)
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

func requireLoopbackAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("local listen address %q must be host:port on 127.0.0.1 or localhost", addr)
	}
	if !hostIsLoopback(host) {
		return fmt.Errorf("local API must listen on 127.0.0.1 or localhost, not %q", addr)
	}
	return nil
}

func serveLocal(ctx context.Context, addr string, h http.Handler) error {
	if err := requireLoopbackAddr(addr); err != nil {
		return err
	}
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
