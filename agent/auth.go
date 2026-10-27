package agent

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"tailfile.local/agent/guitoken"
)

// TailfileCap is the Tailscale app capability this agent reads from WhoIs CapMap.
// A grant value is JSON: {"read":["/path"],"write":["/path"]}.
const TailfileCap = "iori.dev/cap/tailfile"

// OriginMyGo is the Origin the native shell sends. It has no browser origin of its own.
const OriginMyGo = "mygo://tailfile"

// WailsServerOrigin is the browser origin of `go build -tags server`.
const WailsServerOrigin = "http://127.0.0.1:17431"

// GUITokenFile is the per-start bearer token, mode 0600, inside the state dir.
const GUITokenFile = guitoken.FileName

type tailfileGrant struct {
	Read  []string `json:"read"`
	Write []string `json:"write"`
}

func writeGUIToken(stateDir string) (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf[:])
	path := filepath.Join(stateDir, GUITokenFile)
	if err := writeSecret(path, []byte(token+"\n")); err != nil {
		return "", err
	}
	log.Printf("tailfile: GUI token written to %s (value not logged)", path)
	return token, nil
}

func readGUIToken(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

func writeSecret(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil {
		os.Remove(tmp)
		return werr
	}
	if cerr != nil {
		os.Remove(tmp)
		return cerr
	}
	if err := tightenSecret(tmp); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return tightenSecret(path)
}

func bearerOK(header, want string) bool {
	const prefix = "Bearer "
	if want == "" || len(header) != len(prefix)+len(want) || !strings.HasPrefix(header, prefix) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(header[len(prefix):]), []byte(want)) == 1
}

func hostIsLoopback(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// originAllowed reports whether a browser Origin may call the local API.
// An empty Origin is not a browser page; the bearer check still applies.
func originAllowed(origin string) bool {
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" && u.Scheme != "wails" && u.Scheme != "mygo" {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "mygo":
		return strings.EqualFold(u.Host, "tailfile") || u.Host == ""
	case "wails":
		return true
	case "http":
		host := strings.Trim(u.Hostname(), "[]")
		if strings.EqualFold(host, "wails.localhost") {
			return true
		}
		if host != "127.0.0.1" && !strings.EqualFold(host, "localhost") && host != "::1" {
			return false
		}
		return u.Port() == "17431"
	default:
		return false
	}
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
	if !hostIsLoopback(r.Host) {
		return statusErr(403, "host is not loopback")
	}
	origin := r.Header.Get("Origin")
	if origin != "" && !originAllowed(origin) {
		return statusErr(403, "origin is not a tailfile gui")
	}
	if r.Method == http.MethodOptions {
		return nil
	}
	if !bearerOK(r.Header.Get("Authorization"), s.guiToken) {
		return statusErr(401, "gui token required")
	}
	return nil
}

func setLocalCORS(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if originAllowed(origin) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
	}
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Range, Upload-Offset, X-Chunk-SHA256")
	w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Range, Accept-Ranges, Content-Type")
}

func parseGrants(raw [][]byte) ([]tailfileGrant, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]tailfileGrant, 0, len(raw))
	for _, b := range raw {
		if len(strings.TrimSpace(string(b))) == 0 {
			continue
		}
		var g tailfileGrant
		if err := json.Unmarshal(b, &g); err != nil {
			return nil, statusErr(400, "tailfile grant is not valid json")
		}
		out = append(out, g)
	}
	return out, nil
}

// narrowToGrant intersects a node's configured roots with grant paths.
// A grant path outside every configured root is dropped. A grant wider than
// a configured root keeps the configured root.
func narrowToGrant(node Roots, grants []tailfileGrant) Roots {
	var read, write []string
	for _, g := range grants {
		read = append(read, g.Read...)
		write = append(write, g.Write...)
	}
	return Roots{Read: intersectRoots(node.Read, read), Write: intersectRoots(node.Write, write)}
}

func intersectRoots(nodeRoots, grantRoots []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, g := range grantRoots {
		g = filepath.Clean(g)
		for _, n := range nodeRoots {
			n = filepath.Clean(n)
			var keep string
			switch {
			case within(n, g):
				keep = g
			case within(g, n):
				keep = n
			default:
				continue
			}
			if !seen[keep] {
				seen[keep] = true
				out = append(out, keep)
			}
		}
	}
	return out
}

// ResolveOwner decides which tailnet login may call this node.
// A tagged node (or the tagged-devices user) has no human owner; starting
// without -owner would lock the node to a login nobody has.
func ResolveOwner(login string, tagged bool, configured string) (string, error) {
	if strings.TrimSpace(configured) != "" {
		return configured, nil
	}
	if tagged || strings.EqualFold(strings.TrimSpace(login), "tagged-devices") {
		shown := login
		if shown == "" {
			shown = "tagged-devices"
		}
		return "", statusErr(1, "this node is tagged (tailnet user %q) and has no human owner; set -owner or TAILFILE_OWNER to the login allowed to call it", shown)
	}
	return login, nil
}
