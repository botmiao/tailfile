package agent

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
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

	// guiToken is the per-start bearer for the localhost listener.
	// It is never logged. An empty token fails closed.
	guiToken string
	stateDir string

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

func (s *Server) attachState(dir string) error {
	if dir == "" {
		return nil
	}
	s.stateDir = dir
	s.jobs.statePath = filepath.Join(dir, "jobs.json")
	if err := s.jobs.load(); err != nil {
		return err
	}
	return s.loadTracks()
}

func (s *Server) saveTracks() {
	if s.stateDir == "" {
		return
	}
	s.trackMu.Lock()
	recs := make([]transferRec, 0, len(s.tracks))
	for _, rec := range s.tracks {
		recs = append(recs, rec)
	}
	s.trackMu.Unlock()
	raw, err := json.MarshalIndent(struct {
		Transfers []transferRec `json:"transfers"`
	}{Transfers: recs}, "", "  ")
	if err != nil {
		log.Printf("tailfile: persist transfers: %v", err)
		return
	}
	if err := writeSecret(filepath.Join(s.stateDir, "transfers.json"), raw); err != nil {
		log.Printf("tailfile: persist transfers: %v", err)
	}
}

func (s *Server) loadTracks() error {
	if s.stateDir == "" {
		return nil
	}
	raw, err := os.ReadFile(filepath.Join(s.stateDir, "transfers.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var disk struct {
		Transfers []transferRec `json:"transfers"`
	}
	if err := json.Unmarshal(raw, &disk); err != nil {
		return err
	}
	s.trackMu.Lock()
	defer s.trackMu.Unlock()
	for _, rec := range disk.Transfers {
		if rec.ID == "" {
			continue
		}
		s.tracks[rec.ID] = rec
	}
	return nil
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
			setLocalCORS(w, r)
			if err := s.allowLocal(r); err != nil {
				writeErr(w, err)
				return
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
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

type rootCtxKey struct{}

func withRoots(ctx context.Context, roots Roots) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, rootCtxKey{}, roots)
}

func (s *Server) rootsFor(ctx context.Context) Roots {
	if ctx != nil {
		if roots, ok := ctx.Value(rootCtxKey{}).(Roots); ok {
			return roots
		}
	}
	return s.roots
}

func (s *Server) authenticateTailnet(r *http.Request) (Identity, error) {
	if s.whois == nil {
		return Identity{}, statusErr(401, "not authenticated")
	}
	id, err := s.whois(r)
	if err != nil {
		return Identity{}, statusErr(401, "not authenticated: %v", err)
	}
	if id.GrantPresent {
		grants, err := parseGrants(id.GrantRaw)
		if err != nil {
			return Identity{}, err
		}
		narrowed := narrowToGrant(s.roots, grants)
		*r = *r.WithContext(withRoots(r.Context(), narrowed))
		return id, nil
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
