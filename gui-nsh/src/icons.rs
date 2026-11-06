//! Logo mark and Lucide-compatible SVG path icons drawn via nsh `canvas`.
//! Paths are ISC/MIT Lucide (24×24 viewBox); painted with stroke (or fill for logo dots).

use nsh_core::SvgPaint;
use nsh_core::canvas::{LineCap, LineJoin};
use nsh_core::prelude::*;

pub const LOGO_LABEL: &str = "F";
pub const LOGO_TITLE: &str = "Tailfile";

pub const THIRD_PARTY_NOTICES: &str = r#"Lucide Icons — ISC License
Copyright (c) 2026 Lucide Icons and Contributors

Feather-derived Lucide icons — MIT License
Copyright (c) 2013-present Cole Bemis

Full texts: see docs/NSH_UI.md and the approved UX prototype README.
"#;

/// Lucide path data (24×24).
pub mod path {
    pub const FOLDER: &str = "M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z";
    pub const FILE: &str =
        "M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7ZM14 2v4a2 2 0 0 0 2 2h4";
    pub const FILE_TEXT: &str = "M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7ZM14 2v4a2 2 0 0 0 2 2h4M10 9H8M16 13H8M16 17H8";
    pub const IMAGE: &str = "M21 15V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-4M21 15l-5-5L5 21M8.5 10a1.5 1.5 0 1 0 0-3 1.5 1.5 0 0 0 0 3";
    pub const FILE_TYPE: &str =
        "M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7ZM14 2v4a2 2 0 0 0 2 2h4";
    pub const COPY: &str = "M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2M8 8h10c1.1 0 2 .9 2 2v10c0 1.1-.9 2-2 2H8c-1.1 0-2-.9-2-2V10c0-1.1.9-2 2-2";
    pub const CLIPBOARD: &str = "M16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2M9 2h6a1 1 0 0 1 1 1v2a1 1 0 0 1-1 1H9a1 1 0 0 1-1-1V3a1 1 0 0 1 1-1";
    pub const PENCIL: &str = "M21.174 6.812a1 1 0 0 0-3.986-3.987L3.842 16.174a2 2 0 0 0-.5.83l-1.321 4.352a.5.5 0 0 0 .623.622l4.353-1.32a2 2 0 0 0 .83-.497zM15 5l4 4";
    pub const TRASH: &str = "M3 6h18M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2M10 11v6M14 11v6";
    pub const REFRESH: &str = "M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8M21 3v5h-5M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16M8 16H3v5";
    pub const FOLDER_PLUS: &str = "M12 10v6M9 13h6M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z";
    pub const CHEVRON_LEFT: &str = "m15 18-6-6 6-6";
    pub const CHEVRON_RIGHT: &str = "m9 18 6-6-6-6";
    pub const ARROW_UP: &str = "m5 12 7-7 7 7M12 19V5";
    pub const SEARCH: &str = "m21 21-4.34-4.34M11 19a8 8 0 1 0 0-16 8 8 0 0 0 0 16";
    pub const LIST: &str = "M3 12h.01M3 18h.01M3 6h.01M8 12h13M8 18h13M8 6h13";
    pub const LAYOUT_GRID: &str = "M3 3h7v7H3zM14 3h7v7h-7zM14 14h7v7h-7zM3 14h7v7H3z";
    pub const PANEL_RIGHT: &str =
        "M15 3v18M3 5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z";
    pub const SETTINGS: &str = "M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2zM12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6";
    pub const SUN: &str = "M12 8a4 4 0 1 0 0 8 4 4 0 1 0 0-8M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41";
    pub const MOON: &str = "M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z";
    pub const LAPTOP: &str = "M18 10h2a1 1 0 0 1 1 1v1M2 12v-1a1 1 0 0 1 1-1h2M4 20h16M6 8V6a2 2 0 0 1 2-2h8a2 2 0 0 1 2 2v12H6z";
    pub const MONITOR: &str =
        "M20 3H4a2 2 0 0 0-2 2v10a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2V5a2 2 0 0 0-2-2zM8 21h8M12 17v4";
    pub const SERVER: &str = "M4 6a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v2a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2zM4 16a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v2a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2zM6 8h.01M6 18h.01";
    pub const SMARTPHONE: &str =
        "M12 18h.01M7 2h10a2 2 0 0 1 2 2v16a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2z";
    pub const LOCK: &str = "M7 11V7a5 5 0 0 1 10 0v4M5 11h14a2 2 0 0 1 2 2v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-7a2 2 0 0 1 2-2";
    pub const PLUS: &str = "M5 12h14M12 5v14";
    pub const X: &str = "M18 6 6 18M6 6l12 12";
    pub const ELLIPSIS: &str = "M12 12h.01M19 12h.01M5 12h.01";
    /// Lucide `square-terminal`: a Linux terminal window.
    pub const SQUARE_TERMINAL: &str = "M5 3h14a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2zM7 8l3 4-3 4M12 16h5";
    /// Filled document page (folded corner is a separate triangle).
    pub const PAGE: &str = "M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8Z";
    pub const FOLD: &str = "M14 2v6h6Z";
}

/// Stroke icon at `size` pt, inherits `text_color` of the element.
pub fn stroke_icon(d: &'static str, size: f32) -> CanvasView {
    let scale = size / 24.0;
    let lw = 1.75_f32;
    canvas(move || {
        Painter::new()
            .scale(scale, scale)
            .fill_style_from_view(SvgPaint::None)
            .stroke_style_from_view(SvgPaint::CurrentColor)
            .line_width(lw)
            .line_cap(LineCap::Round)
            .line_join(LineJoin::Round)
            .stroke_svg(d)
    })
    .w(px(size))
    .h(px(size))
    .flex_none()
}

/// Filled folder (prototype blue).
pub fn folder_icon(size: f32, filled: bool) -> CanvasView {
    let scale = size / 24.0;
    canvas(move || {
        let mut p = Painter::new().scale(scale, scale);
        if filled {
            p = p
                .fill_style(rgb(0x3b82f6))
                .fill_svg(path::FOLDER)
                .stroke_style(rgb(0x2563eb))
                .line_width(1.25)
                .stroke_svg(path::FOLDER);
        } else {
            p = p
                .fill_style_from_view(SvgPaint::None)
                .stroke_style_from_view(SvgPaint::CurrentColor)
                .line_width(1.75)
                .line_cap(LineCap::Round)
                .line_join(LineJoin::Round)
                .stroke_svg(path::FOLDER);
        }
        p
    })
    .w(px(size))
    .h(px(size))
    .flex_none()
}

/// File-type icon: filled page or folder, colored like Explorer's type tiles.
pub fn typed_file_icon(kind: FileKind, size: f32) -> CanvasView {
    let scale = size / 24.0;
    canvas(move || {
        let mut p = Painter::new().scale(scale, scale);
        if matches!(kind, FileKind::Folder) {
            return p
                .fill_style(rgb(0x4f8df7))
                .fill_svg(path::FOLDER)
                .stroke_style(rgb(0x1d4ed8))
                .line_width(1.15)
                .line_join(LineJoin::Round)
                .stroke_svg(path::FOLDER);
        }
        let (fill, ink) = match kind {
            FileKind::Pdf => (0xffe4e6u32, 0xdc2626u32),
            FileKind::Sheet => (0xdcfce7, 0x15803d),
            FileKind::Image => (0xcffafe, 0x0e7490),
            FileKind::Text => (0xe0e7ff, 0x3730a3),
            FileKind::Markdown => (0xdbeafe, 0x1d4ed8),
            FileKind::Code => (0xede9fe, 0x6d28d9),
            FileKind::Folder | FileKind::Other => (0xf3f4f6, 0x4b5563),
        };
        p = p
            .fill_style(rgb(fill))
            .fill_svg(path::PAGE)
            .fill_style(rgb(ink))
            .fill_svg(path::FOLD)
            .stroke_style(rgb(ink))
            .line_width(1.35)
            .line_cap(LineCap::Round)
            .line_join(LineJoin::Round)
            .stroke_svg(path::PAGE);
        match kind {
            FileKind::Sheet => p
                .begin_path()
                .move_to(7.0, 13.0)
                .line_to(17.0, 13.0)
                .move_to(7.0, 16.5)
                .line_to(17.0, 16.5)
                .move_to(11.0, 11.0)
                .line_to(11.0, 19.0)
                .stroke(),
            FileKind::Image => p
                .begin_path()
                .arc(9.0, 13.0, 1.4, 0.0, std::f32::consts::TAU)
                .fill()
                .begin_path()
                .move_to(6.5, 19.0)
                .line_to(10.5, 15.0)
                .line_to(13.0, 17.2)
                .line_to(17.5, 13.2)
                .stroke(),
            FileKind::Pdf => p
                .line_width(1.6)
                .begin_path()
                .move_to(7.5, 17.2)
                .line_to(16.5, 17.2)
                .stroke(),
            _ => p
                .line_width(1.35)
                .begin_path()
                .move_to(7.5, 13.0)
                .line_to(16.0, 13.0)
                .move_to(7.5, 16.4)
                .line_to(13.2, 16.4)
                .stroke(),
        }
    })
    .w(px(size))
    .h(px(size))
    .flex_none()
}

/// Host OS as a Lucide-style mark in `currentColor` (theme foreground).
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum OsKind {
    Mac,
    Windows,
    Linux,
    Android,
    Other,
}

pub fn os_kind(os: &str) -> OsKind {
    let o = os.trim().to_ascii_lowercase();
    if o.contains("mac") || o.contains("darwin") || o == "ios" {
        OsKind::Mac
    } else if o.contains("win") {
        OsKind::Windows
    } else if o.contains("android") {
        OsKind::Android
    } else if o.contains("linux") || o.contains("unix") || o.contains("bsd") {
        OsKind::Linux
    } else {
        OsKind::Other
    }
}

pub fn os_label(os: &str) -> &'static str {
    match os_kind(os) {
        OsKind::Mac => "macOS",
        OsKind::Windows => "Windows",
        OsKind::Linux => "Linux",
        OsKind::Android => "Android",
        OsKind::Other => "",
    }
}

/// Neutral OS glyph. Mac is a laptop, Windows the four-pane mark, Linux a
/// terminal, Android a phone. All follow the view's text color.
pub fn os_icon(os: &str, size: f32) -> CanvasView {
    match os_kind(os) {
        OsKind::Mac => stroke_icon(path::LAPTOP, size),
        OsKind::Windows => windows_mark(size),
        OsKind::Linux => stroke_icon(path::SQUARE_TERMINAL, size),
        OsKind::Android => stroke_icon(path::SMARTPHONE, size),
        OsKind::Other => stroke_icon(path::MONITOR, size),
    }
}

/// Four panes, the Windows mark, filled with currentColor.
fn windows_mark(size: f32) -> CanvasView {
    let scale = size / 24.0;
    canvas(move || {
        Painter::new()
            .scale(scale, scale)
            .fill_style_from_view(SvgPaint::CurrentColor)
            .fill_rect(3.0, 3.0, 7.6, 7.6)
            .fill_rect(13.4, 3.0, 7.6, 7.6)
            .fill_rect(3.0, 13.4, 7.6, 7.6)
            .fill_rect(13.4, 13.4, 7.6, 7.6)
    })
    .w(px(size))
    .h(px(size))
    .flex_none()
}

#[derive(Clone, Copy)]
pub enum FileKind {
    Folder,
    Text,
    Markdown,
    Image,
    Pdf,
    Sheet,
    Code,
    Other,
}

pub fn file_kind(name: &str, is_dir: bool) -> FileKind {
    if is_dir {
        return FileKind::Folder;
    }
    let ext = name.rsplit('.').next().unwrap_or("").to_ascii_lowercase();
    match ext.as_str() {
        "md" | "markdown" => FileKind::Markdown,
        "txt" | "log" | "csv" | "rtf" => FileKind::Text,
        "png" | "jpg" | "jpeg" | "gif" | "webp" | "svg" | "bmp" => FileKind::Image,
        "pdf" => FileKind::Pdf,
        "xls" | "xlsx" | "ods" => FileKind::Sheet,
        "rs" | "go" | "py" | "js" | "ts" | "tsx" | "json" | "toml" | "yaml" | "yml" | "c"
        | "cpp" | "h" => FileKind::Code,
        _ => FileKind::Other,
    }
}

pub fn type_label(kind: FileKind) -> &'static str {
    match kind {
        FileKind::Folder => "文件夹",
        FileKind::Text => "文本文档",
        FileKind::Markdown => "Markdown",
        FileKind::Image => "图片",
        FileKind::Pdf => "PDF 文档",
        FileKind::Sheet => "电子表格",
        FileKind::Code => "代码",
        FileKind::Other => "文件",
    }
}

/// Orange dotted-F logo mark (matches `assets/tailfile-logo.svg` grid).
pub fn logo_mark(size: f32) -> CanvasView {
    let s = size / 64.0;
    canvas(move || {
        let color = rgb(0xea580c);
        let mut p = Painter::new().scale(s, s).fill_style(color);
        // 3×5 dotted F from the approved SVG (r≈4.4).
        let dots: &[(f32, f32, f32)] = &[
            (20.5, 9.0, 4.4),
            (32.0, 9.0, 4.4),
            (43.5, 9.0, 4.4),
            (20.5, 20.5, 4.4),
            (20.5, 32.0, 4.4),
            (32.0, 32.0, 4.4),
            (43.5, 32.0, 2.64),
            (20.5, 43.5, 4.4),
            (20.5, 55.0, 4.4),
        ];
        for (i, &(cx, cy, r)) in dots.iter().enumerate() {
            if i == 6 {
                // faint trailing motion dot
                p = p.fill_style(rgb(0xea580c).opacity(0.35));
            } else {
                p = p.fill_style(color);
            }
            p = p
                .begin_path()
                .arc(cx, cy, r, 0.0, std::f32::consts::TAU)
                .close_path()
                .fill();
        }
        p
    })
    .w(px(size))
    .h(px(size))
    .flex_none()
}

/// Online status dot.
pub fn status_dot(online: bool, read_only: bool) -> impl IntoElement {
    let color = if !online {
        rgb(0xc0c5cd)
    } else if read_only {
        rgb(0xf59e0b)
    } else {
        rgb(0x12a150)
    };
    view()
        .w(px(7.0))
        .h(px(7.0))
        .rounded_full()
        .bg(move || color)
        .flex_none()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn notices_mention_licenses() {
        assert!(THIRD_PARTY_NOTICES.contains("ISC"));
        assert!(THIRD_PARTY_NOTICES.contains("MIT"));
    }

    #[test]
    fn kinds_from_names() {
        assert!(matches!(file_kind("a", true), FileKind::Folder));
        assert!(matches!(file_kind("x.txt", false), FileKind::Text));
        assert!(matches!(file_kind("notes.md", false), FileKind::Markdown));
        assert!(matches!(file_kind("p.PNG", false), FileKind::Image));
        assert!(matches!(file_kind("预算.xlsx", false), FileKind::Sheet));
        assert_eq!(os_kind("macOS"), OsKind::Mac);
        assert_eq!(os_kind("darwin"), OsKind::Mac);
        assert_eq!(os_kind("windows"), OsKind::Windows);
        assert_eq!(os_kind("linux"), OsKind::Linux);
        assert_eq!(os_kind("android"), OsKind::Android);
        assert_eq!(os_kind("loopback"), OsKind::Other);
        assert_eq!(os_label("windows"), "Windows");
    }
}
