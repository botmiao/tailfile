package gui

import (
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
)

// Lucide 24x24 & host OS silhouette vector paths (ISC/MIT Lucide + monochrome marks).
const (
	PathFolder         = "M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"
	PathFile           = "M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7ZM14 2v4a2 2 0 0 0 2 2h4"
	PathFileText       = "M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7ZM14 2v4a2 2 0 0 0 2 2h4M10 9H8M16 13H8M16 17H8"
	PathImage          = "M21 15V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-4M21 15l-5-5L5 21M8.5 10a1.5 1.5 0 1 0 0-3 1.5 1.5 0 0 0 0 3"
	PathCopy           = "M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2M8 8h10c1.1 0 2 .9 2 2v10c0 1.1-.9 2-2 2H8c-1.1 0-2-.9-2-2V10c0-1.1.9-2 2-2"
	PathClipboard      = "M16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2M9 2h6a1 1 0 0 1 1 1v2a1 1 0 0 1-1 1H9a1 1 0 0 1-1-1V3a1 1 0 0 1 1-1"
	PathPencil         = "M21.174 6.812a1 1 0 0 0-3.986-3.987L3.842 16.174a2 2 0 0 0-.5.83l-1.321 4.352a.5.5 0 0 0 .623.622l4.353-1.32a2 2 0 0 0 .83-.497zM15 5l4 4"
	PathRename         = "M12 4h1a2 2 0 0 1 2 2a2 2 0 0 1 2-2h1M12 20h1a2 2 0 0 0 2-2a2 2 0 0 0 2 2h1M15 6v12M12 8H4a2 2 0 0 0-2 2v4a2 2 0 0 0 2 2h8M18 8h2a2 2 0 0 1 2 2v4a2 2 0 0 1-2 2h-2"
	PathTrash          = "M3 6h18M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2M10 11v6M14 11v6"
	PathRefresh        = "M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8M21 3v5h-5M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16M8 16H3v5"
	PathFolderPlus     = "M12 10v6M9 13h6M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"
	PathChevronLeft    = "m15 18-6-6 6-6"
	PathChevronRight   = "m9 18 6-6-6-6"
	PathArrowUp        = "m5 12 7-7 7 7M12 19V5"
	PathSearch         = "m21 21-4.34-4.34M11 19a8 8 0 1 0 0-16 8 8 0 0 0 0 16"
	PathList           = "M3 12h.01M3 18h.01M3 6h.01M8 12h13M8 18h13M8 6h13"
	PathLayoutGrid     = "M3 3h7v7H3zM14 3h7v7h-7zM14 14h7v7h-7zM3 14h7v7H3z"
	PathPanelRight     = "M15 3v18M3 5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"
	PathPanelLeftClose = "M5 3h14a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2zM9 3v18M16 15l-3-3 3-3"
	PathPanelLeftOpen  = "M5 3h14a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2zM9 3v18M14 9l3 3-3 3"
	PathSettings       = "M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2zM12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6"
	PathSun            = "M12 8a4 4 0 1 0 0 8 4 4 0 1 0 0-8M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41"
	PathMoon           = "M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"
	PathServer         = "M4 6a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v2a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2zM4 16a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v2a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2zM6 8h.01M6 18h.01"
	PathLock           = "M7 11V7a5 5 0 0 1 10 0v4M5 11h14a2 2 0 0 1 2 2v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-7a2 2 0 0 1 2-2"
	PathPlus           = "M5 12h14M12 5v14"
	PathX              = "M18 6 6 18M6 6l12 12"
	PathMenu           = "M4 6h16M4 12h16M4 18h16"
	PathEllipsis       = "M12 12h.01M19 12h.01M5 12h.01"
	PathExternalLink   = "M15 3h6v6M10 14 21 3M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"
	PathApple          = "M16.37 12.25c-.03-2.72 2.22-4.02 2.32-4.09-1.27-1.85-3.24-2.1-3.94-2.13-1.68-.17-3.28 1-4.13 1-.86 0-2.17-.97-3.57-.94-1.83.03-3.52 1.07-4.46 2.71-1.9 3.3-.49 8.18 1.37 10.86.9 1.31 1.98 2.78 3.39 2.73 1.36-.06 1.88-.88 3.53-.88 1.64 0 2.11.88 3.55.85 1.46-.02 2.39-1.33 3.28-2.65.98-1.48 1.38-2.91 1.4-2.98-.03-.01-2.69-1.03-2.72-4.09zM14.34 4.11c.75-.91 1.25-2.17 1.11-3.43-1.07.04-2.37.71-3.14 1.62-.69.8-1.29 2.08-1.13 3.31 1.2.09 2.42-.61 3.16-1.5z"
	PathWindows        = "M3.1 5.35 10.55 4.25 10.55 11.15 3.1 11.55Z M12.15 4.05 20.9 2.9 20.9 11.05 12.15 11.15Z M3.1 12.85 10.55 13.15 10.55 20.15 3.1 18.85Z M12.15 13.25 20.9 13.4 20.9 21.15 12.15 19.95Z"
	PathTux            = "M12 2.0C9.3 2.0 7.4 3.9 7.4 6.5 7.4 7.3 7.6 8.0 7.9 8.6 5.8 9.8 3.9 12.1 3.2 14.5 2.9 15.4 3.8 16.1 4.6 15.5L6.4 13.9C6.4 16.5 7.4 18.3 9.0 19.0L15.0 19.0C16.6 18.3 17.6 16.5 17.6 13.9L19.4 15.5C20.2 16.1 21.1 15.4 20.8 14.5 20.1 12.1 18.2 9.8 16.1 8.6 16.4 8.0 16.6 7.3 16.6 6.5 16.6 3.9 14.7 2.0 12 2.0ZM12 10.5C14.3 10.5 15.8 12.2 15.8 14.6 15.8 16.9 14.3 18.0 12 18.0 9.7 18.0 8.2 16.9 8.2 14.6 8.2 12.2 9.7 10.5 12 10.5ZM8.8 6.0a1.05 1.05 0 1 0 2.1 0 1.05 1.05 0 1 0-2.1 0ZM13.1 6.0a1.05 1.05 0 1 0 2.1 0 1.05 1.05 0 1 0-2.1 0ZM12 7.2 13.8 8.3 12 9.8 10.2 8.3ZM6.4 19.6h4.0a1.1 1.1 0 0 1 0 2.2H6.4a1.1 1.1 0 0 1 0-2.2ZM13.6 19.6h4.0a1.1 1.1 0 0 1 0 2.2h-4.0a1.1 1.1 0 0 1 0-2.2Z"
	PathAndroid        = "M8.15 4.05 6.85 2.15l.95-.55 1.4 2.05A6.4 6.4 0 0 1 12 3.2a6.4 6.4 0 0 1 2.8.45l1.4-2.05.95.55-1.3 1.9A6.3 6.3 0 0 1 18.2 9.2V11H5.8V9.2a6.3 6.3 0 0 1 2.35-5.15z M5.6 12.2h12.8v6.15a1.7 1.7 0 0 1-1.7 1.7H7.3a1.7 1.7 0 0 1-1.7-1.7z M2.7 12.55h2.15v5.15a1.05 1.05 0 0 1-1.05 1.05H3.2a.5.5 0 0 1-.5-.5z M19.15 12.55H21.3v5.7a.5.5 0 0 1-.5.5h-.6a1.05 1.05 0 0 1-1.05-1.05z M7.7 20.35h2.35v2.15a.85.85 0 0 1-.85.85h-.65a.85.85 0 0 1-.85-.85z M13.95 20.35h2.35v2.15a.85.85 0 0 1-.85.85h-.65a.85.85 0 0 1-.85-.85z M9.15 8.15a.9.9 0 1 0 .02 0z M14.85 8.15a.9.9 0 1 0 .02 0z"
	PathIPhone         = "M8 1.45h8a2.7 2.7 0 0 1 2.7 2.7v15.7a2.7 2.7 0 0 1-2.7 2.7H8a2.7 2.7 0 0 1-2.7-2.7V4.15A2.7 2.7 0 0 1 8 1.45z M9.15 4.15h5.7a1.15 1.15 0 0 1 1.15 1.15v9.55a1.15 1.15 0 0 1-1.15 1.15h-5.7a1.15 1.15 0 0 1-1.15-1.15V5.3a1.15 1.15 0 0 1 1.15-1.15z M12 18.55a1.2 1.2 0 1 0 .02 0z"
	PathPage           = "M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8Z"
	PathFold           = "M14 2v6h6Z"
)

var (
	parsedPaths = map[string]*ui.Path{
		PathFolder:         ui.MustParsePath(PathFolder),
		PathFile:           ui.MustParsePath(PathFile),
		PathFileText:       ui.MustParsePath(PathFileText),
		PathImage:          ui.MustParsePath(PathImage),
		PathCopy:           ui.MustParsePath(PathCopy),
		PathClipboard:      ui.MustParsePath(PathClipboard),
		PathPencil:         ui.MustParsePath(PathPencil),
		PathRename:         ui.MustParsePath(PathRename),
		PathTrash:          ui.MustParsePath(PathTrash),
		PathRefresh:        ui.MustParsePath(PathRefresh),
		PathFolderPlus:     ui.MustParsePath(PathFolderPlus),
		PathChevronLeft:    ui.MustParsePath(PathChevronLeft),
		PathChevronRight:   ui.MustParsePath(PathChevronRight),
		PathArrowUp:        ui.MustParsePath(PathArrowUp),
		PathSearch:         ui.MustParsePath(PathSearch),
		PathList:           ui.MustParsePath(PathList),
		PathLayoutGrid:     ui.MustParsePath(PathLayoutGrid),
		PathPanelRight:     ui.MustParsePath(PathPanelRight),
		PathPanelLeftClose: ui.MustParsePath(PathPanelLeftClose),
		PathPanelLeftOpen:  ui.MustParsePath(PathPanelLeftOpen),
		PathSettings:       ui.MustParsePath(PathSettings),
		PathSun:            ui.MustParsePath(PathSun),
		PathMoon:           ui.MustParsePath(PathMoon),
		PathServer:         ui.MustParsePath(PathServer),
		PathLock:           ui.MustParsePath(PathLock),
		PathPlus:           ui.MustParsePath(PathPlus),
		PathX:              ui.MustParsePath(PathX),
		PathMenu:           ui.MustParsePath(PathMenu),
		PathEllipsis:       ui.MustParsePath(PathEllipsis),
		PathExternalLink:   ui.MustParsePath(PathExternalLink),
		PathApple:          ui.MustParsePath(PathApple),
		PathWindows:        ui.MustParsePath(PathWindows),
		PathTux:            ui.MustParsePath(PathTux).FillRule(ui.EvenOdd),
		PathAndroid:        ui.MustParsePath(PathAndroid).FillRule(ui.EvenOdd),
		PathIPhone:         ui.MustParsePath(PathIPhone).FillRule(ui.EvenOdd),
		PathPage:           ui.MustParsePath(PathPage),
		PathFold:           ui.MustParsePath(PathFold),
	}
	squircleLogoPath = roundedRectPath(0, 0, 64, 64, 14.5)
)

func roundedRectPath(x, y, w, h, r float32) *ui.Path {
	var p ui.Path
	// Use cubic approximations via arc/quad or simple polygon? Build with QuadraticTo corners (k = 0.45 approximation or exact circle/rect?).
	// Actually k ≈ 0.55228475 cubic Bezier gives exact circular arcs:
	const k float32 = 0.55228475
	kr := r * k
	p.MoveTo(x+r, y)
	p.LineTo(x+w-r, y)
	p.CubeTo(x+w-r+kr, y, x+w, y+r-kr, x+w, y+r)
	p.LineTo(x+w, y+h-r)
	p.CubeTo(x+w, y+h-r+kr, x+w-r+kr, y+h, x+w-r, y+h)
	p.LineTo(x+r, y+h)
	p.CubeTo(x+r-kr, y+h, x, y+h-r+kr, x, y+h-r)
	p.LineTo(x, y+r)
	p.CubeTo(x, y+r-kr, x+r-kr, y, x+r, y)
	p.Close()
	return &p
}

// OsKind identifies a device OS silhouette family.
type OsKind uint8

const (
	OsOther OsKind = iota
	OsMac
	OsIOS
	OsWindows
	OsLinux
	OsAndroid
)

func ClassifyOS(osStr string) OsKind {
	o := strings.ToLower(strings.TrimSpace(osStr))
	switch {
	case strings.Contains(o, "android"):
		return OsAndroid
	case strings.Contains(o, "ios") || strings.Contains(o, "iphone") || strings.Contains(o, "ipad"):
		return OsIOS
	case strings.Contains(o, "mac") || strings.Contains(o, "darwin") || strings.Contains(o, "osx"):
		return OsMac
	case strings.Contains(o, "win"):
		return OsWindows
	case strings.Contains(o, "linux") || strings.Contains(o, "unix") || strings.Contains(o, "bsd"):
		return OsLinux
	default:
		return OsOther
	}
}

func OSLabel(osStr string) string {
	switch ClassifyOS(osStr) {
	case OsMac:
		return "macOS"
	case OsIOS:
		return "iOS"
	case OsWindows:
		return "Windows"
	case OsLinux:
		return "Linux"
	case OsAndroid:
		return "Android"
	default:
		return ""
	}
}

// ResolveNodeOS infers known demo/loopback device OS families when empty or "loopback".
func ResolveNodeOS(osStr, id, name string) string {
	if ClassifyOS(osStr) != OsOther {
		return osStr
	}
	for _, cand := range []string{name, id} {
		switch strings.ToLower(strings.TrimSpace(cand)) {
		case "alpha":
			return "macos"
		case "beta":
			return "windows"
		case "gamma":
			return "linux"
		case "pixel":
			return "android"
		case "phone":
			return "ios"
		}
	}
	return osStr
}

// FileKind classifies directory entries into 8 visual badge types.
type FileKind uint8

const (
	FileFolder FileKind = iota
	FileText
	FileMarkdown
	FileImage
	FilePdf
	FileSheet
	FileCode
	FileOther
)

func ClassifyFile(name string, isDir bool) FileKind {
	if isDir {
		return FileFolder
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	switch ext {
	case "md", "markdown":
		return FileMarkdown
	case "txt", "log", "csv", "rtf":
		return FileText
	case "png", "jpg", "jpeg", "gif", "webp", "svg", "bmp":
		return FileImage
	case "pdf":
		return FilePdf
	case "xls", "xlsx", "ods":
		return FileSheet
	case "rs", "go", "py", "js", "ts", "tsx", "json", "toml", "yaml", "yml", "c", "cpp", "h":
		return FileCode
	default:
		return FileOther
	}
}

func TypeLabel(k FileKind) string {
	switch k {
	case FileFolder:
		return "文件夹"
	case FileText:
		return "文本文档"
	case FileMarkdown:
		return "Markdown"
	case FileImage:
		return "图片"
	case FilePdf:
		return "PDF 文档"
	case FileSheet:
		return "电子表格"
	case FileCode:
		return "代码"
	default:
		return "文件"
	}
}

// StrokeIcon draws a 24x24 Lucide stroked path icon at size DIPs in color.
func StrokeIcon(c *ui.Context, d string, size float32, color ui.Color) ui.Element {
	path := parsedPaths[d]
	if path == nil {
		path = ui.MustParsePath(d)
	}
	return ui.Box(c).Size(size, size).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		p.Transform(ui.Scaling(r.W/24, r.H/24).Then(ui.Translation(r.X, r.Y)), 0, 0, func() {
			p.StrokePath(path, 1.75, color)
		})
	})
}

// OSIcon draws the monochrome OS glyph in color.
func OSIcon(c *ui.Context, osStr string, size float32, color ui.Color) ui.Element {
	kind := ClassifyOS(osStr)
	if kind == OsOther {
		return StrokeIcon(c, PathServer, size, color)
	}
	var d string
	switch kind {
	case OsMac:
		d = PathApple
	case OsIOS:
		d = PathIPhone
	case OsWindows:
		d = PathWindows
	case OsLinux:
		d = PathTux
	case OsAndroid:
		d = PathAndroid
	}
	path := parsedPaths[d]
	return ui.Box(c).Size(size, size).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		p.Transform(ui.Scaling(r.W/24, r.H/24).Then(ui.Translation(r.X, r.Y)), 0, 0, func() {
			p.FillPath(path, color)
		})
	})
}

// TypedFileIcon draws a folder or colored badge tile matching Explorer/Finder glyphs.
func TypedFileIcon(c *ui.Context, kind FileKind, size float32) ui.Element {
	return ui.Box(c).Size(size, size).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		p.Transform(ui.Scaling(r.W/24, r.H/24).Then(ui.Translation(r.X, r.Y)), 0, 0, func() {
			if kind == FileFolder {
				p.FillPath(parsedPaths[PathFolder], ui.Hex("#4f8df7"))
				p.StrokePath(parsedPaths[PathFolder], 1.15, ui.Hex("#1d4ed8"))
				return
			}
			var fillHex, inkHex string
			switch kind {
			case FilePdf:
				fillHex, inkHex = "#ffe4e6", "#dc2626"
			case FileSheet:
				fillHex, inkHex = "#dcfce7", "#15803d"
			case FileImage:
				fillHex, inkHex = "#cffafe", "#0e7490"
			case FileText:
				fillHex, inkHex = "#e0e7ff", "#3730a3"
			case FileMarkdown:
				fillHex, inkHex = "#dbeafe", "#1d4ed8"
			case FileCode:
				fillHex, inkHex = "#ede9fe", "#6d28d9"
			default:
				fillHex, inkHex = "#f3f4f6", "#4b5563"
			}
			ink := ui.Hex(inkHex)
			p.FillPath(parsedPaths[PathPage], ui.Hex(fillHex))
			p.FillPath(parsedPaths[PathFold], ink)
			p.StrokePath(parsedPaths[PathPage], 1.35, ink)
			switch kind {
			case FileSheet:
				grid := (&ui.Path{}).MoveTo(7, 13).LineTo(17, 13).MoveTo(7, 16.5).LineTo(17, 16.5).MoveTo(11, 11).LineTo(11, 19)
				p.StrokePath(grid, 1.3, ink)
			case FileImage:
				sun := (&ui.Path{}).Circle(9, 13, 1.4)
				p.FillPath(sun, ink)
				mtn := (&ui.Path{}).MoveTo(6.5, 19).LineTo(10.5, 15).LineTo(13, 17.2).LineTo(17.5, 13.2)
				p.StrokePath(mtn, 1.35, ink)
			case FilePdf:
				line := (&ui.Path{}).MoveTo(7.5, 17.2).LineTo(16.5, 17.2)
				p.StrokePath(line, 1.6, ink)
			default:
				lines := (&ui.Path{}).MoveTo(7.5, 13).LineTo(16, 13).MoveTo(7.5, 16.4).LineTo(13.2, 16.4)
				p.StrokePath(lines, 1.35, ink)
			}
		})
	})
}

// LogoMark paints the Tailfile orange squircle + 9 warm-white dots brand mark.
func LogoMark(c *ui.Context, size float32) ui.Element {
	return ui.Box(c).Size(size, size).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		p.Transform(ui.Scaling(r.W/64, r.H/64).Then(ui.Translation(r.X, r.Y)), 0, 0, func() {
			p.ClipPath(squircleLogoPath, func() {
				p.FillGradient(ui.Rect{W: 64, H: 64}, ui.LinearGradient{
					From:  ui.Hex("#fb923c"),
					To:    ui.Hex("#e2510b"),
					Angle: 135,
				}, 0)
				p.FillRadialGradient(ui.Rect{W: 64, H: 64}, ui.RadialGradient{
					Center: ui.Point{X: 0.3, Y: 0.2},
					Radius: 57.6,
					Stops: []ui.GradientStop{
						{Color: ui.RGBA(255, 255, 255, 0.18), At: 0},
						{Color: ui.RGBA(255, 255, 255, 0), At: 0.6},
					},
				}, 0)
			})
			ink := ui.Hex("#fff8f1")
			halfInk := ui.RGBA(0xff, 0xf8, 0xf1, 0.5)
			type dot struct{ cx, cy, r float32 }
			dots := []dot{
				{20.5, 9, 4.4}, {32, 9, 4.4}, {43.5, 9, 4.4},
				{20.5, 20.5, 4.4},
				{20.5, 32, 4.4}, {32, 32, 4.4}, {43.5, 32, 2.64},
				{20.5, 43.5, 4.4},
				{20.5, 55, 4.4},
			}
			for i, d := range dots {
				cx := 32 + 0.74*(d.cx-32)
				cy := 32 + 0.74*(d.cy-32)
				col := ink
				if i == 6 {
					col = halfInk
				}
				p.FillPath((&ui.Path{}).Circle(cx, cy, d.r*0.74), col)
			}
		})
	})
}

// StatusDot paints a 7x7 node availability/read-only indicator.
func StatusDot(c *ui.Context, online, readOnly bool) ui.Element {
	col := ui.Hex("#12a150")
	if !online {
		col = ui.Hex("#c0c5cd")
	} else if readOnly {
		col = ui.Hex("#f59e0b")
	}
	return ui.Box(c).Size(7, 7).Shrink(0).Radius(3.5).Background(col)
}
