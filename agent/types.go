package agent

import "time"

// Identity is the tailnet caller, from tsnet WhoIs or the loopback harness.
type Identity struct {
	Login    string `json:"login"`
	UserID   string `json:"userId,omitempty"`
	NodeName string `json:"nodeName,omitempty"`
	NodeID   string `json:"nodeId,omitempty"`
	Local    bool   `json:"local,omitempty"`

	// GrantPresent is true when WhoIs CapMap contains TailfileCap.
	// GrantRaw is the raw JSON values of that capability. Both are
	// filled by the tailnet whois callback and ignored on the wire.
	GrantPresent bool     `json:"-"`
	GrantRaw     [][]byte `json:"-"`
}

// Entry is one directory listing row.
type Entry struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	IsDir      bool      `json:"isDir"`
	IsSymlink  bool      `json:"isSymlink,omitempty"`
	LinkTarget string    `json:"linkTarget,omitempty"`
	Size       int64     `json:"size"`
	ModTime    time.Time `json:"modTime"`
	Mode       string    `json:"mode,omitempty"`
}

// Listing is a directory listing.
type Listing struct {
	Path    string  `json:"path"`
	Entries []Entry `json:"entries"`
}

// Info describes a node agent.
type Info struct {
	Name       string   `json:"name"`
	Version    string   `json:"version"`
	Owner      string   `json:"owner,omitempty"`
	ReadOnly   bool     `json:"readOnly"`
	ReadRoots  []string `json:"readRoots"`
	WriteRoots []string `json:"writeRoots"`
	Mode       string   `json:"mode"`
}

// NodeInfo is a tailnet node as the local orchestrator sees it.
type NodeInfo struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	OS        string   `json:"os,omitempty"`
	Online    bool     `json:"online"`
	AgentOK   bool     `json:"agentOK"`
	AgentURL  string   `json:"agentURL,omitempty"`
	Login     string   `json:"login,omitempty"`
	Self      bool     `json:"self"`
	ReadOnly  bool     `json:"readOnly"`
	ReadRoots []string `json:"readRoots,omitempty"`
	Addrs     []string `json:"addrs,omitempty"`
}

// Upload is a resumable write session.
type Upload struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	Offset    int64  `json:"offset"`
	ChunkSize int    `json:"chunkSize"`
	Complete  bool   `json:"complete"`
	MtimeUnix int64  `json:"mtimeUnix,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
}

// Job is a direct node-to-node copy (or a localhost import).
type Job struct {
	ID            string        `json:"id"`
	Mode          string        `json:"mode"` // push, pull, import
	Status        string        `json:"status"`
	SrcPath       string        `json:"srcPath,omitempty"`
	Sources       []string      `json:"sources,omitempty"`
	DestPath      string        `json:"destPath"`
	SrcBaseURL    string        `json:"srcBaseURL,omitempty"`
	DestBaseURL   string        `json:"destBaseURL,omitempty"`
	BytesTotal    int64         `json:"bytesTotal"`
	BytesDone     int64         `json:"bytesDone"`
	FilesTotal    int           `json:"filesTotal"`
	FilesDone     int           `json:"filesDone"`
	Current       string        `json:"current,omitempty"`
	CurrentUpload string        `json:"currentUpload,omitempty"`
	DonePaths     []string      `json:"donePaths,omitempty"`
	Stamps        []SourceStamp `json:"stamps,omitempty"`
	UseRoots      bool          `json:"useRoots,omitempty"`
	ReadRoots     []string      `json:"readRoots,omitempty"`
	WriteRoots    []string      `json:"writeRoots,omitempty"`
	Error         string        `json:"error,omitempty"`
	Created       time.Time     `json:"created"`
	Updated       time.Time     `json:"updated"`
}

// SourceStamp is the source file identity stored with a job and in the
// partial's meta. A resume that sees a different size or mtime starts over.
type SourceStamp struct {
	Rel       string `json:"rel"`
	Size      int64  `json:"size"`
	MtimeUnix int64  `json:"mtimeUnix"`
}

const (
	jobQueued      = "queued"
	jobRunning     = "running"
	jobCompleted   = "completed"
	jobCanceled    = "canceled"
	jobError       = "error"
	jobInterrupted = "interrupted"
)

// JobRequest starts a copy on the node that receives it.
// Push reads SrcPath locally and writes to DestBaseURL + DestPath.
// Pull reads SrcBaseURL + SrcPath and writes DestPath locally.
type JobRequest struct {
	Mode        string   `json:"mode"`
	SrcPath     string   `json:"srcPath"`
	DestPath    string   `json:"destPath"`
	SrcBaseURL  string   `json:"srcBaseURL,omitempty"`
	DestBaseURL string   `json:"destBaseURL,omitempty"`
	Sources     []string `json:"sources,omitempty"`
}

// Transfer is a copy the local orchestrator asked some node to run.
// The bytes move between From and To; this record is only the command and progress.
type Transfer struct {
	ID         string `json:"id"`
	Mode       string `json:"mode"`
	FromNode   string `json:"fromNode"`
	FromPath   string `json:"fromPath"`
	ToNode     string `json:"toNode"`
	ToPath     string `json:"toPath"`
	RunnerNode string `json:"runnerNode"`
	Job        Job    `json:"job"`
}

// TransferRequest is what the GUI sends to the local agent.
type TransferRequest struct {
	Mode     string `json:"mode"` // push or pull
	FromNode string `json:"fromNode"`
	FromPath string `json:"fromPath"`
	ToNode   string `json:"toNode"`
	ToPath   string `json:"toPath"`
}

// ImportRequest copies local OS paths into a folder on a node.
// It is accepted only on the localhost listener.
type ImportRequest struct {
	Node    string   `json:"node"`
	DestDir string   `json:"destDir"`
	Paths   []string `json:"paths"`
}

// CacheRequest downloads a remote file into the local drag-out cache.
type CacheRequest struct {
	Node string `json:"node"`
	Path string `json:"path"`
}

// CacheResult is the local filesystem path a drag gesture can offer.
type CacheResult struct {
	LocalPath string `json:"localPath"`
	Cached    bool   `json:"cached"`
}
