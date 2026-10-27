package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"tailscale.com/client/local"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
	"tailscale.com/tsnet"
)

func runTailnet(ctx context.Context, cfg Config) error {
	srv, err := prepareNode(cfg)
	if err != nil {
		return err
	}
	choice, err := chooseNetMode(ctx, cfg.Net, probeSystemTailscaled)
	if err != nil {
		return err
	}
	log.Printf("tailfile: network mode %s (%s)", choice.Mode, choice.Why)

	var backend tailBackend
	switch choice.Mode {
	case netSystem:
		backend = &systemBackend{lc: choice.Client, port: cfg.Port}
		srv.mode = netSystem
	case netTsnet:
		b, err := startTsnet(cfg)
		if err != nil {
			return err
		}
		backend = b
		srv.mode = netTsnet
	default:
		return fmt.Errorf("unknown network mode %q", choice.Mode)
	}
	defer backend.Close()

	srv.whois = whoIsFunc(backend.Local())
	srv.outbound = backend.HTTP()
	srv.resolver = &tsResolver{lc: backend.Local(), srv: srv, port: cfg.Port, http: backend.HTTP()}

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	ownerErr := make(chan error, 1)
	go func() {
		if err := watchOwner(runCtx, backend.Local(), srv, cfg.Owner); err != nil {
			log.Printf("tailfile: %v", err)
			ownerErr <- err
			cancelRun()
		}
	}()
	backendErr := make(chan error, 1)
	go func() {
		if err := backend.Serve(runCtx, srv.TailnetHandler()); err != nil && runCtx.Err() == nil {
			log.Printf("tailfile tailnet: %v", err)
			backendErr <- err
			cancelRun()
		}
	}()

	log.Printf("tailfile %s GUI on http://%s (tailnet port %d, mode %s)", cfg.Hostname, cfg.LocalAddr, cfg.Port, backend.Mode())
	log.Printf("state: %s", cfg.StateDir)
	log.Printf("drag-out cache: %s", srv.cacheDir)
	if len(cfg.WriteRoots) == 0 {
		log.Printf("writes are disabled — pass -write to allow paste onto this node")
	}

	localErr := serveLocal(runCtx, cfg.LocalAddr, srv.LocalHandler())
	cancelRun()
	select {
	case err := <-backendErr:
		return err
	default:
	}
	select {
	case oerr := <-ownerErr:
		return oerr
	default:
		return localErr
	}
}

func prepareNode(cfg Config) (*Server, error) {
	roots := Roots{Read: cfg.ReadRoots, Write: cfg.WriteRoots}
	srv := newServer(cfg.Hostname, "", cfg.Owner, roots, cfg.ChunkSize)
	cacheDir, err := DefaultCacheDir()
	if err != nil {
		return nil, err
	}
	srv.cacheDir = cacheDir
	if err := srv.attachState(cfg.StateDir); err != nil {
		return nil, err
	}
	token, err := writeGUIToken(cfg.StateDir)
	if err != nil {
		return nil, err
	}
	srv.guiToken = token
	return srv, nil
}

func whoIsFunc(lc *local.Client) func(*http.Request) (Identity, error) {
	return func(r *http.Request) (Identity, error) {
		if lc == nil {
			return Identity{}, statusErr(401, "not authenticated")
		}
		who, err := lc.WhoIs(r.Context(), r.RemoteAddr)
		if err != nil {
			return Identity{}, err
		}
		return identityFromWhoIs(who), nil
	}
}

func identityFromWhoIs(who *apitype.WhoIsResponse) Identity {
	id := Identity{}
	if who == nil {
		return id
	}
	if who.UserProfile != nil {
		id.Login = who.UserProfile.LoginName
		id.UserID = fmt.Sprint(who.UserProfile.ID)
	}
	if who.Node != nil {
		id.NodeName = who.Node.ComputedName
		id.NodeID = string(who.Node.StableID)
	}
	cap := tailcfg.PeerCapability(TailfileCap)
	if who.CapMap.HasCapability(cap) {
		id.GrantPresent = true
		for _, raw := range who.CapMap[cap] {
			id.GrantRaw = append(id.GrantRaw, append([]byte(nil), raw...))
		}
	}
	return id
}

type tsnetBackend struct {
	ts   *tsnet.Server
	ln   net.Listener
	lc   *local.Client
	http *http.Client
}

func startTsnet(cfg Config) (*tsnetBackend, error) {
	key, source := resolveAuthKey(cfg)
	ts := &tsnet.Server{
		Hostname:  cfg.Hostname,
		Dir:       cfg.StateDir,
		AuthKey:   key,
		Ephemeral: cfg.Ephemeral,
		UserLogf:  log.Printf,
	}
	// The key is handed to tsnet and never written to the log.
	switch source {
	case "":
		log.Printf("tailfile: no auth key (TS_AUTHKEY, -authkey, or %s) — tsnet will print an interactive login URL on first run", filepath.Join(cfg.StateDir, AuthKeyFile))
	case "TS_AUTHKEY":
		log.Printf("tailfile: using auth key from TS_AUTHKEY (value not logged)")
	case "-authkey":
		log.Printf("tailfile: using auth key from -authkey (value not logged)")
	default:
		log.Printf("tailfile: using auth key from %s (value not logged)", filepath.Join(cfg.StateDir, AuthKeyFile))
	}
	ln, err := ts.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
	if err != nil {
		_ = ts.Close()
		return nil, fmt.Errorf("tsnet listen: %w", err)
	}
	lc, err := ts.LocalClient()
	if err != nil {
		ln.Close()
		_ = ts.Close()
		return nil, err
	}
	return &tsnetBackend{ts: ts, ln: ln, lc: lc, http: ts.HTTPClient()}, nil
}

func (b *tsnetBackend) Mode() string         { return netTsnet }
func (b *tsnetBackend) Local() *local.Client { return b.lc }
func (b *tsnetBackend) HTTP() *http.Client   { return b.http }
func (b *tsnetBackend) Close() error         { return b.ts.Close() }

func (b *tsnetBackend) Serve(ctx context.Context, h http.Handler) error {
	tail := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- tail.Serve(b.ln) }()
	select {
	case <-ctx.Done():
		shut, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = tail.Shutdown(shut)
		return nil
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func watchOwner(ctx context.Context, lc *local.Client, srv *Server, configured string) error {
	if strings.TrimSpace(configured) != "" {
		srv.setOwner(configured)
		log.Printf("tailfile owner (configured): %s", configured)
		return nil
	}
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		login, tagged, ok := selfIdentity(ctx, lc)
		if ok {
			owner, err := ResolveOwner(login, tagged, "")
			if err != nil {
				return err
			}
			srv.setOwner(owner)
			log.Printf("tailfile owner (this node's tailnet user): %s", owner)
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

func selfIdentity(ctx context.Context, lc *local.Client) (login string, tagged, ok bool) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	st, err := lc.Status(ctx)
	if err != nil || st == nil || st.Self == nil {
		return "", false, false
	}
	tagged = st.Self.IsTagged()
	if u, found := st.User[st.Self.UserID]; found {
		login = u.LoginName
	}
	if login == "" && !tagged {
		return "", false, false
	}
	return login, tagged, true
}

type tsResolver struct {
	lc   *local.Client
	srv  *Server
	port int
	http *http.Client

	mu    sync.Mutex
	cache []NodeInfo
	at    time.Time
}

func (r *tsResolver) Nodes(ctx context.Context) ([]NodeInfo, error) {
	r.mu.Lock()
	if time.Since(r.at) < 3*time.Second && r.cache != nil {
		out := append([]NodeInfo(nil), r.cache...)
		r.mu.Unlock()
		return out, nil
	}
	r.mu.Unlock()

	st, err := r.lc.Status(ctx)
	if err != nil {
		return nil, err
	}
	nodes := make([]NodeInfo, 0, 1+len(st.Peer))
	if st.Self != nil {
		nodes = append(nodes, peerInfo(st, st.Self, true, r.port))
	}
	for _, p := range st.Peer {
		nodes = append(nodes, peerInfo(st, p, false, r.port))
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Self != nodes[j].Self {
			return nodes[i].Self
		}
		return nodes[i].Name < nodes[j].Name
	})
	var wg sync.WaitGroup
	for i := range nodes {
		if nodes[i].Self {
			info := r.srv.Info()
			nodes[i].AgentOK = true
			nodes[i].ReadOnly = info.ReadOnly
			nodes[i].ReadRoots = info.ReadRoots
			nodes[i].Online = true
			continue
		}
		if nodes[i].AgentURL == "" {
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pctx, cancel := context.WithTimeout(ctx, 700*time.Millisecond)
			defer cancel()
			api := &httpAPI{base: nodes[i].AgentURL, http: r.http}
			if err := api.Health(pctx); err != nil {
				return
			}
			nodes[i].AgentOK = true
			if info, err := api.Info(pctx); err == nil {
				nodes[i].ReadOnly = info.ReadOnly
				nodes[i].ReadRoots = info.ReadRoots
			}
		}(i)
	}
	wg.Wait()
	r.mu.Lock()
	r.cache = append([]NodeInfo(nil), nodes...)
	r.at = time.Now()
	r.mu.Unlock()
	return nodes, nil
}

func (r *tsResolver) API(ctx context.Context, id string) (NodeAPI, error) {
	nodes, err := r.Nodes(ctx)
	if err != nil {
		return nil, err
	}
	for _, n := range nodes {
		if n.ID == id || n.Name == id {
			if n.Self {
				return directAPI{s: r.srv}, nil
			}
			if n.AgentURL == "" {
				return nil, statusErr(404, "node %s has no tailscale address", id)
			}
			return &httpAPI{base: n.AgentURL, http: r.http}, nil
		}
	}
	return nil, statusErr(404, "unknown node %s", id)
}

func (r *tsResolver) DialURL(id string) (string, error) {
	nodes, err := r.Nodes(context.Background())
	if err != nil {
		return "", err
	}
	for _, n := range nodes {
		if n.ID == id || n.Name == id {
			if n.AgentURL == "" {
				return "", statusErr(404, "node %s has no tailscale address", id)
			}
			return n.AgentURL, nil
		}
	}
	return "", statusErr(404, "unknown node %s", id)
}

func peerInfo(st *ipnstate.Status, p *ipnstate.PeerStatus, self bool, port int) NodeInfo {
	id := strings.TrimSuffix(p.DNSName, ".")
	if id == "" {
		id = string(p.ID)
	}
	name := p.HostName
	if name == "" {
		name = id
	}
	var addrs []string
	var dial string
	for _, a := range p.TailscaleIPs {
		addrs = append(addrs, a.String())
		if dial == "" && a.Is4() {
			dial = fmt.Sprintf("http://%s:%d", a.String(), port)
		}
	}
	if dial == "" && len(p.TailscaleIPs) > 0 {
		dial = fmt.Sprintf("http://[%s]:%d", p.TailscaleIPs[0].String(), port)
	}
	login := ""
	if st != nil {
		if u, ok := st.User[p.UserID]; ok {
			login = u.LoginName
		}
	}
	return NodeInfo{
		ID: id, Name: name, OS: p.OS, Online: self || p.Online,
		AgentURL: dial, Login: login, Self: self, Addrs: addrs,
	}
}
