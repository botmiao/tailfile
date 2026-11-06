package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"net/http/httptest"
)

// LoopSpec is one in-process node. The directories must already exist.
type LoopSpec struct {
	ID    string
	Read  []string
	Write []string
	// Login is the identity this node presents when it calls peers.
	// Empty uses the cluster owner.
	Login string
	// OS is the host OS reported on the node (`macos`, `windows`, `linux`,
	// `android`). Empty stays `loopback`, which is what tests have always seen.
	OS string
}

// Loopback is a set of in-process agents plus a localhost orchestrator.
// It is the test harness and the -demo mode. It is not a tailnet:
// peers trust a header that production WhoIs never reads.
type Loopback struct {
	Owner string
	Token string
	Orch  *Server

	nodes map[string]*loopNode
	bytes map[string]*atomic.Int64
	hts   []*httptest.Server
}

type loopNode struct {
	id  string
	srv *Server
	url string
	rt  *peerRT
	os  string
}

// BytesFrom reports how many request-body bytes that peer has sent.
// "console" is the orchestrator. Use it to show a copy did not pass through the GUI.
func (l *Loopback) BytesFrom(peer string) int64 {
	if c := l.bytes[peer]; c != nil {
		return c.Load()
	}
	return 0
}

// Node returns a node's server, for tests that set job hooks.
func (l *Loopback) Node(id string) *Server {
	n := l.nodes[id]
	if n == nil {
		return nil
	}
	return n.srv
}

// FailNthPatch makes peer's next outbound PATCH number n fail once (1-based).
func (l *Loopback) FailNthPatch(peer string, n int) {
	if node := l.nodes[peer]; node != nil && node.rt != nil {
		node.rt.fail = &failState{nth: n}
	}
}

func (l *Loopback) Handler() http.Handler { return l.Orch.LocalHandler() }

func (l *Loopback) Close() {
	for _, h := range l.hts {
		h.Close()
	}
}

// StartLoopback builds the harness. chunk is the resumable write size for every node.
func StartLoopback(owner string, chunk int, specs []LoopSpec) (*Loopback, error) {
	if owner == "" {
		return nil, errors.New("owner required")
	}
	if chunk <= 0 {
		chunk = 32 << 10
	}
	lb := &Loopback{
		Owner: owner,
		nodes: map[string]*loopNode{},
		bytes: map[string]*atomic.Int64{},
	}
	peers := map[string]Identity{
		"console": {Login: owner, NodeName: "console", Local: true},
	}
	for _, spec := range specs {
		login := spec.Login
		if login == "" {
			login = owner
		}
		peers[spec.ID] = Identity{Login: login, NodeName: spec.ID, NodeID: spec.ID}
	}

	var nodes []*loopNode
	for _, spec := range specs {
		srv := newServer(spec.ID, "loopback", owner, Roots{Read: spec.Read, Write: spec.Write}, chunk)
		srv.allowPeerHeader = true
		srv.peers = peers
		srv.whois = srv.loopbackWhoIs
		rt := &peerRT{base: http.DefaultTransport, peer: spec.ID, bytes: &atomic.Int64{}}
		lb.bytes[spec.ID] = rt.bytes
		osName := spec.OS
		if osName == "" {
			osName = "loopback"
		}
		node := &loopNode{id: spec.ID, srv: srv, rt: rt, os: osName}
		lb.nodes[spec.ID] = node
		nodes = append(nodes, node)
		hs := httptest.NewServer(srv.TailnetHandler())
		lb.hts = append(lb.hts, hs)
		node.url = strings.TrimRight(hs.URL, "/")
	}

	consoleBytes := &atomic.Int64{}
	lb.bytes["console"] = consoleBytes
	consoleRT := &peerRT{base: http.DefaultTransport, peer: "console", bytes: consoleBytes}

	lookup := func(base string) (*loopNode, error) {
		base = strings.TrimRight(base, "/")
		for _, n := range nodes {
			if n.url == base {
				return n, nil
			}
		}
		return nil, fmt.Errorf("unknown node url %s", base)
	}
	wire := func(rt *peerRT) func(string) (NodeAPI, error) {
		return func(base string) (NodeAPI, error) {
			n, err := lookup(base)
			if err != nil {
				return nil, err
			}
			return &httpAPI{base: n.url, http: &http.Client{Transport: rt}}, nil
		}
	}
	for _, n := range nodes {
		n.srv.remoteOverride = wire(n.rt)
	}

	var reads []string
	for _, spec := range specs {
		reads = append(reads, spec.Read...)
	}
	orch := newServer("console", "loopback", owner, Roots{Read: reads}, chunk)
	orch.guiToken = "loopback-gui-token"
	orch.remoteOverride = wire(consoleRT)
	orch.resolver = loopResolver{lb: lb, rt: consoleRT}
	lb.Orch = orch
	lb.Token = orch.guiToken
	return lb, nil
}

type loopResolver struct {
	lb *Loopback
	rt *peerRT
}

func (r loopResolver) Nodes(ctx context.Context) ([]NodeInfo, error) {
	out := make([]NodeInfo, 0, len(r.lb.nodes))
	for _, n := range r.lb.nodes {
		info := n.srv.Info()
		out = append(out, NodeInfo{
			ID: n.id, Name: n.id, Online: true, AgentOK: true,
			AgentURL: n.url, Login: r.lb.Owner, ReadOnly: info.ReadOnly,
			ReadRoots: info.ReadRoots, OS: n.os,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (r loopResolver) API(ctx context.Context, id string) (NodeAPI, error) {
	n := r.lb.nodes[id]
	if n == nil {
		return nil, statusErr(404, "unknown node %s", id)
	}
	return &httpAPI{base: n.url, http: &http.Client{Transport: r.rt}}, nil
}

func (r loopResolver) DialURL(id string) (string, error) {
	n := r.lb.nodes[id]
	if n == nil {
		return "", statusErr(404, "unknown node %s", id)
	}
	return n.url, nil
}

type peerRT struct {
	base  http.RoundTripper
	peer  string
	bytes *atomic.Int64
	fail  *failState
}

type failState struct {
	mu     sync.Mutex
	nth    int
	seen   int
	failed bool
}

func (p *peerRT) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("X-Tailfile-Loopback-Peer", p.peer)
	if r.Body != nil && r.Body != http.NoBody {
		buf, err := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if err != nil {
			return nil, err
		}
		if p.bytes != nil {
			p.bytes.Add(int64(len(buf)))
		}
		r.Body = io.NopCloser(bytes.NewReader(buf))
		r.ContentLength = int64(len(buf))
		r.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(buf)), nil
		}
	}
	resp, err := p.roundTrip(r)
	if err != nil || resp == nil || resp.Body == nil || p.bytes == nil {
		return resp, err
	}
	resp.Body = countReadCloser{ReadCloser: resp.Body, n: p.bytes}
	return resp, nil
}

func (p *peerRT) roundTrip(r *http.Request) (*http.Response, error) {
	if p.fail != nil && r.Method == http.MethodPatch {
		p.fail.mu.Lock()
		p.fail.seen++
		fail := !p.fail.failed && p.fail.nth > 0 && p.fail.seen == p.fail.nth
		if fail {
			p.fail.failed = true
		}
		p.fail.mu.Unlock()
		if fail {
			return nil, errors.New("injected transfer failure")
		}
	}
	base := p.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r)
}

type countReadCloser struct {
	io.ReadCloser
	n *atomic.Int64
}

func (c countReadCloser) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	if n > 0 {
		c.n.Add(int64(n))
	}
	return n, err
}
