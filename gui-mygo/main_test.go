package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
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
