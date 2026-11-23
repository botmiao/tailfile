package gui

import (
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"tailfile.local/internal/agent"
)

func isASCIIAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func hasWindowsDrive(p string) bool {
	return len(p) >= 2 && isASCIIAlpha(p[0]) && p[1] == ':'
}

// splitUNCRoot extracts a UNC prefix ("//server/share") and the rest of the path
// when p starts with exactly two slashes followed by non-empty server and share.
func splitUNCRoot(p string) (root, rest string, ok bool) {
	if !strings.HasPrefix(p, "//") || strings.HasPrefix(p, "///") {
		return "", "", false
	}
	trimmed := strings.TrimPrefix(p, "//")
	parts := strings.Split(trimmed, "/")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			clean = append(clean, part)
		}
	}
	if len(clean) < 2 || clean[0] == "." || clean[0] == ".." || clean[1] == "." || clean[1] == ".." {
		return "", "", false
	}
	root = "//" + clean[0] + "/" + clean[1]
	rest = strings.Join(clean[2:], "/")
	return root, rest, true
}

// NormRemote normalizes slashes and trims trailing separators while preserving
// "/", "X:/", and "//server/share" roots.
func NormRemote(path string) string {
	p := strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	if p == "" {
		return ""
	}
	if uncRoot, rest, ok := splitUNCRoot(p); ok {
		if rest == "" {
			return uncRoot
		}
		return uncRoot + "/" + rest
	}
	if hasWindowsDrive(p) {
		drive := strings.ToUpper(p[:1]) + ":"
		tail := p[2:]
		parts := strings.Split(tail, "/")
		clean := make([]string, 0, len(parts))
		for _, part := range parts {
			if part != "" {
				clean = append(clean, part)
			}
		}
		if len(clean) == 0 {
			return drive + "/"
		}
		return drive + "/" + strings.Join(clean, "/")
	}
	abs := strings.HasPrefix(p, "/")
	parts := strings.Split(p, "/")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			clean = append(clean, part)
		}
	}
	if abs {
		if len(clean) == 0 {
			return "/"
		}
		return "/" + strings.Join(clean, "/")
	}
	return strings.Join(clean, "/")
}

// CanonicalizeRemote resolves ".", "..", backslashes, and repeated slashes
// purely lexically without touching the host filesystem, clamping ".." at the
// path's root anchor ("/", "X:/", or "//server/share").
func CanonicalizeRemote(path string) string {
	p := strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	if p == "" {
		return ""
	}

	var anchor string
	var tail string

	if uncRoot, rest, ok := splitUNCRoot(p); ok {
		anchor = uncRoot
		tail = rest
	} else if hasWindowsDrive(p) {
		anchor = strings.ToUpper(p[:1]) + ":/"
		tail = p[2:]
	} else if strings.HasPrefix(p, "/") {
		anchor = "/"
		tail = p
	} else {
		tail = p
	}

	var stack []string
	for _, seg := range strings.Split(tail, "/") {
		switch seg {
		case "", ".":
			continue
		case "..":
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		default:
			stack = append(stack, seg)
		}
	}

	if len(stack) == 0 {
		return anchor
	}
	if anchor == "" {
		return strings.Join(stack, "/")
	}
	if strings.HasSuffix(anchor, "/") {
		return anchor + strings.Join(stack, "/")
	}
	return anchor + "/" + strings.Join(stack, "/")
}

func remoteCompareForm(canon string) string {
	if _, _, ok := splitUNCRoot(canon); ok {
		// Server and share names on SMB/UNC are case-insensitive.
		root, rest, _ := splitUNCRoot(canon)
		if rest == "" {
			return strings.ToLower(root)
		}
		return strings.ToLower(root) + "/" + rest
	}
	if hasWindowsDrive(canon) {
		return strings.ToLower(canon[:2]) + canon[2:]
	}
	return canon
}

func isAbsRemote(canon string) bool {
	if canon == "" {
		return false
	}
	if strings.HasPrefix(canon, "/") || hasWindowsDrive(canon) {
		return true
	}
	return false
}

// PathIsUnder reports whether path lies inside root (or equals root) on a clean
// segment boundary after lexical canonicalization.
func PathIsUnder(path, root string) bool {
	cp := CanonicalizeRemote(path)
	cr := CanonicalizeRemote(root)
	if !isAbsRemote(cp) || !isAbsRemote(cr) {
		return false
	}
	cpCmp := remoteCompareForm(cp)
	crCmp := remoteCompareForm(cr)
	if cpCmp == crCmp {
		return true
	}
	prefix := crCmp
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return strings.HasPrefix(cpCmp, prefix)
}

// PathIsUnderAny reports whether path lies under any root in roots.
func PathIsUnderAny(path string, roots []string) bool {
	for _, root := range roots {
		if PathIsUnder(path, root) {
			return true
		}
	}
	return false
}

// JoinRemote joins a remote directory and entry name with forward slashes.
func JoinRemote(dir, name string) string {
	return agent.JoinRemote(dir, name)
}

// BaseRemote returns the final segment of path (or root itself for "/", "X:/", UNC).
func BaseRemote(path string) string {
	n := NormRemote(path)
	if n == "" || n == "/" {
		return n
	}
	if hasWindowsDrive(n) && len(n) == 3 && strings.HasSuffix(n, ":/") {
		return n
	}
	if uncRoot, rest, ok := splitUNCRoot(n); ok && rest == "" {
		return uncRoot
	}
	return agent.BaseRemote(n)
}

// ParentRemote returns the parent directory of path clamped to readRoots.
func ParentRemote(path string, readRoots []string) (string, bool) {
	cur := CanonicalizeRemote(path)
	if !isAbsRemote(cur) {
		return "", false
	}
	curCmp := remoteCompareForm(cur)
	for _, r := range readRoots {
		if cr := CanonicalizeRemote(r); cr != "" && remoteCompareForm(cr) == curCmp {
			return "", false
		}
	}
	if cur == "/" {
		return "", false
	}
	if hasWindowsDrive(cur) && len(cur) == 3 && strings.HasSuffix(cur, ":/") {
		return "", false
	}
	if _, rest, ok := splitUNCRoot(cur); ok && rest == "" {
		return "", false
	}

	idx := strings.LastIndex(cur, "/")
	if idx < 0 {
		return "", false
	}
	var parent string
	switch {
	case idx == 0:
		parent = "/"
	case hasWindowsDrive(cur) && idx == 2:
		parent = cur[:3]
	default:
		parent = cur[:idx]
	}
	if len(readRoots) > 0 && !PathIsUnderAny(parent, readRoots) {
		return "", false
	}
	return parent, true
}

// Crumb is one clickable breadcrumb segment.
type Crumb struct {
	Label string
	Path  string
}

// BuildCrumbs builds breadcrumbs from the deepest matching shared root down to path.
func BuildCrumbs(path string, readRoots []string) []Crumb {
	cp := CanonicalizeRemote(path)
	if cp == "" {
		return nil
	}
	var matchedRoot string
	for _, r := range readRoots {
		cr := CanonicalizeRemote(r)
		if cr != "" && PathIsUnder(cp, cr) && len(cr) > len(matchedRoot) {
			matchedRoot = cr
		}
	}
	if matchedRoot == "" {
		return []Crumb{{Label: BaseRemote(cp), Path: cp}}
	}
	crumbs := []Crumb{{Label: BaseRemote(matchedRoot), Path: matchedRoot}}
	if remoteCompareForm(cp) == remoteCompareForm(matchedRoot) {
		return crumbs
	}
	prefix := matchedRoot
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	if len(cp) <= len(prefix) {
		return crumbs
	}
	rel := cp[len(prefix):]
	cur := matchedRoot
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" {
			continue
		}
		cur = JoinRemote(cur, seg)
		crumbs = append(crumbs, Crumb{Label: seg, Path: cur})
	}
	return crumbs
}

// FileURIToPath converts a single line from text/uri-list into a local OS path.
// Non-file schemes and malformed URIs return "".
func FileURIToPath(line string) string {
	s := strings.TrimSpace(line)
	if s == "" || strings.HasPrefix(s, "#") {
		return ""
	}
	lower := strings.ToLower(s)
	if !strings.HasPrefix(lower, "file://") {
		if strings.Contains(s, "://") {
			return ""
		}
		return filepath.Clean(s)
	}
	u, err := url.Parse(s)
	if err != nil || !strings.EqualFold(u.Scheme, "file") {
		return ""
	}
	decodedPath, err := url.PathUnescape(u.Path)
	if err != nil {
		decodedPath = u.Path
	}
	if u.Host != "" && !strings.EqualFold(u.Host, "localhost") {
		// UNC file://server/share/path -> \\server\share\path on Windows, //server/share/path on Unix
		unc := "//" + u.Host + decodedPath
		if runtime.GOOS == "windows" {
			return filepath.FromSlash(unc)
		}
		return unc
	}
	// Windows drive URI: file:///C:/Users/Ada -> C:/Users/Ada
	if len(decodedPath) >= 3 && decodedPath[0] == '/' && isASCIIAlpha(decodedPath[1]) && decodedPath[2] == ':' {
		drivePath := strings.ToUpper(decodedPath[1:2]) + decodedPath[2:]
		if runtime.GOOS == "windows" {
			return filepath.FromSlash(drivePath)
		}
		return strings.ReplaceAll(drivePath, "\\", "/")
	}
	if runtime.GOOS == "windows" {
		return filepath.FromSlash(decodedPath)
	}
	return decodedPath
}

// FormatSize formats byte counts cleanly for list rows and details panes.
func FormatSize(n int64) string {
	if n < 0 {
		return "--"
	}
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	val := float64(n) / 1024.0
	idx := 0
	for val >= 1024.0 && idx < len(units)-1 {
		val /= 1024.0
		idx++
	}
	if val >= 100 {
		return fmt.Sprintf("%.0f %s", val, units[idx])
	}
	return strings.TrimSuffix(fmt.Sprintf("%.1f", val), ".0") + " " + units[idx]
}

// FormatTime formats modification timestamps for display.
func FormatTime(t time.Time) string {
	if t.IsZero() {
		return "--"
	}
	return t.Local().Format("2006-01-02 15:04")
}
