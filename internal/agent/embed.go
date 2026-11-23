package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ConnMode describes how the GUI reaches srv.LocalHandler().
type ConnMode uint8

const (
	ConnOffline  ConnMode = iota // AttachOnly / explicit --agent or token mismatch and daemon unreachable
	ConnAttached                 // Dialing running background daemon over loopback TCP
	ConnEmbedded                 // In-process Server via memRoundTripper (zero TCP port)
	ConnDemo                     // In-process isolated demo tailnet via memRoundTripper (zero TCP port)
)

func (m ConnMode) String() string {
	switch m {
	case ConnAttached:
		return "attached"
	case ConnEmbedded:
		return "embedded"
	case ConnDemo:
		return "demo"
	default:
		return "offline"
	}
}

// ConnectConfig controls daemon discovery vs in-process embedding.
// Connect always probes an existing daemon FIRST before touching StateDir or gui-token.
type ConnectConfig struct {
	Agent        Config
	LocalURL     string
	ExplicitAddr bool // True when --agent or TAILFILE_AGENT was explicitly passed
	TokenFile    string
	AttachOnly   bool
	ProbeTimeout time.Duration
}

// Conn owns one GUI-to-agent connection. Callers talk exclusively through Conn.Client.
type Conn struct {
	Client   *Client
	Mode     ConnMode
	LocalURL string
	StateDir string
	ProbeErr error

	closeOnce sync.Once
	cleanup   func() error
}

// Close is idempotent: shuts down any embedded runtime and releases locks/temp state.
// On ConnAttached or ConnOffline, Close closes idle HTTP connections without touching the daemon.
func (c *Conn) Close() error {
	if c == nil {
		return nil
	}
	var err error
	c.closeOnce.Do(func() {
		if c.cleanup != nil {
			err = c.cleanup()
		}
		if c.Client != nil && c.Client.HTTP != nil {
			c.Client.HTTP.CloseIdleConnections()
		}
	})
	return err
}

// Connect establishes the single *Client path for the GUI:
//  1. Demo mode -> StartEmbedded (ConnDemo).
//  2. iOS / Android -> StartEmbedded (ConnEmbedded, zero TCP port).
//  3. Explicit --agent / --local / --attach-only -> probe loopback TCP;
//     explicit/attach-only failure returns ConnOffline with ProbeErr.
//  4. Default desktop -> StartEmbedded (ConnEmbedded, in-process memRoundTripper,
//     zero TCP :8645 port, zero disk gui-token).
func Connect(ctx context.Context, cfg ConnectConfig) (*Conn, error) {
	if cfg.Agent.Demo {
		return StartEmbedded(ctx, cfg.Agent)
	}
	if runtime.GOOS == "ios" || runtime.GOOS == "android" {
		return StartEmbedded(ctx, cfg.Agent)
	}

	probeCfg := cfg.Agent
	probeCfg.quietDefaults = true
	if err := probeCfg.applyDefaults(); err != nil {
		return nil, err
	}
	localURL := strings.TrimRight(strings.TrimSpace(cfg.LocalURL), "/")
	if localURL == "" && probeCfg.LocalAddr == "" && !cfg.ExplicitAddr && !cfg.AttachOnly {
		return StartEmbedded(ctx, cfg.Agent)
	}
	if localURL == "" {
		if probeCfg.LocalAddr != "" {
			localURL = "http://" + probeCfg.LocalAddr
		} else {
			localURL = "http://127.0.0.1:8645"
		}
	}
	timeout := cfg.ProbeTimeout
	if timeout <= 0 {
		timeout = 500 * time.Millisecond
	}

	token, tokErr := LoadGUIToken(cfg.TokenFile, probeCfg.StateDir)
	tcpClient := NewClient(localURL)
	tcpClient.HTTP = &http.Client{Timeout: 60 * time.Second}
	tcpClient.Token = token

	if tokErr == nil {
		pctx, cancel := context.WithTimeout(ctx, timeout)
		err := tcpClient.Health(pctx)
		cancel()
		if err == nil {
			return &Conn{
				Client:   tcpClient,
				Mode:     ConnAttached,
				LocalURL: localURL,
				StateDir: probeCfg.StateDir,
			}, nil
		}
		var se *StatusError
		if errors.As(err, &se) && (se.Code == http.StatusUnauthorized || se.Code == http.StatusForbidden) {
			return &Conn{
				Client:   tcpClient,
				Mode:     ConnOffline,
				LocalURL: localURL,
				StateDir: probeCfg.StateDir,
				ProbeErr: fmt.Errorf("agent on %s rejected gui-token (%w)", localURL, err),
			}, nil
		}
		if cfg.ExplicitAddr || cfg.AttachOnly {
			return &Conn{
				Client:   tcpClient,
				Mode:     ConnOffline,
				LocalURL: localURL,
				StateDir: probeCfg.StateDir,
				ProbeErr: err,
			}, nil
		}
	} else if cfg.ExplicitAddr || cfg.AttachOnly {
		return &Conn{
			Client:   tcpClient,
			Mode:     ConnOffline,
			LocalURL: localURL,
			StateDir: probeCfg.StateDir,
			ProbeErr: tokErr,
		}, nil
	} else {
		// No token yet: check if another agent already bound LocalURL so we do not
		// report misleading errors when starting embedded would fail or conflict.
		pctx, cancel := context.WithTimeout(ctx, timeout)
		err := tcpClient.Health(pctx)
		cancel()
		var se *StatusError
		if err == nil || (errors.As(err, &se) && (se.Code == http.StatusUnauthorized || se.Code == http.StatusForbidden)) {
			return &Conn{
				Client:   tcpClient,
				Mode:     ConnOffline,
				LocalURL: localURL,
				StateDir: probeCfg.StateDir,
				ProbeErr: tokErr,
			}, nil
		}
	}

	return StartEmbedded(ctx, cfg.Agent)
}

// StartEmbedded runs the full agent orchestrator in-process over memRoundTripper
// without binding a loopback TCP port and without writing gui-token to disk.
func StartEmbedded(ctx context.Context, cfg Config) (*Conn, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var tempStateDir string
	if cfg.Demo && strings.TrimSpace(cfg.StateDir) == "" {
		dir, err := os.MkdirTemp("", "tailfile-demo-state-")
		if err != nil {
			return nil, err
		}
		tempStateDir = dir
		cfg.StateDir = dir
	}
	if err := cfg.applyDefaults(); err != nil {
		if tempStateDir != "" {
			_ = os.RemoveAll(tempStateDir)
		}
		return nil, err
	}

	embeddedBase := "http://127.0.0.1"
	if cfg.LocalAddr != "" {
		embeddedBase = "http://" + cfg.LocalAddr
	}

	if cfg.Demo {
		dc, err := startDemoCluster(cfg, false)
		if err != nil {
			if tempStateDir != "" {
				_ = os.RemoveAll(tempStateDir)
			}
			return nil, err
		}
		c := newMemClient(embeddedBase, dc.token, dc.lb.Handler())
		return &Conn{
			Client:   c,
			Mode:     ConnDemo,
			LocalURL: embeddedBase,
			StateDir: cfg.StateDir,
			cleanup: func() error {
				dc.Close()
				if tempStateDir != "" {
					_ = os.RemoveAll(tempStateDir)
				}
				return nil
			},
		}, nil
	}

	lock, err := acquireStateLock(cfg.StateDir)
	if err != nil {
		return nil, err
	}
	rt, err := startTailnetRuntime(ctx, cfg, false)
	if err != nil {
		_ = lock.Close()
		return nil, err
	}
	c := newMemClient(embeddedBase, rt.token, rt.srv.LocalHandler())
	return &Conn{
		Client:   c,
		Mode:     ConnEmbedded,
		LocalURL: embeddedBase,
		StateDir: cfg.StateDir,
		cleanup: func() error {
			rt.Close()
			return lock.Close()
		},
	}, nil
}

func newMemClient(base, token string, handler http.Handler) *Client {
	c := NewClient(base)
	c.Token = token
	c.HTTP = &http.Client{
		Transport: &memRoundTripper{handler: handler},
		Timeout:   60 * time.Second,
	}
	return c
}

// ReconnectClient constructs an attached TCP client for an arbitrary URL/token
// when the user changes Agent address in Settings.
func ReconnectClient(base, token string) *Client {
	c := NewClient(base)
	c.Token = token
	c.HTTP = &http.Client{Timeout: 60 * time.Second}
	return c
}

var _ io.Closer = (*Conn)(nil)
