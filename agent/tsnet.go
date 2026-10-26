package agent

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"tailscale.com/client/local"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tsnet"
)

func runTailnet(ctx context.Context, cfg Config) error {
	roots := Roots{Read: cfg.ReadRoots, Write: cfg.WriteRoots}
	srv := newServer(cfg.Hostname, "tsnet", cfg.Owner, roots, cfg.ChunkSize)
	srv.cacheDir = cfg.StateDir
	ts := &tsnet.Server{
		Hostname:  cfg.Hostname,
		Dir:       cfg.StateDir,
		AuthKey:   cfg.AuthKey,
		Ephemeral: cfg.Ephemeral,
		UserLogf:  log.Printf,
	}
	// AuthKey and TS_AUTHKEY are read by tsnet. They are never logged here.
	if cfg.AuthKey == "" {
		log.Printf("tailfile: no TS_AUTHKEY — tsnet will print an interactive login URL on first run")
	} else {
		log.Printf("tailfile: using auth key from the environment or -authkey (value not logged)")
	}
	ln, err := ts.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
	if err != nil {
		_ = ts.Close()
		return fmt.Errorf("tsnet listen: %w", err)
	}
	lc, err := ts.LocalClient()
	if err != nil {
		ln.Close()
		_ = ts.Close()
		return err
	}
	srv.whois = func(r *http.Request) (Identity, error) {
		who, err := lc.WhoIs(r.Context(), r.RemoteAddr)
		if err != nil {
			return Identity{}, err
		}
		id := Identity{}
		if who.UserProfile != nil {
			id.Login = who.UserProfile.LoginName
			id.UserID = fmt.Sprint(who.UserProfile.ID)
		}
		if who.Node != nil {
			id.NodeName = who.Node.ComputedName
			id.NodeID = string(who.Node.StableID)
		}
		return id, nil
	}
	srv.outbound = ts.HTTPClient()
	res := &tsResolver{lc: lc, srv: srv, port: cfg.Port, http: srv.outbound}
	srv.resolver = res

	go watchOwner(ctx, lc, srv, cfg.Owner)

	tail := &http.Server{Handler: srv.TailnetHandler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := tail.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("tailfile tailnet http: %v", err)
		}
	}()
	log.Printf("tailfile %s listening on the tailnet :%d and http://%s", cfg.Hostname, cfg.Port, cfg.LocalAddr)
	log.Printf("state: %s", cfg.StateDir)
	if len(cfg.WriteRoots) == 0 {
		log.Printf("writes are disabled — pass -write to allow paste onto this node")
	}

	err = serveLocal(ctx, cfg.LocalAddr, srv.LocalHandler())
	shut, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = tail.Shutdown(shut)
	_ = ts.Close()
	return err
}

func watchOwner(ctx context.Context, lc *local.Client, srv *Server, configured string) {
	if configured != "" {
		srv.setOwner(configured)
		log.Printf("tailfile owner (configured): %s", configured)
		return
	}
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		if login := selfLogin(ctx, lc); login != "" {
			srv.setOwner(login)
			log.Printf("tailfile owner (this node's tailnet user): %s", login)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func selfLogin(ctx context.Context, lc *local.Client) string {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	st, err := lc.Status(ctx)
	if err != nil || st == nil || st.Self == nil {
		return ""
	}
	if u, ok := st.User[st.Self.UserID]; ok {
		return u.LoginName
	}
	return ""
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
