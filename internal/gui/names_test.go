package gui

import "testing"

func TestRenameStemRuneLen(t *testing.T) {
	cases := []struct {
		name  string
		isDir bool
		want  int
	}{
		{name: "hello.txt", isDir: false, want: 5},
		{name: "文档.pdf", isDir: false, want: 2},
		{name: "archive.tar.gz", isDir: false, want: 11},
		{name: "README", isDir: false, want: 6},
		{name: ".gitignore", isDir: false, want: 10},
		{name: ".env.local", isDir: false, want: 4},
		{name: "v1.0.notes", isDir: true, want: 10},
	}
	for _, tc := range cases {
		if got := RenameStemRuneLen(tc.name, tc.isDir); got != tc.want {
			t.Fatalf("RenameStemRuneLen(%q, %v) = %d, want %d", tc.name, tc.isDir, got, tc.want)
		}
	}
}
