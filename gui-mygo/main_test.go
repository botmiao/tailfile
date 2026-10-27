package main

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"tailfile.local/agent"
)

func TestChrome(t *testing.T) {
	a := newApp("http://127.0.0.1:1", "")
	a.status = "Agent is not running"
	tt := ui.NewTester(a.view, 1200, 800)
	if !tt.HasText("Tailfile") {
		t.Fatalf("texts: %q", tt.Texts())
	}
	if err := tt.Click("Copy"); err != nil {
		t.Fatal(err)
	}
	if a.status != "Select a file or folder to copy" {
		t.Fatalf("status %q", a.status)
	}
	if err := tt.Click("Paste"); err != nil {
		t.Fatal(err)
	}
	if a.status != "Clipboard is empty" {
		t.Fatalf("status %q", a.status)
	}
}

func TestConflictAndSettingsDialogs(t *testing.T) {
	a := newApp("http://127.0.0.1:1", "")
	a.transfers = []agent.Transfer{{
		ID:       "t1",
		FromNode: "alpha",
		ToNode:   "beta",
		Job: agent.Job{
			ID:     "j1",
			Status: "conflict",
			Conflict: &agent.FileConflict{
				Rel:  "note.txt",
				Path: "/tmp/note.txt",
			},
		},
	}}
	a.settingsOpen = true
	a.netMode = "tsnet"
	a.authConfigured = true
	tt := ui.NewTester(a.view, 1200, 800)
	for _, text := range []string{"覆盖", "跳过", "对后续冲突都这样处理", "Net mode: tsnet", "configured"} {
		if !tt.HasText(text) {
			t.Fatalf("missing %q in %q", text, tt.Texts())
		}
	}
}
