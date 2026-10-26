package agent

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"tailscale.com/client/local"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
)

func TestNetAutoSelectsSystem(t *testing.T) {
	f := &fakeLocalAPI{st: runningStatus("100.64.0.8", "fd7a:115c:a1e0::8")}
	lc, done := f.client(t)
	defer done()
	choice, err := chooseNetMode(context.Background(), "auto", statusProbe(lc))
	if err != nil {
		t.Fatal(err)
	}
	if choice.Mode != netSystem {
		t.Fatalf("mode %s why %s", choice.Mode, choice.Why)
	}
	if !strings.Contains(choice.Why, "auto:") || !strings.Contains(choice.Why, "100.64.0.8") {
		t.Fatalf("why %q", choice.Why)
	}
	if choice.Client == nil {
		t.Fatal("missing local client")
	}
}

func TestNetAutoFallsBackToTsnet(t *testing.T) {
	choice, err := chooseNetMode(context.Background(), "auto", func(context.Context) (*ipnstate.Status, *local.Client, error) {
		return nil, nil, os.ErrNotExist
	})
	if err != nil {
		t.Fatal(err)
	}
	if choice.Mode != netTsnet || !strings.Contains(choice.Why, "not usable") {
		t.Fatalf("%+v", choice)
	}

	stopped := runningStatus("100.64.0.1")
	stopped.BackendState = "Stopped"
	choice, err = chooseNetMode(context.Background(), "auto", func(context.Context) (*ipnstate.Status, *local.Client, error) {
		return stopped, &local.Client{}, nil
	})
	if err != nil || choice.Mode != netTsnet || !strings.Contains(choice.Why, "Stopped") {
		t.Fatalf("%+v %v", choice, err)
	}

	choice, err = chooseNetMode(context.Background(), "tsnet", func(context.Context) (*ipnstate.Status, *local.Client, error) {
		t.Fatal("tsnet mode must not probe tailscaled")
		return nil, nil, nil
	})
	if err != nil || choice.Mode != netTsnet {
		t.Fatalf("%+v %v", choice, err)
	}
}

func TestNetSystemRequiresRunningTailscaled(t *testing.T) {
	_, err := chooseNetMode(context.Background(), "system", func(context.Context) (*ipnstate.Status, *local.Client, error) {
		st := runningStatus("100.64.0.1")
		st.BackendState = "NeedsLogin"
		return st, &local.Client{}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "NeedsLogin") {
		t.Fatalf("got %v", err)
	}

	_, err = chooseNetMode(context.Background(), "nope", func(context.Context) (*ipnstate.Status, *local.Client, error) {
		t.Fatal("invalid mode must not probe")
		return nil, nil, nil
	})
	if err == nil || !strings.Contains(err.Error(), "auto, system, or tsnet") {
		t.Fatalf("got %v", err)
	}
}

func TestNetSystemPermissionError(t *testing.T) {
	probe := func(context.Context) (*ipnstate.Status, *local.Client, error) {
		return nil, nil, os.ErrPermission
	}
	_, err := chooseNetMode(context.Background(), "system", probe)
	if err == nil || !isLocalAPIPermission(err) && !strings.Contains(strings.ToLower(err.Error()), "permission") {
		t.Fatal(err)
	}
	if !strings.Contains(err.Error(), "sudo tailscale set --operator=$USER") && !strings.Contains(err.Error(), "LocalAPI") {
		t.Fatalf("hint missing: %v", err)
	}
	choice, err := chooseNetMode(context.Background(), "auto", probe)
	if err != nil || choice.Mode != netTsnet {
		t.Fatalf("auto should fall back, got %+v %v", choice, err)
	}
	if !strings.Contains(choice.Why, "operator=$USER") && !strings.Contains(choice.Why, "LocalAPI") {
		t.Fatalf("fallback why %q", choice.Why)
	}
}

func TestSystemWhoIsAuth(t *testing.T) {
	parent := t.TempDir()
	sub := filepath.Join(parent, "a")
	sib := filepath.Join(parent, "b")
	if err := os.MkdirAll(sub, 0o755); err != nil || os.MkdirAll(sib, 0o755) != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "f.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	grant := tailcfg.RawMessage(`{"read":["` + sub + `"]}`)
	f := &fakeLocalAPI{st: runningStatus("100.64.0.1")}
	f.who = map[string]*apitype.WhoIsResponse{
		"100.90.0.2:9": whoIs("ada@example.com", "ada", nil),
		"100.90.0.3:9": whoIs("bob@example.com", "bob", tailcfg.PeerCapMap{
			tailcfg.PeerCapability(TailfileCap): []tailcfg.RawMessage{grant},
		}),
	}
	lc, done := f.client(t)
	defer done()

	s := newServer("n", netSystem, "ada@example.com", Roots{Read: []string{parent}}, 1024)
	s.whois = whoIsFunc(lc)
	ts := httptest.NewServer(s.TailnetHandler())
	defer ts.Close()

	code := func(remote, path string) int {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, ts.URL+"/v1/fs/list?path="+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.RemoteAddr = remote
		rr := httptest.NewRecorder()
		s.TailnetHandler().ServeHTTP(rr, req)
		return rr.Code
	}
	if code("100.90.0.2:9", sib) != 200 {
		t.Fatal("same user should keep the configured roots")
	}
	if code("100.90.0.3:9", sub) != 200 {
		t.Fatal("grant should allow the other user inside the granted root")
	}
	if code("100.90.0.3:9", sib) != 403 {
		t.Fatalf("grant should hide the sibling, got %d", code("100.90.0.3:9", sib))
	}
	if code("100.90.0.9:1", sub) != 401 {
		t.Fatal("unknown peer should be unauthenticated")
	}
}

func TestTailListenAddrsRejectsNonTailscale(t *testing.T) {
	for _, raw := range []string{"0.0.0.0", "127.0.0.1", "::", "8.8.8.8", "192.168.1.1", "fd00::1"} {
		_, err := tailListenAddrs([]netip.Addr{netip.MustParseAddr(raw)}, 8644)
		if err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	got, err := tailListenAddrs([]netip.Addr{
		netip.MustParseAddr("100.64.0.1"),
		netip.MustParseAddr("100.127.255.2"),
		netip.MustParseAddr("fd7a:115c:a1e0::1"),
	}, 8644)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"100.64.0.1:8644", "100.127.255.2:8644", "[fd7a:115c:a1e0::1]:8644"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v", got)
	}
	if _, err := selfTailscaleIPs(&ipnstate.Status{Self: &ipnstate.PeerStatus{
		TailscaleIPs: []netip.Addr{netip.MustParseAddr("0.0.0.0")},
	}}); err == nil {
		t.Fatal("status with 0.0.0.0 should be refused")
	}
}

func TestSystemRebindOnIPChange(t *testing.T) {
	f := &fakeLocalAPI{st: runningStatus("100.64.0.1")}
	lc, done := f.client(t)
	defer done()

	var mu sync.Mutex
	var got [][]string
	b := &systemBackend{
		lc:   lc,
		port: 8644,
		poll: 15 * time.Millisecond,
		listen: func(addrs []string) error {
			for _, addr := range addrs {
				host, _, err := net.SplitHostPort(addr)
				if err != nil {
					return err
				}
				ip, err := netip.ParseAddr(host)
				if err != nil || !isTailscaleAddr(ip) || ip.IsUnspecified() {
					t.Errorf("non-tailscale bind %s", addr)
				}
			}
			mu.Lock()
			got = append(got, append([]string(nil), addrs...))
			n := len(got)
			mu.Unlock()
			if n == 1 {
				f.setIPs("100.64.0.2", "fd7a:115c:a1e0::2")
			}
			return nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() { errc <- b.Serve(ctx, http.NewServeMux()) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not return")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) < 2 {
		t.Fatalf("binds %v", got)
	}
	if got[0][0] != "100.64.0.1:8644" {
		t.Fatalf("first bind %v", got[0])
	}
	joined := strings.Join(got[1], ",")
	if !strings.Contains(joined, "100.64.0.2:8644") || !strings.Contains(joined, "[fd7a:115c:a1e0::2]:8644") {
		t.Fatalf("rebind %v", got[1])
	}
	for _, batch := range got {
		for _, addr := range batch {
			if strings.HasPrefix(addr, "0.0.0.0") || strings.HasPrefix(addr, ":") {
				t.Fatalf("wildcard bind %s", addr)
			}
		}
	}
}

func TestProbeSystemTailscaledReturns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _, _ = probeSystemTailscaled(ctx)
	if ctx.Err() != nil {
		t.Fatal("probe hung")
	}
}

type fakeLocalAPI struct {
	mu   sync.Mutex
	st   *ipnstate.Status
	who  map[string]*apitype.WhoIsResponse
	perm bool
}

func (f *fakeLocalAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.perm {
		http.Error(w, "permission denied", http.StatusForbidden)
		return
	}
	switch {
	case strings.HasPrefix(r.URL.Path, "/localapi/v0/status"):
		if err := json.NewEncoder(w).Encode(f.st); err != nil {
			http.Error(w, err.Error(), 500)
		}
	case strings.HasPrefix(r.URL.Path, "/localapi/v0/whois"):
		who := f.who[r.URL.Query().Get("addr")]
		if who == nil {
			http.NotFound(w, r)
			return
		}
		if err := json.NewEncoder(w).Encode(who); err != nil {
			http.Error(w, err.Error(), 500)
		}
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeLocalAPI) client(t *testing.T) (*local.Client, func()) {
	t.Helper()
	ts := httptest.NewServer(f)
	lc := &local.Client{
		OmitAuth: true,
		Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "tcp", ts.Listener.Addr().String())
		},
	}
	return lc, ts.Close
}

func (f *fakeLocalAPI) setIPs(raw ...string) {
	ips := make([]netip.Addr, len(raw))
	for i, s := range raw {
		ips[i] = netip.MustParseAddr(s)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.st.Self.TailscaleIPs = ips
}

func statusProbe(lc *local.Client) systemProbe {
	return func(ctx context.Context) (*ipnstate.Status, *local.Client, error) {
		st, err := lc.Status(ctx)
		return st, lc, err
	}
}

func runningStatus(ips ...string) *ipnstate.Status {
	addrs := make([]netip.Addr, len(ips))
	for i, s := range ips {
		addrs[i] = netip.MustParseAddr(s)
	}
	uid := tailcfg.UserID(1)
	return &ipnstate.Status{
		BackendState: ipn.Running.String(),
		Self: &ipnstate.PeerStatus{
			ID:           "self",
			HostName:     "laptop",
			DNSName:      "laptop.tailnet.ts.net.",
			UserID:       uid,
			Online:       true,
			TailscaleIPs: addrs,
		},
		User: map[tailcfg.UserID]tailcfg.UserProfile{
			uid: {ID: uid, LoginName: "ada@example.com"},
		},
	}
}

func whoIs(login, name string, caps tailcfg.PeerCapMap) *apitype.WhoIsResponse {
	return &apitype.WhoIsResponse{
		Node: &tailcfg.Node{
			Name:         name + ".tailnet.ts.net.",
			ComputedName: name,
			StableID:     tailcfg.StableNodeID(name),
		},
		UserProfile: &tailcfg.UserProfile{ID: 2, LoginName: login},
		CapMap:      caps,
	}
}

func TestSystemNodeListFromStatus(t *testing.T) {
	f := &fakeLocalAPI{st: runningStatus("100.64.0.1")}
	lc, done := f.client(t)
	defer done()
	s := newServer("laptop", netSystem, "ada@example.com", Roots{}, 1024)
	r := &tsResolver{lc: lc, srv: s, port: 8644, http: directHTTPClient()}
	nodes, err := r.Nodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || !nodes[0].Self || nodes[0].Name != "laptop" {
		t.Fatalf("%+v", nodes)
	}
	if len(nodes[0].Addrs) != 1 || nodes[0].Addrs[0] != "100.64.0.1" {
		t.Fatalf("addrs %+v", nodes[0])
	}
	if nodes[0].AgentURL != "http://100.64.0.1:8644" {
		t.Fatalf("url %s", nodes[0].AgentURL)
	}
}
