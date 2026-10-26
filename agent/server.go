package agent

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
)

// Resolver finds other agents. The local orchestrator uses it to issue
// copy commands; it does not carry file bytes.
type Resolver interface {
	Nodes(ctx context.Context) ([]NodeInfo, error)
	API(ctx context.Context, id string) (NodeAPI, error)
	DialURL(id string) (string, error)
}

// Server is one node agent plus, on the localhost listener, the orchestrator.
type Server struct {
	name    string
	mode    string
	roots   Roots
	uploads *Uploads
	jobs    *Jobs

	resolver       Resolver
	outbound       *http.Client
	remoteOverride func(baseURL string) (NodeAPI, error)

	trustLocal      bool
	whois           func(*http.Request) (Identity, error)
	allowPeerHeader bool
	peers           map[string]Identity

	mu    sync.Mutex
	owner string

	cacheDir string
	selfID   string

	dropMu   sync.Mutex
	dropNode string
	dropDir  string

	trackMu sync.Mutex
	tracks  map[string]transferRec
}

type transferRec struct {
	ID       string
	Req      TransferRequest
	Runner   string
	RemoteID string
}

func newServer(name, mode, owner string, roots Roots, chunk int) *Server {
	s := &Server{
		name:     name,
		mode:     mode,
		roots:    roots,
		uploads:  NewUploads(chunk),
		owner:    owner,
		tracks:   map[string]transferRec{},
		cacheDir: os.TempDir(),
	}
	s.jobs = newJobs(s)
	return s
}

func (s *Server) openRemote(base string) (NodeAPI, error) {
	base = strings.TrimRight(base, "/")
	if s.remoteOverride != nil {
		return s.remoteOverride(base)
	}
	if base == "" {
		return nil, statusErr(400, "missing peer url")
	}
	return &httpAPI{base: base, http: s.outbound}, nil
}

func (s *Server) TailnetHandler() http.Handler {
	mux := http.NewServeMux()
	s.fileRoutes(mux)
	return s.withAuth(false, mux)
}

func (s *Server) LocalHandler() http.Handler {
	mux := http.NewServeMux()
	s.fileRoutes(mux)
	s.localRoutes(mux)
	return s.withAuth(true, mux)
}

func (s *Server) withAuth(local bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if local {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, HEAD, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Range, Upload-Offset, X-Chunk-SHA256")
			w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Range, Accept-Ranges, Content-Type")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if err := s.allowLocal(r); err != nil {
				writeErr(w, err)
				return
			}
		} else {
			if _, err := s.authenticateTailnet(r); err != nil {
				writeErr(w, err)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) allowLocal(r *http.Request) error {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return statusErr(403, "localhost only")
	}
	return nil
}

func (s *Server) authenticateTailnet(r *http.Request) (Identity, error) {
	if s.whois == nil {
		return Identity{}, statusErr(401, "not authenticated")
	}
	id, err := s.whois(r)
	if err != nil {
		return Identity{}, statusErr(401, "not authenticated: %v", err)
	}
	owner := s.ownerLogin()
	if owner == "" {
		return Identity{}, statusErr(503, "tailnet owner is not known yet")
	}
	if !strings.EqualFold(id.Login, owner) {
		log.Printf("tailfile: denied %s (%s) — owner is %s", id.Login, id.NodeName, owner)
		return Identity{}, statusErr(403, "caller %s is not the node owner", id.Login)
	}
	return id, nil
}

// loopbackWhoIs is installed only in demo mode and tests.
// Production tailnet handlers never honor X-Tailfile-Loopback-Peer.
func (s *Server) loopbackWhoIs(r *http.Request) (Identity, error) {
	if !s.allowPeerHeader {
		return Identity{}, statusErr(401, "loopback identity is disabled")
	}
	name := r.Header.Get("X-Tailfile-Loopback-Peer")
	id, ok := s.peers[name]
	if !ok || name == "" {
		return Identity{}, statusErr(401, "unknown loopback peer")
	}
	return id, nil
}
