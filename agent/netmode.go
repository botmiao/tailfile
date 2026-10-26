package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"tailscale.com/client/local"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/safesocket"
)

const (
	netAuto   = "auto"
	netSystem = "system"
	netTsnet  = "tsnet"
)

// tailBackend is the tailnet transport. system mode dials the host's
// tailscaled; tsnet mode embeds one. Both expose the same LocalAPI client
// so WhoIs, grants, and the node list stay in one place.
type tailBackend interface {
	Mode() string
	Local() *local.Client
	HTTP() *http.Client
	Serve(ctx context.Context, h http.Handler) error
	Close() error
}

type netChoice struct {
	Mode   string
	Why    string
	Client *local.Client
}

type systemProbe func(ctx context.Context) (st *ipnstate.Status, client *local.Client, err error)

func chooseNetMode(ctx context.Context, mode string, probe systemProbe) (netChoice, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = netAuto
	}
	switch mode {
	case netTsnet:
		return netChoice{Mode: netTsnet, Why: "requested with -net tsnet"}, nil
	case netAuto, netSystem:
	default:
		return netChoice{}, fmt.Errorf("-net must be auto, system, or tsnet, not %q", mode)
	}
	st, lc, err := probe(ctx)
	if err != nil {
		err = explainLocalAPI(err)
		if mode == netSystem {
			return netChoice{}, err
		}
		return netChoice{Mode: netTsnet, Why: "system tailscaled is not usable: " + err.Error()}, nil
	}
	if st == nil || st.BackendState != ipn.Running.String() {
		state := "unknown"
		if st != nil && st.BackendState != "" {
			state = st.BackendState
		}
		msg := fmt.Sprintf("tailscaled state is %s, not Running", state)
		if mode == netSystem {
			return netChoice{}, errors.New(msg)
		}
		return netChoice{Mode: netTsnet, Why: msg}, nil
	}
	ips, err := selfTailscaleIPs(st)
	if err != nil {
		if mode == netSystem {
			return netChoice{}, err
		}
		return netChoice{Mode: netTsnet, Why: err.Error()}, nil
	}
	why := "local tailscaled is Running with " + formatIPs(ips)
	if mode == netAuto {
		why = "auto: " + why
	} else {
		why = "requested with -net system; " + why
	}
	return netChoice{Mode: netSystem, Why: why, Client: lc}, nil
}

// systemLocalClient talks to the machine's tailscaled.
// On macOS the zero Client tries the App Store (IPNExtension) and standalone
// (macsys) LocalAPI, discovered via safesocket.LocalTCPPortAndToken, before
// the open-source tailscaled unix socket. UseSocketOnly stays false so both
// paths remain available.
func systemLocalClient() *local.Client {
	return &local.Client{}
}

func probeSystemTailscaled(ctx context.Context) (*ipnstate.Status, *local.Client, error) {
	pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var last error
	// macOS GUI builds publish a sameuserproof file instead of (or in addition
	// to) the unix socket. Other operating systems return ErrNoTokenOnOS
	// immediately, so this probe does not spawn lsof there.
	if _, _, err := safesocket.LocalTCPPortAndToken(); err == nil {
		lc := systemLocalClient()
		st, err := lc.Status(pctx)
		if err == nil {
			return st, lc, nil
		}
		last = fmt.Errorf("macOS Tailscale app LocalAPI: %w", err)
	}
	lc := &local.Client{UseSocketOnly: true}
	st, err := lc.Status(pctx)
	if err != nil {
		if last != nil {
			return nil, nil, fmt.Errorf("%v; tailscaled socket: %w", last, err)
		}
		return nil, nil, err
	}
	return st, lc, nil
}

func explainLocalAPI(err error) error {
	if err == nil || !isLocalAPIPermission(err) {
		return err
	}
	if runtime.GOOS == "linux" {
		return fmt.Errorf("cannot use the system tailscaled (%v). On Linux the LocalAPI socket is often root-only; run: sudo tailscale set --operator=$USER", err)
	}
	return fmt.Errorf("cannot use the system tailscaled (%v). Check that this user can access the Tailscale LocalAPI", err)
}

func isLocalAPIPermission(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "permission denied") || strings.Contains(msg, "operation not permitted") || strings.Contains(msg, "access is denied")
}

func selfTailscaleIPs(st *ipnstate.Status) ([]netip.Addr, error) {
	if st == nil || st.Self == nil || len(st.Self.TailscaleIPs) == 0 {
		return nil, errors.New("tailscaled is Running but has no Tailscale IP")
	}
	out := make([]netip.Addr, 0, len(st.Self.TailscaleIPs))
	seen := map[netip.Addr]bool{}
	for _, a := range st.Self.TailscaleIPs {
		if !a.IsValid() || a.IsUnspecified() || !isTailscaleAddr(a) {
			return nil, fmt.Errorf("refusing to listen on %s: not a Tailscale address", a)
		}
		if !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	return out, nil
}

func isTailscaleAddr(a netip.Addr) bool {
	if a.Is4() {
		b := a.As4()
		return b[0] == 100 && b[1] >= 64 && b[1] <= 127
	}
	if !a.Is6() {
		return false
	}
	b := a.As16()
	return b[0] == 0xfd && b[1] == 0x7a && b[2] == 0x11 && b[3] == 0x5c && b[4] == 0xa1 && b[5] == 0xe0
}

// tailListenAddrs returns host:port strings for the node's own Tailscale IPs.
// It refuses 0.0.0.0, loopback, and any address outside the Tailscale ranges.
func tailListenAddrs(ips []netip.Addr, port int) ([]string, error) {
	if port <= 0 || port > 65535 {
		return nil, fmt.Errorf("tailnet port %d is out of range", port)
	}
	if len(ips) == 0 {
		return nil, errors.New("no Tailscale addresses to bind")
	}
	ordered := append([]netip.Addr(nil), ips...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Compare(ordered[j]) < 0 })
	out := make([]string, 0, len(ordered))
	for _, a := range ordered {
		if !a.IsValid() || a.IsUnspecified() || !isTailscaleAddr(a) {
			return nil, fmt.Errorf("refusing to listen on %s: not a Tailscale address", a)
		}
		if a.Is4() {
			out = append(out, fmt.Sprintf("%s:%d", a, port))
		} else {
			out = append(out, fmt.Sprintf("[%s]:%d", a, port))
		}
	}
	return out, nil
}

func formatIPs(ips []netip.Addr) string {
	parts := make([]string, len(ips))
	for i, a := range ips {
		parts[i] = a.String()
	}
	return strings.Join(parts, ", ")
}

func directHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
	}
}

type systemBackend struct {
	lc     *local.Client
	port   int
	poll   time.Duration
	listen func(addrs []string) error
	http   *http.Client
}

func (b *systemBackend) Mode() string { return netSystem }

func (b *systemBackend) Local() *local.Client { return b.lc }

func (b *systemBackend) HTTP() *http.Client {
	if b.http == nil {
		b.http = directHTTPClient()
	}
	return b.http
}

func (b *systemBackend) Close() error { return nil }

func (b *systemBackend) Serve(ctx context.Context, h http.Handler) error {
	poll := b.poll
	if poll <= 0 {
		poll = 2 * time.Second
	}
	var (
		bound []string
		lns   []net.Listener
		srv   *http.Server
	)
	stop := func() {
		if srv != nil {
			shut, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = srv.Shutdown(shut)
			cancel()
			srv = nil
		}
		for _, ln := range lns {
			_ = ln.Close()
		}
		lns = nil
	}
	defer stop()

	rebind := func() error {
		st, err := b.lc.Status(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if isLocalAPIPermission(err) {
				return explainLocalAPI(err)
			}
			log.Printf("tailfile: system tailscaled status failed (%v); will re-resolve", err)
			stop()
			bound = nil
			return nil
		}
		if st.BackendState != ipn.Running.String() {
			log.Printf("tailfile: tailscaled state is %s; unbinding until it is Running", st.BackendState)
			stop()
			bound = nil
			return nil
		}
		ips, err := selfTailscaleIPs(st)
		if err != nil {
			log.Printf("tailfile: %v; will re-resolve", err)
			stop()
			bound = nil
			return nil
		}
		addrs, err := tailListenAddrs(ips, b.port)
		if err != nil {
			return err
		}
		if sameStrings(bound, addrs) && (srv != nil || (b.listen != nil && len(bound) > 0)) {
			return nil
		}
		stop()
		if b.listen != nil {
			if err := b.listen(addrs); err != nil {
				return err
			}
		} else {
			opened, err := listenAll(addrs)
			if err != nil {
				// The address can be missing for a moment while tailscaled
				// restarts or moves the node's IP. Keep the GUI up and try again.
				log.Printf("tailfile: %v; will re-resolve", err)
				bound = nil
				return nil
			}
			lns = opened
			srv = &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
			for _, ln := range lns {
				go func(ln net.Listener) {
					err := srv.Serve(ln)
					if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
						log.Printf("tailfile tailnet http: %v", err)
					}
				}(ln)
			}
		}
		bound = append([]string(nil), addrs...)
		log.Printf("tailfile: system mode listening on %s", strings.Join(addrs, ", "))
		return nil
	}

	if err := rebind(); err != nil {
		return err
	}
	tick := time.NewTicker(poll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			if err := rebind(); err != nil {
				return err
			}
		}
	}
}

func listenAll(addrs []string) ([]net.Listener, error) {
	var lns []net.Listener
	for _, addr := range addrs {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			for _, opened := range lns {
				_ = opened.Close()
			}
			return nil, fmt.Errorf("listen %s: %w", addr, err)
		}
		lns = append(lns, ln)
	}
	return lns, nil
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
