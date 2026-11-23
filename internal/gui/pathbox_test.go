package gui

import "testing"

func TestPathSandboxAndNormalization(t *testing.T) {
	if got := CanonicalizeRemote(`/srv/alpha/../alpha2`); got != `/srv/alpha2` {
		t.Fatalf("got %q", got)
	}
	if got := CanonicalizeRemote(`/srv/alpha/../../../etc/passwd`); got != `/etc/passwd` {
		t.Fatalf("got %q", got)
	}
	if got := CanonicalizeRemote(`C:\Users\Ada\..\Tailfile\`); got != `C:/Users/Tailfile` {
		t.Fatalf("got %q", got)
	}
	if got := CanonicalizeRemote(`C:\..\..\..`); got != `C:/` {
		t.Fatalf("drive clamp got %q", got)
	}
	if got := CanonicalizeRemote(`\\server\share\sub\..\ok`); got != `//server/share/ok` {
		t.Fatalf("UNC got %q", got)
	}
	if got := CanonicalizeRemote(`\\server\share\..\..`); got != `//server/share` {
		t.Fatalf("UNC clamp got %q", got)
	}
	if got := CanonicalizeRemote(`//server/../share2/secret`); PathIsUnder(got, `//server/share`) {
		t.Fatalf("malformed UNC traversed into share: %q", got)
	}

	if PathIsUnder(`/srv/alpha2`, `/srv/alpha`) {
		t.Fatal("sibling prefix /srv/alpha2 must NOT match /srv/alpha")
	}
	if PathIsUnder(`/srv/alpha/../alpha2`, `/srv/alpha`) {
		t.Fatal("dotdot escape must NOT match /srv/alpha")
	}
	if !PathIsUnder(`c:/Tailfile/sub/a.txt`, `C:\Tailfile`) {
		t.Fatal("Windows drive case/slash mismatch")
	}
	if PathIsUnder(`C:\Tailfile2\a.txt`, `C:\Tailfile`) {
		t.Fatal("Windows drive sibling prefix must fail")
	}
	if PathIsUnder(`//server/share2/a`, `//server/share`) {
		t.Fatal("UNC sibling share must fail")
	}
	if !PathIsUnder(`//SERVER/SHARE/sub`, `//server/share`) {
		t.Fatal("UNC case-insensitive share should match")
	}
	if PathIsUnder(`relative/path`, `/`) {
		t.Fatal("relative path must not match root")
	}

	if parent, ok := ParentRemote(`/srv/alpha`, []string{`/srv/alpha`}); ok || parent != "" {
		t.Fatalf("expected root clamp, got %q %v", parent, ok)
	}
	if parent, ok := ParentRemote(`/srv/alpha/notes`, []string{`/srv/alpha`}); !ok || parent != `/srv/alpha` {
		t.Fatalf("parent got %q %v", parent, ok)
	}
	crumbs := BuildCrumbs(`/home/ada/Tailfile/notes/2026`, []string{`/home/ada/Tailfile`})
	if len(crumbs) != 3 || crumbs[0].Label != "Tailfile" || crumbs[2].Path != "/home/ada/Tailfile/notes/2026" {
		t.Fatalf("crumbs %+v", crumbs)
	}
}

func TestFileURIToPath(t *testing.T) {
	if got := FileURIToPath("https://example.com/a.txt"); got != "" {
		t.Fatalf("non-file URI must return empty, got %q", got)
	}
	if got := FileURIToPath("file:///C:/Users/Ada/My%20File.txt"); got != `C:\Users\Ada\My File.txt` && got != `C:/Users/Ada/My File.txt` {
		t.Fatalf("drive URI got %q", got)
	}
	if got := FileURIToPath("file://nas/share/dir/a.bin"); got != `\\nas\share\dir\a.bin` && got != `//nas/share/dir/a.bin` {
		t.Fatalf("UNC URI got %q", got)
	}
}
