package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"
)

func TestWindowsResourceObjectsContainRsrcSection(t *testing.T) {
	cases := []struct {
		file    string
		machine uint16
	}{
		{"rsrc_windows_amd64.syso", 0x8664},
		{"rsrc_windows_arm64.syso", 0xAA64},
	}
	for _, tc := range cases {
		raw, err := os.ReadFile(tc.file)
		if err != nil {
			t.Fatalf("missing %s: %v", tc.file, err)
		}
		if len(raw) < 128 {
			t.Fatalf("%s unexpectedly small: %d bytes", tc.file, len(raw))
		}
		if got := binary.LittleEndian.Uint16(raw[0:2]); got != tc.machine {
			t.Fatalf("%s COFF machine = 0x%04x, want 0x%04x", tc.file, got, tc.machine)
		}
		if !bytes.Equal(raw[20:25], []byte(".rsrc")) {
			t.Fatalf("%s missing .rsrc section header", tc.file)
		}
		wantUTF16 := []byte{'V', 0, 'S', 0, '_', 0, 'V', 0, 'E', 0, 'R', 0, 'S', 0, 'I', 0, 'O', 0, 'N', 0}
		if !bytes.Contains(raw, wantUTF16) {
			t.Fatalf("%s missing UTF-16LE VS_VERSION_INFO payload", tc.file)
		}
		if !bytes.Contains(raw, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
			t.Fatalf("%s missing 256px PNG icon payload", tc.file)
		}
		if !bytes.Contains(raw, []byte("dev.iori.tailfile")) {
			t.Fatalf("%s missing manifest identifier payload", tc.file)
		}
	}
}
