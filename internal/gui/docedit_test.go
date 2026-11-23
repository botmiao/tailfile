package gui

import (
	"bytes"
	"testing"
)

func TestDocEditDecodeAndEncodeRoundTrip(t *testing.T) {
	cases := []struct {
		name     string
		raw      []byte
		wantText string
		wantEOL  LineEnding
		wantBOM  bool
	}{
		{"empty", nil, "", LineEndingLF, false},
		{"lf", []byte("a\nb\n"), "a\nb\n", LineEndingLF, false},
		{"crlf", []byte("a\r\nb\r\n"), "a\nb\n", LineEndingCRLF, false},
		{"utf8_bom_crlf", append([]byte{0xEF, 0xBB, 0xBF}, []byte("第一行\r\n第二行")...), "第一行\n第二行", LineEndingCRLF, true},
		{"utf8_bom_lf", append([]byte{0xEF, 0xBB, 0xBF}, []byte("alpha\nbeta")...), "alpha\nbeta", LineEndingLF, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			txt, eol, bom, err := DecodeEditableText(tc.raw)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if txt != tc.wantText || eol != tc.wantEOL || bom != tc.wantBOM {
				t.Fatalf("got (%q, %v, %v), want (%q, %v, %v)", txt, eol, bom, tc.wantText, tc.wantEOL, tc.wantBOM)
			}
			reencoded := EncodeEditableText(txt, eol, bom)
			if !bytes.Equal(reencoded, tc.raw) {
				t.Fatalf("roundtrip = %q, want %q", reencoded, tc.raw)
			}
		})
	}

	if _, _, _, err := DecodeEditableText([]byte("abc\x00def")); err == nil {
		t.Fatal("expected NUL byte binary rejection")
	}
	if _, _, _, err := DecodeEditableText([]byte{0xff, 0xfe, 0xfd}); err == nil {
		t.Fatal("expected invalid UTF-8 rejection")
	}
}

func TestDocEditIsLikelyEditableName(t *testing.T) {
	editable := []string{
		"hello.txt", "README.md", "main.go", "config.toml", ".env",
		".gitignore", "Makefile", "Dockerfile", "query.sql", "widget.cpp",
	}
	for _, name := range editable {
		if !IsLikelyEditableName(name) {
			t.Fatalf("%q should be editable", name)
		}
	}

	nonEditable := []string{
		"photo.png", "doc.pdf", "sheet.xlsx", "archive.zip", "app.exe",
		"blob.bin", "song.mp3", "movie.mp4", "font.woff2", "a.txt.tailfile-partial",
	}
	for _, name := range nonEditable {
		if IsLikelyEditableName(name) {
			t.Fatalf("%q should not be editable", name)
		}
	}
}

func TestDocEditFindTextMatchesRuneOffsets(t *testing.T) {
	if got := FindTextMatches("hello", ""); got != nil {
		t.Fatalf("empty query = %+v, want nil", got)
	}
	// Mix CJK multi-byte runes and ASCII to ensure offsets are runes, not UTF-8 bytes.
	text := "你好Alpha\nalpha世界ALPHA"
	got := FindTextMatches(text, "alpha")
	want := []TextMatchRange{
		{Start: 2, End: 7},
		{Start: 8, End: 13},
		{Start: 15, End: 20},
	}
	if len(got) != len(want) {
		t.Fatalf("matches = %+v, want %+v", got, want)
	}
	textRunes := []rune(text)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("match[%d] = %+v, want %+v (%q)", i, got[i], want[i], string(textRunes[got[i].Start:got[i].End]))
		}
	}
}
