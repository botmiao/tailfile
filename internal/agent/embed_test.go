package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStartEmbeddedDemoAndNoTokenFile(t *testing.T) {
	state := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn, err := StartEmbedded(ctx, Config{Demo: true, StateDir: state})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if conn.Mode != ConnDemo {
		t.Fatalf("mode = %v, want %v", conn.Mode, ConnDemo)
	}
	if _, err := os.Stat(filepath.Join(state, "gui-token")); !os.IsNotExist(err) {
		t.Fatalf("embedded mode must not write gui-token, stat err = %v", err)
	}

	if err := conn.Client.Health(ctx); err != nil {
		t.Fatalf("health: %v", err)
	}
	nodes, err := conn.Client.Nodes(ctx)
	if err != nil {
		t.Fatalf("nodes: %v", err)
	}
	if len(nodes) != 5 {
		t.Fatalf("got %d nodes, want 5: %+v", len(nodes), nodes)
	}
	byID := map[string]NodeInfo{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	for _, id := range []string{"alpha", "beta", "gamma", "pixel", "phone"} {
		if _, ok := byID[id]; !ok {
			t.Fatalf("missing demo node %q", id)
		}
	}

	alphaRoot := byID["alpha"].ReadRoots[0]
	betaRoot := byID["beta"].ReadRoots[0]

	listing, err := conn.Client.List(ctx, "alpha", alphaRoot)
	if err != nil {
		t.Fatalf("list alpha: %v", err)
	}
	if len(listing.Entries) == 0 {
		t.Fatal("expected seeded entries on alpha")
	}

	helloPath := filepath.Join(alphaRoot, "hello.txt")
	prev, err := conn.Client.Preview(ctx, "alpha", helloPath)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if !strings.Contains(prev.Text, "Hello from alpha") {
		t.Fatalf("unexpected preview: %+v", prev)
	}

	newDir := filepath.Join(alphaRoot, "新建目录")
	renamedDir := filepath.Join(alphaRoot, "已重命名")
	if err := conn.Client.Mkdir(ctx, "alpha", newDir); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := conn.Client.Rename(ctx, "alpha", newDir, renamedDir); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if err := conn.Client.Delete(ctx, "alpha", renamedDir); err != nil {
		t.Fatalf("delete: %v", err)
	}

	destPath := filepath.Join(betaRoot, "copied-hello.txt")
	tr, err := conn.Client.StartTransfer(ctx, TransferRequest{
		Mode:     "push",
		FromNode: "alpha",
		FromPath: helloPath,
		ToNode:   "beta",
		ToPath:   destPath,
	})
	if err != nil {
		t.Fatalf("start transfer: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		list, err := conn.Client.Transfers(ctx)
		if err != nil {
			t.Fatalf("transfers: %v", err)
		}
		done := false
		for _, item := range list {
			if item.ID == tr.ID && item.Job.Status == jobCompleted {
				done = true
				break
			}
			if item.ID == tr.ID && item.Job.Status == jobError {
				t.Fatalf("transfer error: %s", item.Job.Error)
			}
		}
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("transfer timed out")
		}
		time.Sleep(25 * time.Millisecond)
	}

	raw, err := conn.Client.Open(ctx, "beta", destPath, 0)
	if err != nil {
		t.Fatalf("open copied file: %v", err)
	}
	if !strings.Contains(string(raw), "Hello from alpha") {
		t.Fatalf("copied payload mismatch: %q", raw)
	}

	st, err := conn.Client.Settings(ctx)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if st.Net != "demo" {
		t.Fatalf("settings net = %q, want demo", st.Net)
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestConnectProbeAndTokenClobberProtection(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	state := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDone := make(chan error, 1)
	go func() {
		runDone <- Run(ctx, Config{
			Demo:      true,
			StateDir:  state,
			LocalAddr: addr,
		})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-runDone:
		case <-time.After(5 * time.Second):
		}
	})

	// Wait until the first daemon has written gui-token and serves /v1/health.
	var origToken string
	deadline := time.Now().Add(5 * time.Second)
	for {
		if tok, err := LoadGUIToken("", state); err == nil {
			c := NewClient("http://" + addr)
			c.Token = tok
			if err := c.Health(ctx); err == nil {
				origToken = tok
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("background demo daemon did not become healthy")
		}
		time.Sleep(25 * time.Millisecond)
	}

	// Default Connect attaches to the running daemon over TCP without clobbering it.
	attached, err := Connect(ctx, ConnectConfig{
		Agent: Config{StateDir: state, LocalAddr: addr},
	})
	if err != nil {
		t.Fatal(err)
	}
	if attached.Mode != ConnAttached {
		t.Fatalf("mode = %v, want %v", attached.Mode, ConnAttached)
	}
	if err := attached.Client.Health(ctx); err != nil {
		t.Fatalf("attached health: %v", err)
	}
	_ = attached.Close()

	// Launching a second Run against the same LocalAddr/StateDir must fail fast
	// and MUST NOT overwrite gui-token on disk.
	err = Run(ctx, Config{
		Demo:      true,
		StateDir:  state,
		LocalAddr: addr,
	})
	if err == nil {
		t.Fatal("second Run on busy address unexpectedly succeeded")
	}

	afterToken, err := LoadGUIToken("", state)
	if err != nil {
		t.Fatal(err)
	}
	if afterToken != origToken {
		t.Fatalf("gui-token clobbered: got %q, want %q", afterToken, origToken)
	}

	// Original daemon continues answering with original token.
	c := NewClient("http://" + addr)
	c.Token = origToken
	if err := c.Health(ctx); err != nil {
		t.Fatalf("original daemon health failed after second Run: %v", err)
	}

	// Mismatched token against busy port returns ConnOffline cleanly.
	badTokFile := filepath.Join(t.TempDir(), "bad-token")
	if err := os.WriteFile(badTokFile, []byte("wrong-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	offline, err := Connect(ctx, ConnectConfig{
		Agent:     Config{StateDir: state, LocalAddr: addr},
		TokenFile: badTokFile,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer offline.Close()
	if offline.Mode != ConnOffline || offline.ProbeErr == nil {
		t.Fatalf("expected ConnOffline with ProbeErr, got mode=%v err=%v", offline.Mode, offline.ProbeErr)
	}
}

func TestTransfersDeterministicOrderAndClear(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn, err := StartEmbedded(ctx, Config{Demo: true})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	nodes, err := conn.Client.Nodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var alphaRoot, betaRoot string
	for _, n := range nodes {
		if n.ID == "alpha" {
			alphaRoot = n.ReadRoots[0]
		} else if n.ID == "beta" {
			betaRoot = n.ReadRoots[0]
		}
	}

	var wantOrder []string
	for i := 0; i < 5; i++ {
		tr, err := conn.Client.StartTransfer(ctx, TransferRequest{
			Mode:     "push",
			FromNode: "alpha",
			FromPath: filepath.Join(alphaRoot, "hello.txt"),
			ToNode:   "beta",
			ToPath:   filepath.Join(betaRoot, fmt.Sprintf("order-%d.txt", i)),
		})
		if err != nil {
			t.Fatal(err)
		}
		wantOrder = append(wantOrder, tr.ID)
	}

	// Wait for all 5 transfers to reach terminal state.
	deadline := time.Now().Add(5 * time.Second)
	for {
		list, err := conn.Client.Transfers(ctx)
		if err != nil {
			t.Fatal(err)
		}
		allDone := len(list) == 5
		for _, tr := range list {
			if tr.Job.Status == jobRunning || tr.Job.Status == jobQueued {
				allDone = false
				break
			}
		}
		if allDone {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for transfers to finish")
		}
		time.Sleep(25 * time.Millisecond)
	}

	// Verify 20 consecutive Transfers() polls return the exact same deterministic order.
	for poll := 0; poll < 20; poll++ {
		list, err := conn.Client.Transfers(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != len(wantOrder) {
			t.Fatalf("poll %d: len=%d, want %d", poll, len(list), len(wantOrder))
		}
		for i, tr := range list {
			if tr.ID != wantOrder[i] {
				t.Fatalf("poll %d idx %d: got %q, want %q", poll, i, tr.ID, wantOrder[i])
			}
		}
	}

	// Dismiss one record explicitly, then ClearTransfers for the remaining finished ones.
	if err := conn.Client.DismissTransfer(ctx, wantOrder[0]); err != nil {
		t.Fatalf("dismiss transfer: %v", err)
	}
	list, err := conn.Client.Transfers(ctx)
	if err != nil || len(list) != 4 {
		t.Fatalf("after dismiss: len=%d err=%v", len(list), err)
	}
	remaining, err := conn.Client.ClearTransfers(ctx)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("after clear: remaining=%d err=%v", len(remaining), err)
	}
	list, err = conn.Client.Transfers(ctx)
	if err != nil || len(list) != 0 {
		t.Fatalf("after clear reload: len=%d err=%v", len(list), err)
	}
}

func TestNonDemoServerPrunesPollutedDemoTracks(t *testing.T) {
	dir := t.TempDir()
	state := t.TempDir()

	disk := struct {
		Transfers []transferRec `json:"transfers"`
	}{
		Transfers: []transferRec{
			{
				ID:     "demo-polluted",
				Runner: "alpha",
				Req: TransferRequest{
					Mode:     "push",
					FromNode: "alpha",
					FromPath: `C:\Users\mzf\AppData\Local\Temp\tailfile-demo-1234\alpha\hello.txt`,
					ToNode:   "beta",
					ToPath:   `C:/Users/mzf/AppData/Local/Temp/tailfile-demo-1234/beta/hello.txt`,
				},
			},
			{
				ID:     "real-transfer",
				Runner: "iori-rog",
				Req: TransferRequest{
					Mode:     "push",
					FromNode: "iori-rog",
					FromPath: filepath.Join(dir, "real.bin"),
					ToNode:   "iori-claw",
					ToPath:   filepath.Join(dir, "dest.bin"),
				},
			},
		},
	}
	raw, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "transfers.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	s := newServer("iori-rog", "tsnet", "ada@example.com", Roots{Read: []string{dir}, Write: []string{dir}}, 1024)
	t.Cleanup(func() { _ = s.Close() })
	if err := s.attachState(state); err != nil {
		t.Fatal(err)
	}

	s.trackMu.Lock()
	_, hasDemo := s.tracks["demo-polluted"]
	realRec, hasReal := s.tracks["real-transfer"]
	s.trackMu.Unlock()
	if hasDemo {
		t.Fatal("polluted demo transfer was not pruned on attachState")
	}
	if !hasReal || realRec.Seq == 0 {
		t.Fatalf("real transfer missing or missing seq: %v %+v", hasReal, realRec)
	}

	cleanedRaw, err := os.ReadFile(filepath.Join(state, "transfers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cleanedRaw), "demo-polluted") {
		t.Fatalf("transfers.json still contains pruned demo record: %s", cleanedRaw)
	}
}
