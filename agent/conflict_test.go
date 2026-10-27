package agent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExistingFilePromptsAndOverwriteIsAtomic(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	src := filepath.Join(a, "note.txt")
	dst := filepath.Join(b, "note.txt")
	old := []byte("old-contents")
	payload := bytes.Repeat([]byte("N"), 2500)
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, old, 0o644); err != nil {
		t.Fatal(err)
	}
	lb := newPair(t, a, b, 1000)
	lb.lb.Node("alpha").jobs.hooks.AfterChunk = func(int) {
		got, err := os.ReadFile(dst)
		if err != nil {
			t.Errorf("dest during copy: %v", err)
			return
		}
		if !bytes.Equal(got, old) {
			t.Errorf("dest changed before finish: %q", got)
		}
	}
	tr := mustTransfer(t, lb, TransferRequest{
		Mode: "push", FromNode: "alpha", FromPath: src, ToNode: "beta", ToPath: dst,
	})
	paused := waitTransfer(t, lb.client, tr.ID, jobConflict)
	if paused.Job.Conflict == nil || paused.Job.Conflict.Path != dst {
		t.Fatalf("conflict %+v", paused.Job.Conflict)
	}
	still, err := os.ReadFile(dst)
	if err != nil || !bytes.Equal(still, old) {
		t.Fatalf("dest while paused %q %v", still, err)
	}
	if _, err := lb.client.AnswerTransferConflict(context.Background(), tr.ID, conflictOverwrite, false); err != nil {
		t.Fatal(err)
	}
	waitTransfer(t, lb.client, tr.ID, jobCompleted)
	got, err := os.ReadFile(dst)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("overwritten %q %v", got, err)
	}
	if _, err := os.Stat(dst + ".tailfile-partial"); !os.IsNotExist(err) {
		t.Fatalf("partial left behind: %v", err)
	}
}

func TestExistingFileSkipLeavesIt(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	src := filepath.Join(a, "note.txt")
	dst := filepath.Join(b, "note.txt")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	lb := newPair(t, a, b, 1000)
	tr := mustTransfer(t, lb, TransferRequest{
		Mode: "push", FromNode: "alpha", FromPath: src, ToNode: "beta", ToPath: dst,
	})
	waitTransfer(t, lb.client, tr.ID, jobConflict)
	if _, err := lb.client.AnswerTransferConflict(context.Background(), tr.ID, conflictSkip, false); err != nil {
		t.Fatal(err)
	}
	waitTransfer(t, lb.client, tr.ID, jobCompleted)
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "keep" {
		t.Fatalf("skip wrote %q %v", got, err)
	}
}

func TestConflictApplyToRestSkipsTheNextFile(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(a, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(b, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(a, "dir", name), []byte("new-"+name), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(b, "dir", name), []byte("old-"+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lb := newPair(t, a, b, 1000)
	tr := mustTransfer(t, lb, TransferRequest{
		Mode: "push", FromNode: "alpha", FromPath: filepath.Join(a, "dir"), ToNode: "beta", ToPath: filepath.Join(b, "dir"),
	})
	paused := waitTransfer(t, lb.client, tr.ID, jobConflict)
	if paused.Job.Conflict == nil {
		t.Fatal("expected a conflict")
	}
	if _, err := lb.client.AnswerTransferConflict(context.Background(), tr.ID, conflictSkip, true); err != nil {
		t.Fatal(err)
	}
	done := waitTransfer(t, lb.client, tr.ID, jobCompleted)
	if done.Job.ConflictPolicy != conflictSkip {
		t.Fatalf("policy %q", done.Job.ConflictPolicy)
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		got, err := os.ReadFile(filepath.Join(b, "dir", name))
		if err != nil || string(got) != "old-"+name {
			t.Fatalf("%s = %q %v", name, got, err)
		}
	}
}

func TestPartialSameSourceResumesWithoutPrompt(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.bin")
	payload := []byte("abcdefghij")
	if err := os.WriteFile(dest, []byte("already"), 0o644); err != nil {
		t.Fatal(err)
	}
	roots := Roots{Read: []string{dir}, Write: []string{dir}}
	u := NewUploads(4)
	mtime := time.Now().UnixNano()
	// A previous overwrite got as far as a partial and then stopped.
	// Drop the completed file's claim by starting with overwrite, writing
	// one chunk, and leaving the destination in place.
	up, err := u.Start(roots, dest, int64(len(payload)), mtime, "", true)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256Hex(payload[:4])
	if _, err := u.Put(up.ID, 0, payload[:4], sum); err != nil {
		t.Fatal(err)
	}
	// Forget the in-memory session the way a restart would, but keep the meta.
	u.mu.Lock()
	delete(u.items, up.ID)
	u.mu.Unlock()
	still, err := os.ReadFile(dest)
	if err != nil || string(still) != "already" {
		t.Fatalf("dest %q %v", still, err)
	}

	resumed, err := u.Start(roots, dest, int64(len(payload)), mtime, "", false)
	if err != nil {
		t.Fatalf("matching partial should resume without overwrite: %v", err)
	}
	if resumed.Offset != 4 || resumed.ID != up.ID {
		t.Fatalf("resume %+v", resumed)
	}
	if got, err := os.ReadFile(dest); err != nil || string(got) != "already" {
		t.Fatalf("resume touched dest %q %v", got, err)
	}
}

func TestOverwriteRenamesOnlyAfterHash(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.bin")
	old := []byte("OLD")
	payload := []byte("abcdef")
	if err := os.WriteFile(dest, old, 0o644); err != nil {
		t.Fatal(err)
	}
	roots := Roots{Read: []string{dir}, Write: []string{dir}}
	u := NewUploads(1024)
	if _, err := u.Start(roots, dest, int64(len(payload)), 1, "", false); err == nil || !isDestExists(err) {
		t.Fatalf("expected exists, got %v", err)
	}
	if _, err := os.Stat(dest + ".tailfile-partial"); !os.IsNotExist(err) {
		t.Fatal("refused start created a partial")
	}
	up, err := u.Start(roots, dest, int64(len(payload)), 1, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(dest); err != nil || !bytes.Equal(got, old) {
		t.Fatalf("start overwrite changed dest %q %v", got, err)
	}
	sum := sha256Hex(payload)
	if _, err := u.Put(up.ID, 0, payload, sum); err != nil {
		t.Fatal(err)
	}
	if _, err := u.Finish(up.ID, strings.Repeat("ab", 32)); err == nil {
		t.Fatal("bad hash should fail")
	}
	if got, err := os.ReadFile(dest); err != nil || !bytes.Equal(got, old) {
		t.Fatalf("bad hash replaced dest %q %v", got, err)
	}
	if _, err := os.Stat(dest + ".tailfile-partial"); !os.IsNotExist(err) {
		t.Fatalf("partial left behind: %v", err)
	}
	up, err = u.Start(roots, dest, int64(len(payload)), 1, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := u.Put(up.ID, 0, payload, sum); err != nil {
		t.Fatal(err)
	}
	if _, err := u.Finish(up.ID, sum); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(dest); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("finish %q %v", got, err)
	}
}

func TestConflictSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	state := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	dst := filepath.Join(dir, "dst.bin")
	if err := os.WriteFile(src, []byte("new-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newServer("n", "test", "ada@example.com", Roots{Read: []string{dir}, Write: []string{dir}}, 1024)
	if err := s.attachState(state); err != nil {
		t.Fatal(err)
	}
	s.remoteOverride = func(string) (NodeAPI, error) { return directAPI{s: s}, nil }
	job, err := s.jobs.start(context.Background(), JobRequest{
		Mode: "push", SrcPath: src, DestPath: dst, DestBaseURL: "http://self",
	})
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, s, job.ID, jobConflict)
	raw, err := os.ReadFile(filepath.Join(state, "jobs.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"status": "conflict"`)) {
		t.Fatalf("jobs.json did not persist conflict: %s", raw)
	}
	snap := append([]byte(nil), raw...)

	s2 := newServer("n", "test", "ada@example.com", Roots{Read: []string{dir}, Write: []string{dir}}, 1024)
	state2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(state2, "jobs.json"), snap, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s2.attachState(state2); err != nil {
		t.Fatal(err)
	}
	s2.remoteOverride = func(string) (NodeAPI, error) { return directAPI{s: s2}, nil }
	loaded, err := s2.jobs.get(job.ID)
	if err != nil || loaded.Status != jobConflict || loaded.Conflict == nil || loaded.Conflict.Path != dst {
		t.Fatalf("reloaded %+v %v", loaded, err)
	}
	if _, err := s2.jobs.answer(job.ID, conflictOverwrite, false); err != nil {
		t.Fatal(err)
	}
	waitJob(t, s2, job.ID, jobCompleted)
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "new-bytes" {
		t.Fatalf("after restart %q %v", got, err)
	}
	// The original job is still paused on the first server. Cancel it and wait
	// until that goroutine has finished saving, so it does not write into the
	// temp dir after the test returns.
	if _, err := s.jobs.cancel(job.ID); err != nil {
		t.Fatal(err)
	}
	waitNotExecuting(t, s, job.ID)
}

func waitNotExecuting(t *testing.T, s *Server, id string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		run := s.jobs.getRun(id)
		if run == nil {
			return
		}
		run.mu.Lock()
		running := run.executing
		run.mu.Unlock()
		if !running {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job goroutine still running")
}

func TestAnswerWithoutChoiceDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	s := newServer("n", "test", "ada@example.com", Roots{Read: []string{dir}, Write: []string{dir}}, 64)
	if _, err := s.jobs.answer("missing", conflictOverwrite, false); err == nil {
		t.Fatal("missing job should fail")
	}
	src := filepath.Join(dir, "a.txt")
	dst := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.remoteOverride = func(string) (NodeAPI, error) { return directAPI{s: s}, nil }
	job, err := s.jobs.start(context.Background(), JobRequest{
		Mode: "push", SrcPath: src, DestPath: dst, DestBaseURL: "http://self",
	})
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, s, job.ID, jobConflict)
	if _, err := s.jobs.answer(job.ID, "maybe", false); err == nil {
		t.Fatal("bad action should fail")
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "y" {
		t.Fatalf("dest %q %v", got, err)
	}
	if _, err := s.jobs.cancel(job.ID); err != nil {
		t.Fatal(err)
	}
	waitNotExecuting(t, s, job.ID)
}
