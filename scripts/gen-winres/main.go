// Pure-Go Windows COFF resource (.syso) generator for Tailfile icons, manifest, and version metadata.
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
)

type bundleConfig struct {
	Name       string `json:"name"`
	Slug       string `json:"slug"`
	Identifier string `json:"identifier"`
	Version    string `json:"version"`
	Icon       string `json:"icon"`
}

var windowsIconSizes = []int{16, 24, 32, 48, 64, 128, 256}

func main() {
	root, err := os.Getwd()
	if err != nil {
		fatal(err)
	}
	rawCfg, err := os.ReadFile(filepath.Join(root, "mygo.json"))
	if err != nil {
		fatal(err)
	}
	var cfg bundleConfig
	if err := json.Unmarshal(rawCfg, &cfg); err != nil {
		fatal(err)
	}
	if cfg.Icon == "" {
		cfg.Icon = "resources/icon.png"
	}
	iconBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(cfg.Icon)))
	if err != nil {
		fatal(err)
	}
	srcImg, err := png.Decode(bytes.NewReader(iconBytes))
	if err != nil {
		fatal(err)
	}

	images, err := encodeWindowsIcons(srcImg)
	if err != nil {
		fatal(err)
	}

	targets := []struct {
		goarch string
		file   string
	}{
		{"amd64", "rsrc_windows_amd64.syso"},
		{"arm64", "rsrc_windows_arm64.syso"},
	}
	for _, tgt := range targets {
		syso, err := buildWinresSyso(cfg, tgt.goarch, images)
		if err != nil {
			fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, tgt.file), syso, 0o644); err != nil {
			fatal(err)
		}
		fmt.Printf("generated %s (%d bytes)\n", tgt.file, len(syso))
	}
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "gen-winres: %v\n", err)
	os.Exit(1)
}

func encodeWindowsIcons(src image.Image) ([][]byte, error) {
	out := make([][]byte, len(windowsIconSizes))
	for i, size := range windowsIconSizes {
		scaled := resizeNRGBA(src, size)
		if size >= 256 {
			var p bytes.Buffer
			if err := png.Encode(&p, scaled); err != nil {
				return nil, err
			}
			out[i] = p.Bytes()
			continue
		}
		out[i] = encodeIconDIB32(scaled)
	}
	return out, nil
}

// encodeIconDIB32 writes a standard 32bpp bottom-up BGRA DIB (XOR pixels + padded AND mask)
// so legacy Win32 ExtractAssociatedIcon/GDI and Explorer render every icon size crisply.
func encodeIconDIB32(img *image.NRGBA) []byte {
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	andStride := ((w + 31) / 32) * 4
	xorSize := w * h * 4
	andSize := andStride * h

	var b bytes.Buffer
	le := binary.LittleEndian
	_ = binary.Write(&b, le, uint32(40)) // biSize
	_ = binary.Write(&b, le, int32(w))   // biWidth
	_ = binary.Write(&b, le, int32(h*2)) // biHeight (XOR + AND)
	_ = binary.Write(&b, le, uint16(1))  // biPlanes
	_ = binary.Write(&b, le, uint16(32)) // biBitCount
	_ = binary.Write(&b, le, uint32(0))  // biCompression = BI_RGB
	_ = binary.Write(&b, le, uint32(xorSize+andSize))
	_ = binary.Write(&b, le, int32(0))  // biXPelsPerMeter
	_ = binary.Write(&b, le, int32(0))  // biYPelsPerMeter
	_ = binary.Write(&b, le, uint32(0)) // biClrUsed
	_ = binary.Write(&b, le, uint32(0)) // biClrImportant

	for y := h - 1; y >= 0; y-- {
		for x := 0; x < w; x++ {
			c := img.NRGBAAt(x, y)
			b.Write([]byte{c.B, c.G, c.R, c.A})
		}
	}
	andRow := make([]byte, andStride)
	for y := h - 1; y >= 0; y-- {
		clear(andRow)
		for x := 0; x < w; x++ {
			if img.NRGBAAt(x, y).A < 128 {
				andRow[x/8] |= 0x80 >> (x % 8)
			}
		}
		b.Write(andRow)
	}
	return b.Bytes()
}

func resizeNRGBA(img image.Image, size int) *image.NRGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	if w == 0 || h == 0 {
		return dst
	}
	sx, sy := float64(w)/float64(size), float64(h)/float64(size)
	at := func(x, y int) (float64, float64, float64, float64) {
		c := color.NRGBAModel.Convert(img.At(b.Min.X+min(max(x, 0), w-1), b.Min.Y+min(max(y, 0), h-1))).(color.NRGBA)
		a := float64(c.A)
		return float64(c.R) * a, float64(c.G) * a, float64(c.B) * a, a
	}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var r, g, bl, a, n float64
			if sx >= 1 {
				x0, x1 := int(float64(x)*sx), max(int(float64(x+1)*sx), int(float64(x)*sx)+1)
				y0, y1 := int(float64(y)*sy), max(int(float64(y+1)*sy), int(float64(y)*sy)+1)
				for yy := y0; yy < y1; yy++ {
					for xx := x0; xx < x1; xx++ {
						cr, cg, cb, ca := at(xx, yy)
						r, g, bl, a, n = r+cr, g+cg, bl+cb, a+ca, n+1
					}
				}
			} else {
				fx, fy := (float64(x)+0.5)*sx-0.5, (float64(y)+0.5)*sy-0.5
				x0, y0 := int(fx), int(fy)
				dx, dy := fx-float64(x0), fy-float64(y0)
				for _, s := range [4]struct {
					x, y int
					wt   float64
				}{{x0, y0, (1 - dx) * (1 - dy)}, {x0 + 1, y0, dx * (1 - dy)}, {x0, y0 + 1, (1 - dx) * dy}, {x0 + 1, y0 + 1, dx * dy}} {
					cr, cg, cb, ca := at(s.x, s.y)
					r, g, bl, a, n = r+cr*s.wt, g+cg*s.wt, bl+cb*s.wt, a+ca*s.wt, n+s.wt
				}
			}
			if a == 0 {
				continue
			}
			dst.SetNRGBA(x, y, color.NRGBA{
				R: uint8(r/a + 0.5),
				G: uint8(g/a + 0.5),
				B: uint8(bl/a + 0.5),
				A: uint8(a/n + 0.5),
			})
		}
	}
	return dst
}

func buildWinresSyso(cfg bundleConfig, goarch string, images [][]byte) ([]byte, error) {
	arch, ok := map[string]struct{ machine, relocation uint16 }{
		"amd64": {0x8664, 0x0003},
		"arm64": {0xAA64, 0x0002},
		"386":   {0x014C, 0x0007},
	}[goarch]
	if !ok {
		return nil, fmt.Errorf("unsupported Windows arch %q", goarch)
	}

	const (
		rtIcon      = 3
		rtGroupIcon = 14
		rtVersion   = 16
		rtManifest  = 24
		langUS      = 0x0409
	)

	res := map[uint16]map[uint16][]byte{
		rtIcon:     {},
		rtVersion:  {1: buildVersionInfo(cfg, langUS)},
		rtManifest: {1: []byte(buildManifest(cfg))},
	}

	var group bytes.Buffer
	_ = binary.Write(&group, binary.LittleEndian, [3]uint16{0, 1, uint16(len(images))})
	for i, img := range images {
		id := uint16(i + 1)
		res[rtIcon][id] = img
		dim := byte(windowsIconSizes[i])
		if windowsIconSizes[i] >= 256 {
			dim = 0
		}
		group.Write([]byte{dim, dim, 0, 0})
		_ = binary.Write(&group, binary.LittleEndian, [2]uint16{1, 32})
		_ = binary.Write(&group, binary.LittleEndian, uint32(len(img)))
		_ = binary.Write(&group, binary.LittleEndian, id)
	}
	res[rtGroupIcon] = map[uint16][]byte{1: group.Bytes()}

	types := sortedKeys(res)
	dirSize := func(n int) int { return 16 + 8*n }
	size := dirSize(len(types))
	count := 0
	for _, t := range types {
		size += dirSize(len(res[t])) + len(res[t])*dirSize(1)
		count += len(res[t])
	}
	entriesAt := size
	dataAt := entriesAt + 16*count

	raw := make([]byte, dataAt)
	le := binary.LittleEndian
	dir := func(at, n int) { le.PutUint16(raw[at+14:], uint16(n)) }

	var relocs []uint32
	var data bytes.Buffer
	dir(0, len(types))
	next := dirSize(len(types))
	entry := entriesAt
	for i, t := range types {
		le.PutUint32(raw[16+8*i:], uint32(t))
		le.PutUint32(raw[16+8*i+4:], uint32(next)|0x80000000)
		ids := sortedKeys(res[t])
		idDir := next
		dir(idDir, len(ids))
		next += dirSize(len(ids))
		for j, id := range ids {
			le.PutUint32(raw[idDir+16+8*j:], uint32(id))
			le.PutUint32(raw[idDir+16+8*j+4:], uint32(next)|0x80000000)
			dir(next, 1)
			le.PutUint32(raw[next+16:], uint32(langUS))
			le.PutUint32(raw[next+20:], uint32(entry))
			next += dirSize(1)

			for data.Len()%8 != 0 {
				data.WriteByte(0)
			}
			le.PutUint32(raw[entry:], uint32(dataAt+data.Len()))
			le.PutUint32(raw[entry+4:], uint32(len(res[t][id])))
			relocs = append(relocs, uint32(entry))
			entry += 16
			data.Write(res[t][id])
		}
	}
	raw = append(raw, data.Bytes()...)
	for len(raw)%4 != 0 {
		raw = append(raw, 0)
	}

	var out bytes.Buffer
	const headerSize, sectionHeaderSize = 20, 40
	rawAt := headerSize + sectionHeaderSize
	relocAt := rawAt + len(raw)
	symbolsAt := relocAt + 10*len(relocs)
	_ = binary.Write(&out, le, struct {
		Machine, Sections                   uint16
		Time, Symbols, SymbolCount          uint32
		OptionalHeaderSize, Characteristics uint16
	}{arch.machine, 1, 0, uint32(symbolsAt), 1, 0, 0})
	_ = binary.Write(&out, le, struct {
		Name                                        [8]byte
		VirtualSize, VirtualAddress, RawSize, RawAt uint32
		RelocAt, LineNumbersAt                      uint32
		Relocs, LineNumbers                         uint16
		Characteristics                             uint32
	}{[8]byte{'.', 'r', 's', 'r', 'c'}, 0, 0, uint32(len(raw)), uint32(rawAt), uint32(relocAt), 0, uint16(len(relocs)), 0, 0x40000040})
	out.Write(raw)
	for _, r := range relocs {
		_ = binary.Write(&out, le, r)
		_ = binary.Write(&out, le, uint32(0))
		_ = binary.Write(&out, le, arch.relocation)
	}
	out.Write([]byte{'.', 'r', 's', 'r', 'c', 0, 0, 0})
	_ = binary.Write(&out, le, uint32(0))
	_ = binary.Write(&out, le, uint16(1))
	_ = binary.Write(&out, le, uint16(0))
	out.Write([]byte{3, 0})
	_ = binary.Write(&out, le, uint32(4))
	return out.Bytes(), nil
}

func sortedKeys[V any](m map[uint16]V) []uint16 {
	keys := make([]uint16, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func parseVersion4(v string) [4]uint16 {
	var out [4]uint16
	v, _, _ = strings.Cut(v, "-")
	for i, part := range strings.SplitN(v, ".", 4) {
		n, _ := strconv.Atoi(part)
		out[i] = uint16(n)
	}
	return out
}

func buildManifest(cfg bundleConfig) string {
	v := parseVersion4(cfg.Version)
	name := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' {
			return r
		}
		return -1
	}, cfg.Identifier)
	if name == "" {
		name = "tailfile"
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <assemblyIdentity type="win32" name="%s" version="%d.%d.%d.%d" processorArchitecture="*"/>
  <dependency>
    <dependentAssembly>
      <assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="6.0.0.0" processorArchitecture="*" publicKeyToken="6595b64144ccf1df" language="*"/>
    </dependentAssembly>
  </dependency>
  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
    <application>
      <supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/>
    </application>
  </compatibility>
  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true/pm</dpiAware>
      <dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">PerMonitorV2</dpiAwareness>
      <activeCodePage xmlns="http://schemas.microsoft.com/SMI/2019/WindowsSettings">UTF-8</activeCodePage>
    </windowsSettings>
  </application>
</assembly>
`, name, v[0], v[1], v[2], v[3])
}

type verNode struct {
	key      string
	text     bool
	value    []byte
	valueLen int
	children []verNode
}

func (n verNode) encode() []byte {
	b := make([]byte, 6)
	for _, u := range utf16.Encode([]rune(n.key + "\x00")) {
		b = binary.LittleEndian.AppendUint16(b, u)
	}
	pad := func() {
		for len(b)%4 != 0 {
			b = append(b, 0)
		}
	}
	pad()
	b = append(b, n.value...)
	for _, child := range n.children {
		pad()
		b = append(b, child.encode()...)
	}
	binary.LittleEndian.PutUint16(b[0:], uint16(len(b)))
	binary.LittleEndian.PutUint16(b[2:], uint16(n.valueLen))
	if n.text {
		binary.LittleEndian.PutUint16(b[4:], 1)
	}
	return b
}

func buildVersionInfo(cfg bundleConfig, lang uint16) []byte {
	v := parseVersion4(cfg.Version)
	fixed := make([]byte, 52)
	le := binary.LittleEndian
	le.PutUint32(fixed[0:], 0xFEEF04BD)
	le.PutUint32(fixed[4:], 0x00010000)
	le.PutUint32(fixed[8:], uint32(v[0])<<16|uint32(v[1]))
	le.PutUint32(fixed[12:], uint32(v[2])<<16|uint32(v[3]))
	copy(fixed[16:24], fixed[8:16])
	le.PutUint32(fixed[24:], 0x3F)
	le.PutUint32(fixed[32:], 0x00040004)
	le.PutUint32(fixed[36:], 1)

	exe := cfg.Slug
	if exe == "" {
		exe = "tailfile"
	}
	exe += ".exe"
	title := cfg.Name
	if title == "" {
		title = "Tailfile"
	}

	var table []verNode
	for _, kv := range [][2]string{
		{"FileDescription", title},
		{"FileVersion", cfg.Version},
		{"InternalName", exe},
		{"OriginalFilename", exe},
		{"ProductName", title},
		{"ProductVersion", cfg.Version},
		{"MyGoIdentifier", cfg.Identifier},
	} {
		if kv[1] == "" {
			continue
		}
		text := utf16.Encode([]rune(kv[1] + "\x00"))
		val := make([]byte, 2*len(text))
		for i, u := range text {
			le.PutUint16(val[2*i:], u)
		}
		table = append(table, verNode{key: kv[0], text: true, value: val, valueLen: len(text)})
	}
	tables := []verNode{{key: fmt.Sprintf("%04x04b0", lang), text: true, children: table}}
	trans := []byte{byte(lang), byte(lang >> 8), 0xB0, 0x04}
	root := verNode{
		key:      "VS_VERSION_INFO",
		value:    fixed,
		valueLen: len(fixed),
		children: []verNode{
			{key: "StringFileInfo", text: true, children: tables},
			{key: "VarFileInfo", text: true, children: []verNode{{key: "Translation", value: trans, valueLen: len(trans)}}},
		},
	}
	return root.encode()
}
