package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Hooks let tests observe or interrupt a copy. Leave the zero value in production.
type Hooks struct {
	// AfterChunk runs on the job goroutine after each successful chunk.
	AfterChunk func(chunks int)
	// Retries is how many extra attempts a failed chunk gets. Zero means a single attempt.
	Retries int
}

// Jobs runs direct copies on this node.
type Jobs struct {
	s     *Server
	hooks Hooks

	mu  sync.Mutex
	all map[string]*runningJob
}

type runningJob struct {
	mu       sync.Mutex
	job      Job
	cancel   context.CancelFunc
	done     map[string]struct{}
	baseDone int64
}

type copyItem struct {
	Rel    string
	IsDir  bool
	Size   int64
	Local  string
	Remote string
	Dest   string
}

func newJobs(s *Server) *Jobs {
	return &Jobs{s: s, all: map[string]*runningJob{}}
}

func (j *Jobs) list() []Job {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]Job, 0, len(j.all))
	for _, r := range j.all {
		out = append(out, r.snapshot())
	}
	return out
}

func (j *Jobs) get(id string) (Job, error) {
	run := j.getRun(id)
	if run == nil {
		return Job{}, statusErr(404, "job not found")
	}
	return run.snapshot(), nil
}

func (j *Jobs) getRun(id string) *runningJob {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.all[id]
}

func (r *runningJob) snapshot() Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := r.job
	if len(r.job.DonePaths) > 0 {
		cp.DonePaths = append([]string(nil), r.job.DonePaths...)
	}
	if len(r.job.Sources) > 0 {
		cp.Sources = append([]string(nil), r.job.Sources...)
	}
	return cp
}

func (r *runningJob) update(fn func(*Job)) {
	r.mu.Lock()
	fn(&r.job)
	r.job.Updated = time.Now().UTC()
	r.mu.Unlock()
}

func (j *Jobs) start(req JobRequest) (Job, error) {
	req.Mode = strings.ToLower(strings.TrimSpace(req.Mode))
	switch req.Mode {
	case "push":
		if req.DestBaseURL == "" {
			return Job{}, statusErr(400, "push requires destBaseURL")
		}
	case "pull":
		if req.SrcBaseURL == "" {
			return Job{}, statusErr(400, "pull requires srcBaseURL")
		}
	default:
		return Job{}, statusErr(400, "mode must be push or pull")
	}
	if req.SrcPath == "" || req.DestPath == "" {
		return Job{}, statusErr(400, "srcPath and destPath are required")
	}
	return j.launch(req, nil)
}

func (j *Jobs) startImport(destBase, destDir string, locals []string) (Job, error) {
	if destBase == "" || destDir == "" || len(locals) == 0 {
		return Job{}, statusErr(400, "import needs a destination and at least one path")
	}
	items, err := collectImport(destDir, locals)
	if err != nil {
		return Job{}, err
	}
	if len(items) == 0 {
		return Job{}, statusErr(400, "nothing to import")
	}
	req := JobRequest{
		Mode:        "import",
		SrcPath:     locals[0],
		DestPath:    destDir,
		DestBaseURL: destBase,
		Sources:     append([]string(nil), locals...),
	}
	return j.launch(req, items)
}

func (j *Jobs) launch(req JobRequest, preset []copyItem) (Job, error) {
	id, err := randID()
	if err != nil {
		return Job{}, err
	}
	now := time.Now().UTC()
	ctx, cancel := context.WithCancel(context.Background())
	run := &runningJob{
		job: Job{
			ID: id, Mode: req.Mode, Status: jobRunning,
			SrcPath: req.SrcPath, DestPath: req.DestPath,
			SrcBaseURL: req.SrcBaseURL, DestBaseURL: req.DestBaseURL,
			Sources: append([]string(nil), req.Sources...),
			Created: now, Updated: now,
		},
		cancel: cancel,
		done:   map[string]struct{}{},
	}
	j.mu.Lock()
	j.all[id] = run
	j.mu.Unlock()
	go j.execute(ctx, run, preset)
	return run.snapshot(), nil
}

func (j *Jobs) cancel(id string) (Job, error) {
	run := j.getRun(id)
	if run == nil {
		return Job{}, statusErr(404, "job not found")
	}
	run.cancel()
	run.update(func(job *Job) {
		if job.Status == jobRunning || job.Status == jobQueued {
			job.Status = jobCanceled
			job.Error = "canceled"
		}
	})
	return run.snapshot(), nil
}

func (j *Jobs) resume(id string) (Job, error) {
	run := j.getRun(id)
	if run == nil {
		return Job{}, statusErr(404, "job not found")
	}
	snap := run.snapshot()
	switch snap.Status {
	case jobRunning, jobQueued:
		return Job{}, statusErr(409, "job is still running")
	case jobCompleted:
		return snap, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	run.mu.Lock()
	run.cancel = cancel
	run.job.Status = jobRunning
	run.job.Error = ""
	run.job.Updated = time.Now().UTC()
	run.done = map[string]struct{}{}
	for _, p := range run.job.DonePaths {
		run.done[p] = struct{}{}
	}
	run.mu.Unlock()
	go j.execute(ctx, run, nil)
	return run.snapshot(), nil
}

func (j *Jobs) execute(ctx context.Context, run *runningJob, preset []copyItem) {
	err := j.copyAll(ctx, run, preset)
	if err == nil {
		err = ctx.Err()
	}
	run.update(func(job *Job) {
		switch {
		case errors.Is(err, context.Canceled):
			job.Status = jobCanceled
			if job.Error == "" {
				job.Error = "canceled"
			}
		case err != nil:
			job.Status = jobError
			job.Error = err.Error()
		default:
			job.Status = jobCompleted
			job.Error = ""
			if job.BytesDone < job.BytesTotal {
				job.BytesDone = job.BytesTotal
			}
		}
	})
}

func (j *Jobs) copyAll(ctx context.Context, run *runningJob, preset []copyItem) error {
	snap := run.snapshot()
	items, err := j.plan(ctx, snap, preset)
	if err != nil {
		return err
	}
	var total int64
	files := 0
	for _, it := range items {
		if !it.IsDir {
			total += it.Size
			files++
		}
	}
	run.mu.Lock()
	var base int64
	for _, it := range items {
		if it.IsDir {
			continue
		}
		if _, ok := run.done[it.Rel]; ok {
			base += it.Size
		}
	}
	run.baseDone = base
	run.job.BytesTotal = total
	run.job.FilesTotal = files
	run.job.BytesDone = base
	run.job.FilesDone = countFilesDone(run.done, items)
	run.job.Updated = time.Now().UTC()
	run.mu.Unlock()

	dest, src, err := j.endpoints(snap)
	if err != nil {
		return err
	}
	chunk := defaultChunkSize
	if j.s.uploads != nil && j.s.uploads.ChunkSize > 0 {
		chunk = j.s.uploads.ChunkSize
	}
	chunkN := 0
	for _, it := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !it.IsDir {
			run.mu.Lock()
			_, already := run.done[it.Rel]
			run.mu.Unlock()
			if already {
				continue
			}
		}
		run.update(func(job *Job) { job.Current = it.Dest })
		if it.IsDir {
			if err := dest.Mkdir(ctx, it.Dest); err != nil {
				return err
			}
			continue
		}
		up, err := dest.StartUpload(ctx, it.Dest, it.Size, "")
		if err != nil {
			return err
		}
		run.update(func(job *Job) { job.CurrentUpload = up.ID })
		var reader io.ReadCloser
		if snap.Mode == "pull" {
			reader, _, err = src.Open(ctx, it.Remote, up.Offset, -1)
		} else {
			reader, err = openLocal(it.Local, up.Offset)
		}
		if err != nil {
			return err
		}
		err = j.writeChunks(ctx, run, dest, up, reader, chunk, func() {
			chunkN++
			if j.hooks.AfterChunk != nil {
				j.hooks.AfterChunk(chunkN)
			}
		})
		reader.Close()
		if err != nil {
			return err
		}
		if err := dest.FinishUpload(ctx, up.ID); err != nil {
			return err
		}
		run.mu.Lock()
		run.baseDone += it.Size
		if _, ok := run.done[it.Rel]; !ok {
			run.done[it.Rel] = struct{}{}
			run.job.DonePaths = append(run.job.DonePaths, it.Rel)
		}
		run.job.FilesDone = countFilesDone(run.done, items)
		run.job.BytesDone = run.baseDone
		run.job.CurrentUpload = ""
		run.job.Updated = time.Now().UTC()
		run.mu.Unlock()
	}
	return nil
}

func countFilesDone(done map[string]struct{}, items []copyItem) int {
	n := 0
	for _, it := range items {
		if it.IsDir {
			continue
		}
		if _, ok := done[it.Rel]; ok {
			n++
		}
	}
	return n
}

func (j *Jobs) plan(ctx context.Context, snap Job, preset []copyItem) ([]copyItem, error) {
	if preset != nil {
		return preset, nil
	}
	switch snap.Mode {
	case "push":
		return collectLocal(j.s.roots, snap.SrcPath, snap.DestPath)
	case "pull":
		src, err := j.s.openRemote(snap.SrcBaseURL)
		if err != nil {
			return nil, err
		}
		return collectRemote(ctx, src, snap.SrcPath, snap.DestPath)
	case "import":
		sources := snap.Sources
		if len(sources) == 0 && snap.SrcPath != "" {
			sources = []string{snap.SrcPath}
		}
		return collectImport(snap.DestPath, sources)
	default:
		return nil, statusErr(400, "unknown mode")
	}
}

func (j *Jobs) endpoints(snap Job) (dest, src NodeAPI, err error) {
	switch snap.Mode {
	case "push", "import":
		dest, err = j.s.openRemote(snap.DestBaseURL)
		return dest, nil, err
	case "pull":
		src, err = j.s.openRemote(snap.SrcBaseURL)
		if err != nil {
			return nil, nil, err
		}
		return directAPI{s: j.s}, src, nil
	default:
		return nil, nil, statusErr(400, "unknown mode")
	}
}

func (j *Jobs) writeChunks(ctx context.Context, run *runningJob, dest NodeAPI, up Upload, r io.Reader, chunk int, after func()) error {
	buf := make([]byte, chunk)
	offset := up.Offset
	run.mu.Lock()
	run.job.BytesDone = run.baseDone + offset
	run.job.Updated = time.Now().UTC()
	run.mu.Unlock()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, rerr := io.ReadFull(r, buf)
		if n > 0 {
			sum := sha256.Sum256(buf[:n])
			var putErr error
			for try := 0; try <= j.hooks.Retries; try++ {
				_, putErr = dest.PutChunk(ctx, up.ID, offset, buf[:n], hex.EncodeToString(sum[:]))
				if putErr == nil || errors.Is(putErr, context.Canceled) {
					break
				}
			}
			if putErr != nil {
				return putErr
			}
			offset += int64(n)
			run.mu.Lock()
			run.job.BytesDone = run.baseDone + offset
			run.job.Updated = time.Now().UTC()
			run.mu.Unlock()
			if after != nil {
				after()
			}
		}
		if errors.Is(rerr, io.EOF) || errors.Is(rerr, io.ErrUnexpectedEOF) {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}

func openLocal(path string, offset int64) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			f.Close()
			return nil, err
		}
	}
	return f, nil
}

func collectLocal(roots Roots, src, dest string) ([]copyItem, error) {
	clean, err := roots.Resolve(src, false)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(clean)
	if err != nil {
		return nil, err
	}
	walkRoot := clean
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		if !roots.canRead(resolved) {
			return nil, errForbidden
		}
		walkRoot = resolved
	}
	if !info.IsDir() {
		if !info.Mode().IsRegular() {
			return nil, errNotFile
		}
		return []copyItem{{
			Rel: BaseRemote(dest), Size: info.Size(), Local: walkRoot, Dest: dest,
		}}, nil
	}
	items := []copyItem{{Rel: ".", IsDir: true, Dest: dest}}
	err = filepath.WalkDir(walkRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == walkRoot {
			return nil
		}
		rel, err := filepath.Rel(walkRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		target := JoinRemote(dest, rel)
		if d.IsDir() {
			items = append(items, copyItem{Rel: rel, IsDir: true, Dest: target})
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		st, err := d.Info()
		if err != nil {
			return err
		}
		items = append(items, copyItem{Rel: rel, Size: st.Size(), Local: path, Dest: target})
		return nil
	})
	return items, err
}

func collectRemote(ctx context.Context, api NodeAPI, src, dest string) ([]copyItem, error) {
	st, err := api.Stat(ctx, src)
	if err != nil {
		return nil, err
	}
	if !st.IsDir {
		return []copyItem{{
			Rel: BaseRemote(dest), Size: st.Size, Remote: st.Path, Dest: dest,
		}}, nil
	}
	var items []copyItem
	var walk func(srcPath, destPath, rel string) error
	walk = func(srcPath, destPath, rel string) error {
		key := rel
		if key == "" {
			key = "."
		}
		items = append(items, copyItem{Rel: key, IsDir: true, Dest: destPath})
		listing, err := api.List(ctx, srcPath)
		if err != nil {
			return err
		}
		for _, e := range listing.Entries {
			childRel := e.Name
			if rel != "" {
				childRel = rel + "/" + e.Name
			}
			childDest := JoinRemote(destPath, e.Name)
			if e.IsDir {
				if err := walk(e.Path, childDest, childRel); err != nil {
					return err
				}
				continue
			}
			if e.IsSymlink || e.Size < 0 {
				continue
			}
			items = append(items, copyItem{Rel: childRel, Size: e.Size, Remote: e.Path, Dest: childDest})
		}
		return nil
	}
	if err := walk(st.Path, dest, ""); err != nil {
		return nil, err
	}
	return items, nil
}

func collectImport(destDir string, locals []string) ([]copyItem, error) {
	var items []copyItem
	for _, local := range locals {
		abs, err := filepath.Abs(local)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			if !info.Mode().IsRegular() {
				continue
			}
			items = append(items, copyItem{
				Rel: filepath.Base(abs), Size: info.Size(), Local: abs,
				Dest: JoinRemote(destDir, filepath.Base(abs)),
			})
			continue
		}
		baseParent := filepath.Dir(abs)
		err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(baseParent, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			target := JoinRemote(destDir, rel)
			if d.IsDir() {
				items = append(items, copyItem{Rel: rel, IsDir: true, Dest: target})
				return nil
			}
			if !d.Type().IsRegular() {
				return nil
			}
			st, err := d.Info()
			if err != nil {
				return err
			}
			items = append(items, copyItem{Rel: rel, Size: st.Size(), Local: path, Dest: target})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}
