package gui

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"tailfile.local/internal/agent"
)

// LineEnding records the original newline convention of an opened file so saves preserve it.
type LineEnding uint8

const (
	LineEndingLF LineEnding = iota
	LineEndingCRLF
)

func (e LineEnding) Label() string {
	if e == LineEndingCRLF {
		return "CRLF"
	}
	return "LF"
}

// DocEditorPhase models the mutually exclusive states of the in-app text editor modal.
type DocEditorPhase uint8

const (
	DocClosed DocEditorPhase = iota
	DocLoading
	DocReady
	DocSaving
	DocConflict
	DocConfirmClose
	DocError
)

// TextMatchRange stores 0-based rune offsets matching ui.TextBuffer coordinates.
type TextMatchRange struct {
	Start int
	End   int
}

// DocEditorState holds the isolated document buffer and lifecycle for ModalDocEdit.
// Kept separate from Session.Editor so folder navigation never discards unsaved text.
type DocEditorState struct {
	Phase       DocEditorPhase
	Gen         uint64
	Node        string
	Path        string
	Name        string
	ReadOnly    bool
	LineEnding  LineEnding
	HasBOM      bool
	OrigSize    int64
	OrigModTime time.Time
	OrigText    string
	SavedVer    uint64
	Buffer      *ui.TextBuffer

	Err            string
	ConflictMsg    string
	ConflictCur    *agent.Entry
	StatusNote     string
	CloseAfterSave bool

	FindOpen    bool
	FindQuery   string
	FindMatches []TextMatchRange
	FindCurrent int

	dirtyVer    uint64
	dirtyCached bool
	findSeeded  bool
	findVer     uint64
	findLastQ   string
}

// Dirty reports whether the current buffer differs from OrigText.
func (d *DocEditorState) Dirty() bool {
	if d.Buffer == nil {
		return false
	}
	ver := d.Buffer.Version()
	if ver == d.SavedVer {
		return false
	}
	if ver == d.dirtyVer {
		return d.dirtyCached
	}
	d.dirtyVer = ver
	d.dirtyCached = d.Buffer.String() != d.OrigText
	return d.dirtyCached
}

// SyncFindMatches refreshes FindMatches when FindQuery or Buffer version changed.
func (d *DocEditorState) SyncFindMatches() {
	if d.Buffer == nil || !d.FindOpen {
		d.FindMatches = nil
		d.FindCurrent = 0
		d.findSeeded = false
		return
	}
	ver := d.Buffer.Version()
	if !d.findSeeded || ver != d.findVer || d.FindQuery != d.findLastQ {
		d.FindMatches = FindTextMatches(d.Buffer.String(), d.FindQuery)
		d.findSeeded = true
		d.findVer = ver
		d.findLastQ = d.FindQuery
	}
	if len(d.FindMatches) == 0 || d.FindCurrent < 0 || d.FindCurrent >= len(d.FindMatches) {
		d.FindCurrent = 0
	}
}

// DecodeEditableText validates raw bytes as NUL-free UTF-8 and normalizes newlines to LF.
func DecodeEditableText(raw []byte) (normalized string, eol LineEnding, hasBOM bool, err error) {
	if bytes.IndexByte(raw, 0) >= 0 {
		return "", LineEndingLF, false, errors.New("不支持直接编辑二进制文件")
	}
	if bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}) {
		hasBOM = true
		raw = raw[3:]
	}
	if !utf8.Valid(raw) {
		return "", LineEndingLF, false, errors.New("文件不是有效的 UTF-8 文本")
	}
	eol = LineEndingLF
	if bytes.Contains(raw, []byte("\r\n")) {
		eol = LineEndingCRLF
	}
	s := strings.ReplaceAll(string(raw), "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s, eol, hasBOM, nil
}

// EncodeEditableText restores original line endings and UTF-8 BOM before saving.
func EncodeEditableText(normalized string, eol LineEnding, hasBOM bool) []byte {
	s := strings.ReplaceAll(normalized, "\r\n", "\n")
	if eol == LineEndingCRLF {
		s = strings.ReplaceAll(s, "\n", "\r\n")
	}
	if !hasBOM {
		return []byte(s)
	}
	out := make([]byte, 3+len(s))
	out[0], out[1], out[2] = 0xEF, 0xBB, 0xBF
	copy(out[3:], s)
	return out
}

var nonEditableExts = map[string]bool{
	"zip": true, "tar": true, "gz": true, "tgz": true, "bz2": true, "xz": true,
	"7z": true, "rar": true, "mp3": true, "wav": true, "flac": true, "ogg": true,
	"m4a": true, "mp4": true, "mov": true, "mkv": true, "avi": true, "webm": true,
	"exe": true, "dll": true, "so": true, "dylib": true, "bin": true, "dmg": true,
	"iso": true, "apk": true, "ipa": true, "wasm": true, "o": true, "a": true,
	"pyc": true, "class": true, "ttf": true, "otf": true, "woff": true, "woff2": true,
	"ico": true, "icns": true, "sqlite": true, "db": true,
}

// IsLikelyEditableName excludes obvious binary, media, archive, and image formats.
func IsLikelyEditableName(name string) bool {
	if name == "" || strings.HasSuffix(name, ".tailfile-partial") || strings.HasSuffix(name, ".tailfile-meta") {
		return false
	}
	switch ClassifyFile(name, false) {
	case FileImage, FilePdf, FileSheet:
		return false
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	return !nonEditableExts[ext]
}

// FindTextMatches returns non-overlapping case-insensitive rune ranges in text.
func FindTextMatches(text, query string) []TextMatchRange {
	if text == "" || query == "" {
		return nil
	}
	qRunes := make([]rune, 0, len(query))
	for _, r := range query {
		qRunes = append(qRunes, unicode.ToLower(r))
	}
	tRunes := make([]rune, 0, len(text))
	for _, r := range text {
		tRunes = append(tRunes, unicode.ToLower(r))
	}
	qLen := len(qRunes)
	if qLen == 0 || len(tRunes) < qLen {
		return nil
	}
	var out []TextMatchRange
	for i := 0; i <= len(tRunes)-qLen; {
		matched := true
		for j := 0; j < qLen; j++ {
			if tRunes[i+j] != qRunes[j] {
				matched = false
				break
			}
		}
		if matched {
			out = append(out, TextMatchRange{Start: i, End: i + qLen})
			i += qLen
		} else {
			i++
		}
	}
	return out
}

// OpenDocEditor opens the built-in text editor (or read-only viewer) for the selected file.
func (s *Session) OpenDocEditor() bool {
	tab := s.Active()
	sel := s.SelectedEntries()
	if len(sel) != 1 || sel[0].IsDir || !IsLikelyEditableName(sel[0].Name) {
		return false
	}
	e := sel[0]
	return s.openDocTarget(tab.Node, e.Path, e.Name, e.Size, e.ModTime)
}

// ReloadDocEditor reloads the currently open file from the agent, discarding local edits.
func (s *Session) ReloadDocEditor() bool {
	d := &s.DocEditor
	if s.Modal != ModalDocEdit || d.Node == "" || d.Path == "" {
		return false
	}
	return s.openDocTarget(d.Node, d.Path, d.Name, d.OrigSize, d.OrigModTime)
}

func (s *Session) openDocTarget(nodeID, filePath, name string, fallbackSize int64, fallbackModTime time.Time) bool {
	if nodeID == "" || filePath == "" {
		return false
	}
	s.ctxMenuOpen = false
	s.mobileDetailsOpen = false
	readOnly := !s.CanWritePath(nodeID, filePath)
	gen := s.DocEditor.Gen + 1
	s.DocEditor = DocEditorState{
		Phase:       DocLoading,
		Gen:         gen,
		Node:        nodeID,
		Path:        filePath,
		Name:        name,
		ReadOnly:    readOnly,
		OrigSize:    fallbackSize,
		OrigModTime: fallbackModTime,
	}
	s.Modal = ModalDocEdit
	s.docModalOpen = true

	if s.Client == nil {
		buf := ui.NewTextBuffer("")
		ver := buf.Version()
		s.DocEditor.Buffer = buf
		s.DocEditor.SavedVer = ver
		s.DocEditor.dirtyVer = ver
		s.DocEditor.Phase = DocReady
		return true
	}

	s.launchAsync(func() func() {
		st, raw, readErr := s.Client.ReadFile(s.ctx, nodeID, filePath, agent.MaxEditableFileBytes)
		var normalized string
		var eol LineEnding
		var hasBOM bool
		if readErr == nil {
			normalized, eol, hasBOM, readErr = DecodeEditableText(raw)
		}
		return func() {
			if s.Modal != ModalDocEdit || s.DocEditor.Gen != gen {
				return
			}
			d := &s.DocEditor
			if readErr != nil {
				d.Phase = DocError
				d.Err = friendlyDocError(readErr)
				return
			}
			buf := ui.NewTextBuffer(normalized)
			ver := buf.Version()
			d.Buffer = buf
			d.OrigText = normalized
			d.SavedVer = ver
			d.dirtyVer = ver
			d.dirtyCached = false
			d.LineEnding = eol
			d.HasBOM = hasBOM
			d.OrigSize = st.Size
			d.OrigModTime = st.ModTime
			d.Err = ""
			d.Phase = DocReady
		}
	})
	return true
}

// SaveDocEditor writes the current buffer back to the target node.
func (s *Session) SaveDocEditor(force bool) bool {
	if s.Modal != ModalDocEdit {
		return false
	}
	d := &s.DocEditor
	if d.ReadOnly || !s.CanWritePath(d.Node, d.Path) || d.Buffer == nil {
		return false
	}
	allowedPhase := d.Phase == DocReady || d.Phase == DocConfirmClose || (force && d.Phase == DocConflict)
	if !allowedPhase {
		return false
	}

	textSnap := d.Buffer.String()
	verSnap := d.Buffer.Version()
	payload := EncodeEditableText(textSnap, d.LineEnding, d.HasBOM)

	if s.Client == nil {
		d.OrigText = textSnap
		d.SavedVer = verSnap
		d.dirtyVer = verSnap
		d.dirtyCached = false
		d.OrigSize = int64(len(payload))
		d.Err = ""
		d.ConflictMsg = ""
		d.ConflictCur = nil
		d.StatusNote = "已保存"
		d.Phase = DocReady
		s.invalidateFileCaches(d.Node, d.Path)
		if d.CloseAfterSave {
			s.CloseDocEditorForce()
		}
		return true
	}

	gen := d.Gen
	node := d.Node
	filePath := d.Path
	var expectedMtime int64
	if !d.OrigModTime.IsZero() {
		expectedMtime = d.OrigModTime.UnixNano()
	}
	req := agent.WriteFileRequest{
		Node:              node,
		Path:              filePath,
		Data:              payload,
		ExpectedMtimeUnix: expectedMtime,
		ExpectedSize:      d.OrigSize,
		Force:             force,
	}
	d.Phase = DocSaving
	d.Err = ""
	d.ConflictMsg = ""
	d.ConflictCur = nil
	d.StatusNote = ""

	s.launchAsync(func() func() {
		updated, err := s.Client.WriteFile(s.ctx, req)
		return func() {
			if s.Modal != ModalDocEdit || s.DocEditor.Gen != gen {
				return
			}
			cur := &s.DocEditor
			var ce *agent.WriteConflictError
			if errors.As(err, &ce) {
				cur.Phase = DocConflict
				cur.ConflictMsg = ce.Error()
				curEnt := ce.Current
				cur.ConflictCur = &curEnt
				cur.CloseAfterSave = false
				return
			}
			if err != nil {
				cur.Phase = DocReady
				cur.Err = "保存失败: " + err.Error()
				cur.CloseAfterSave = false
				return
			}
			cur.OrigText = textSnap
			cur.SavedVer = verSnap
			cur.dirtyVer = verSnap
			cur.dirtyCached = false
			cur.OrigSize = updated.Size
			cur.OrigModTime = updated.ModTime
			cur.Phase = DocReady
			cur.Err = ""
			cur.StatusNote = "已保存"
			s.SetStatus("已保存 "+cur.Name, false)
			s.invalidateFileCaches(node, filePath)
			s.loadActiveListing()
			s.syncPreviewWithSelection()
			if cur.CloseAfterSave {
				s.CloseDocEditorForce()
			}
		}
	})
	return true
}

func (s *Session) invalidateFileCaches(nodeID, filePath string) {
	canon := CanonicalizeRemote(filePath)
	for _, t := range s.Tabs {
		delete(t.CachedPaths, filePath)
		for k := range t.CachedPaths {
			if CanonicalizeRemote(k) == canon {
				delete(t.CachedPaths, k)
			}
		}
		if t.Preview != nil && t.Preview.Node == nodeID && CanonicalizeRemote(t.Preview.Path) == canon {
			t.Preview = nil
			t.PreviewGen++
		}
	}
}

// RequestCloseDocEditor closes the editor if clean, or prompts before discarding edits.
func (s *Session) RequestCloseDocEditor() {
	if s.Modal != ModalDocEdit {
		return
	}
	if s.DocEditor.Phase == DocSaving {
		s.docModalOpen = true
		return
	}
	if s.DocEditor.Dirty() {
		s.DocEditor.Phase = DocConfirmClose
		s.docModalOpen = true
		return
	}
	s.CloseDocEditorForce()
}

// DismissDocSubPrompt returns from a conflict or discard prompt back to editing.
func (s *Session) DismissDocSubPrompt() {
	if s.Modal != ModalDocEdit {
		return
	}
	if s.DocEditor.Phase == DocConfirmClose || s.DocEditor.Phase == DocConflict {
		s.DocEditor.Phase = DocReady
		s.DocEditor.CloseAfterSave = false
		s.DocEditor.ConflictMsg = ""
		s.DocEditor.ConflictCur = nil
	}
	s.docModalOpen = true
}

// CloseDocEditorForce closes ModalDocEdit unconditionally and bumps Gen to drop inflight callbacks.
func (s *Session) CloseDocEditorForce() {
	gen := s.DocEditor.Gen + 1
	s.DocEditor = DocEditorState{Gen: gen}
	if s.Modal == ModalDocEdit {
		s.Modal = ModalNone
	}
	s.docModalOpen = false
}

func friendlyDocError(err error) string {
	var se *agent.StatusError
	if errors.As(err, &se) && se.Code == http.StatusRequestEntityTooLarge {
		return fmt.Sprintf("文件超过内置编辑器 %s 上限，请下载后在本地编辑", FormatSize(agent.MaxEditableFileBytes))
	}
	return err.Error()
}
