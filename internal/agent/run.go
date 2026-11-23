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

	"tailfile.local/internal/guitoken"
)

// Config is the process configuration for tailfile and for embedded GUI sessions.
type Config struct {
	Hostname   string
	StateDir   string
	Port       int
	LocalAddr  string
	Owner      string
	ReadRoots  []string
	WriteRoots []string
	ReadOnly   bool
	Demo       bool
	AuthKey    string
	ChunkSize  int
	Ephemeral  bool
	// Net is system (default) or tsnet (experimental). Empty or legacy auto resolves to system.
	Net string

	// quietDefaults suppresses the default-root log line. Service control
	// verbs other than install set it: their flags describe no running agent.
	quietDefaults bool
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
	c.LocalAddr = strings.TrimSpace(c.LocalAddr)
	if c.LocalAddr != "" {
		if err := requireLoopbackAddr(c.LocalAddr); err != nil {
			return err
		}
	}
	if c.ChunkSize == 0 {
		c.ChunkSize = defaultChunkSize
	}
	// TS_AUTHKEY is not copied onto Config. resolveAuthKey reads it at tsnet
	// start so the environment wins over --authkey and over the ts-authkey file,
	// and so the key is not kept on the config struct just to be logged later.
	if c.Owner == "" {
		c.Owner = os.Getenv("TAILFILE_OWNER")
	}
	switch strings.ToLower(strings.TrimSpace(c.Net)) {
	case "", netSystem, netAuto:
		c.Net = netSystem
	case netTsnet:
		c.Net = netTsnet
	default:
		return fmt.Errorf("--net must be system or tsnet (experimental), not %q", c.Net)
	}
	if !c.Demo && c.StateDir == "" {
		dir, err := DefaultStateDir(c.Hostname)
		if err != nil {
			return err
		}
		c.StateDir = dir
	}
	if c.StateDir != "" {
		if err := os.MkdirAll(c.StateDir, 0o700); err != nil {
			return err
		}
		if err := os.Chmod(c.StateDir, 0o700); err != nil {
			return err
		}
	}
	if c.ReadOnly {
		c.WriteRoots = nil
	}
	if !c.Demo && len(c.ReadRoots) == 0 {
		if len(c.WriteRoots) > 0 {
			c.ReadRoots = append([]string(nil), c.WriteRoots...)
		} else {
			home, err := userHomeDir()
			if err != nil {
				return fmt.Errorf("no --read root and no home directory: %w", err)
			}
			dir, err := ensureShareDir(home)
			if err != nil {
				return fmt.Errorf("default read root: %w", err)
			}
			c.ReadRoots = []string{dir}
			if !c.ReadOnly && len(c.WriteRoots) == 0 {
				c.WriteRoots = []string{dir}
			}
			if !c.quietDefaults {
				if c.ReadOnly || len(c.WriteRoots) == 0 {
					log.Printf("tailfile: read root defaulting to %s (read-only)", dir)
				} else {
					log.Printf("tailfile: read/write root defaulting to %s (pass --read-only to disable writes)", dir)
				}
			}
		}
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

func listenLoopback(addr string) (net.Listener, error) {
	if err := requireLoopbackAddr(addr); err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", addr, err)
	}
	return ln, nil
}

func serveListener(ctx context.Context, ln net.Listener, h http.Handler) error {
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

func serveLocal(ctx context.Context, addr string, h http.Handler) error {
	ln, err := listenLoopback(addr)
	if err != nil {
		return err
	}
	return serveListener(ctx, ln, h)
}
