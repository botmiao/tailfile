//! The same `gui-token` lookup as `agent/guitoken`.
//! Errors name the path and never the token.

use std::path::PathBuf;

pub const FILE_NAME: &str = "gui-token";

/// `tailfile-` plus the sanitized host name. Matches `guitoken.DefaultHostname`.
pub fn default_hostname() -> String {
    let host = hostname().unwrap_or_else(|| "node".to_owned());
    let host = host.trim();
    let host = if host.is_empty() { "node" } else { host };
    format!("tailfile-{}", sanitize(host))
}

/// `UserConfigDir/tailfile/<hostname>`, or the temp dir when the config dir
/// cannot be resolved. Matches `guitoken.DefaultStateDir`.
pub fn default_state_dir(hostname: &str) -> PathBuf {
    let hostname = if hostname.is_empty() {
        default_hostname()
    } else {
        hostname.to_owned()
    };
    user_config_dir().join("tailfile").join(hostname)
}

/// `token_file` wins. Otherwise `<state>/gui-token`. An empty state uses
/// [`default_state_dir`].
pub fn token_path(token_file: &str, state_dir: &str) -> PathBuf {
    if !token_file.is_empty() {
        return PathBuf::from(token_file);
    }
    let dir = if state_dir.is_empty() {
        default_state_dir("")
    } else {
        PathBuf::from(state_dir)
    };
    dir.join(FILE_NAME)
}

/// Reads the bearer. The error names `path` and does not include the file body.
pub fn load(token_file: &str, state_dir: &str) -> Result<String, String> {
    let path = token_path(token_file, state_dir);
    let raw = std::fs::read_to_string(&path).map_err(|err| {
        format!("无法读取 GUI token（{}）：{err}", path.display())
    })?;
    let token = raw.trim().to_owned();
    if token.is_empty() {
        return Err(format!("GUI token 文件是空的（{}）", path.display()));
    }
    Ok(token)
}

pub fn sanitize(input: &str) -> String {
    let mut out = String::new();
    for ch in input.chars() {
        match ch {
            'a'..='z' | 'A'..='Z' | '0'..='9' | '-' | '_' | '.' => out.push(ch),
            _ => out.push('-'),
        }
    }
    if out.is_empty() || out == "." || out == ".." {
        "node".to_owned()
    } else {
        out
    }
}

/// `os.UserConfigDir`: `%AppData%` on Windows, `~/Library/Application Support`
/// on macOS, `$XDG_CONFIG_HOME` or `~/.config` on Linux. A relative
/// `XDG_CONFIG_HOME` is rejected, matching Go, and we fall back to the temp dir.
pub fn user_config_dir() -> PathBuf {
    #[cfg(target_os = "windows")]
    {
        if let Some(dir) = env_path("APPDATA") {
            return dir;
        }
        return std::env::temp_dir();
    }
    #[cfg(target_os = "macos")]
    {
        if let Some(home) = env_path("HOME") {
            return home.join("Library").join("Application Support");
        }
        return std::env::temp_dir();
    }
    #[cfg(not(any(target_os = "windows", target_os = "macos")))]
    {
        if let Some(xdg) = std::env::var_os("XDG_CONFIG_HOME") {
            if !xdg.is_empty() {
                let path = PathBuf::from(&xdg);
                if path.is_absolute() {
                    return path;
                }
                return std::env::temp_dir();
            }
        }
        if let Some(home) = env_path("HOME") {
            return home.join(".config");
        }
        std::env::temp_dir()
    }
}

/// `os.UserCacheDir`: `%LocalAppData%`, `~/Library/Caches`, or
/// `$XDG_CACHE_HOME` / `~/.cache`.
pub fn user_cache_dir() -> PathBuf {
    #[cfg(target_os = "windows")]
    {
        if let Some(dir) = env_path("LOCALAPPDATA") {
            return dir;
        }
        return std::env::temp_dir();
    }
    #[cfg(target_os = "macos")]
    {
        if let Some(home) = env_path("HOME") {
            return home.join("Library").join("Caches");
        }
        return std::env::temp_dir();
    }
    #[cfg(not(any(target_os = "windows", target_os = "macos")))]
    {
        if let Some(xdg) = std::env::var_os("XDG_CACHE_HOME") {
            if !xdg.is_empty() {
                let path = PathBuf::from(xdg);
                if path.is_absolute() {
                    return path;
                }
            }
        }
        if let Some(home) = env_path("HOME") {
            return home.join(".cache");
        }
        std::env::temp_dir()
    }
}

fn env_path(key: &str) -> Option<PathBuf> {
    let value = std::env::var_os(key)?;
    if value.is_empty() {
        return None;
    }
    Some(PathBuf::from(value))
}

fn hostname() -> Option<String> {
    hostname::get()
        .ok()
        .map(|value| value.to_string_lossy().into_owned())
        .filter(|value| !value.is_empty())
}

/// Preview on/off. `0` or `1`, mode `0600` where the OS honors it.
/// `UserConfigDir/tailfile/nsh-preview`.
pub fn preview_pref_path() -> PathBuf {
    user_config_dir().join("tailfile").join("nsh-preview")
}

pub fn load_preview_on() -> bool {
    match std::fs::read_to_string(preview_pref_path()) {
        Ok(text) => text.trim() != "0",
        Err(_) => true,
    }
}

pub fn save_preview_on(on: bool) {
    let path = preview_pref_path();
    if let Some(dir) = path.parent() {
        let _ = std::fs::create_dir_all(dir);
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let _ = std::fs::set_permissions(dir, std::fs::Permissions::from_mode(0o700));
        }
    }
    let _ = std::fs::write(&path, if on { "1" } else { "0" });
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let _ = std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o600));
    }
}

/// Writes preview bytes under the user cache. The path is absolute so the
/// image element can load it. Returns the path, not a URL.
pub fn write_preview_file(ext: &str, bytes: &[u8]) -> Result<PathBuf, String> {
    let dir = user_cache_dir().join("tailfile");
    std::fs::create_dir_all(&dir).map_err(|err| err.to_string())?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let _ = std::fs::set_permissions(&dir, std::fs::Permissions::from_mode(0o700));
    }
    let name = format!("nsh-preview-{:016x}.{}", fnv(bytes), ext.trim_start_matches('.'));
    let path = dir.join(name);
    std::fs::write(&path, bytes).map_err(|err| err.to_string())?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let _ = std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o600));
    }
    Ok(path)
}

fn fnv(bytes: &[u8]) -> u64 {
    let mut hash = 0xcbf29ce484222325u64;
    for byte in bytes {
        hash ^= u64::from(*byte);
        hash = hash.wrapping_mul(0x100000001b3);
    }
    hash
}

pub fn path_is_under(path: &str, root: &str) -> bool {
    let path = norm_remote(path);
    let root = norm_remote(root);
    path == root || path.starts_with(&(root.trim_end_matches('/').to_owned() + "/"))
}

pub fn norm_remote(path: &str) -> String {
    path.replace('\\', "/").trim_end_matches('/').to_owned()
}

/// `agent.JoinRemote`.
pub fn join_remote(dir: &str, name: &str) -> String {
    let dir = norm_remote(dir);
    let name = norm_remote(name).trim_start_matches('/').to_owned();
    if name.is_empty() {
        return if dir.is_empty() { "/".to_owned() } else { dir };
    }
    if dir.is_empty() {
        return name;
    }
    if dir.ends_with(':') {
        return format!("{dir}/{name}");
    }
    format!("{dir}/{name}")
}

/// `agent.BaseRemote`.
pub fn base_remote(path: &str) -> String {
    let path = norm_remote(path);
    if path.is_empty() {
        return String::new();
    }
    match path.rfind('/') {
        Some(index) => path[index + 1..].to_owned(),
        None => path,
    }
}

pub fn parent_remote(path: &str) -> Option<String> {
    let path = norm_remote(path);
    let index = path.rfind('/')?;
    let parent = &path[..index];
    if parent.is_empty() {
        return Some("/".to_owned());
    }
    if parent.ends_with(':') {
        return Some(format!("{parent}/"));
    }
    Some(parent.to_owned())
}

/// `agent.PreviewKind`.
pub fn preview_kind(name: &str) -> &'static str {
    let ext = name
        .rsplit_once('.')
        .map(|(_, ext)| ext.to_ascii_lowercase())
        .unwrap_or_default();
    match ext.as_str() {
        "txt" | "md" | "log" | "csv" | "json" | "yml" | "yaml" | "xml" | "html" | "htm"
        | "css" | "js" | "ts" | "tsx" | "jsx" | "go" | "py" | "rs" | "toml" | "ini" | "sh"
        | "mod" | "sum" => "text",
        "png" | "jpg" | "jpeg" | "gif" | "webp" | "bmp" => "image",
        "pdf" => "pdf",
        _ => "other",
    }
}

pub fn format_size(bytes: i64, dir: bool) -> String {
    if dir {
        return "—".to_owned();
    }
    let n = bytes.max(0) as f64;
    if n < 1024.0 {
        return format!("{bytes} B");
    }
    if n < 1024.0 * 1024.0 {
        return format!("{:.1} KB", n / 1024.0);
    }
    if n < 1024.0 * 1024.0 * 1024.0 {
        return format!("{:.1} MB", n / (1024.0 * 1024.0));
    }
    format!("{:.1} GB", n / (1024.0 * 1024.0 * 1024.0))
}

pub fn format_time(raw: &str) -> String {
    if raw.len() >= 16 && raw.as_bytes().get(10) == Some(&b'T') {
        return format!("{} {}", &raw[..10], &raw[11..16]);
    }
    raw.to_owned()
}

/// Turns a drop URI into a filesystem path. Plain paths pass through.
pub fn file_uri_to_path(uri: &str) -> String {
    let Some(rest) = uri.strip_prefix("file:") else {
        return uri.to_owned();
    };
    let decoded = percent_decode(rest);
    let path = if let Some(after) = decoded.strip_prefix("//") {
        match after.split_once('/') {
            Some(("" | "localhost", path)) => path.to_owned(),
            Some((host, path)) => format!("//{host}/{path}"),
            None => decoded,
        }
    } else {
        decoded.trim_start_matches('/').to_owned()
    };
    #[cfg(target_os = "windows")]
    {
        let path = path.replace('/', "\\");
        if path.len() >= 3 && path.as_bytes()[1] == b':' {
            return path;
        }
        if path.starts_with("\\\\") {
            return path;
        }
        return uri.to_owned();
    }
    #[cfg(not(target_os = "windows"))]
    {
        if path.starts_with('/') {
            path
        } else if uri.contains("://") || uri.starts_with("file:") {
            format!("/{path}")
        } else {
            path
        }
    }
}

fn percent_decode(input: &str) -> String {
    let bytes = input.as_bytes();
    let mut out = Vec::with_capacity(bytes.len());
    let mut i = 0;
    while i < bytes.len() {
        if bytes[i] == b'%' && i + 2 < bytes.len() {
            if let (Some(hi), Some(lo)) = (hex(bytes[i + 1]), hex(bytes[i + 2])) {
                out.push((hi << 4) | lo);
                i += 3;
                continue;
            }
        }
        if bytes[i] == b'+' {
            out.push(b' ');
        } else {
            out.push(bytes[i]);
        }
        i += 1;
    }
    String::from_utf8_lossy(&out).into_owned()
}

fn hex(byte: u8) -> Option<u8> {
    match byte {
        b'0'..=b'9' => Some(byte - b'0'),
        b'a'..=b'f' => Some(byte - b'a' + 10),
        b'A'..=b'F' => Some(byte - b'A' + 10),
        _ => None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn sanitize_matches_go() {
        assert_eq!(sanitize("box"), "box");
        assert_eq!(sanitize("a b"), "a-b");
        assert_eq!(sanitize(".."), "node");
        assert_eq!(sanitize(""), "node");
    }

    #[test]
    fn token_file_wins_over_state() {
        let path = token_path("/tmp/gui-token", "/other");
        assert_eq!(path, PathBuf::from("/tmp/gui-token"));
    }

    #[test]
    fn state_dir_joins_gui_token() {
        let path = token_path("", "/var/state");
        assert_eq!(path, PathBuf::from("/var/state/gui-token"));
    }

    #[test]
    fn empty_token_file_is_an_error_without_echoing_whitespace() {
        let dir = std::env::temp_dir().join(format!("tf-nsh-token-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        std::fs::write(dir.join("gui-token"), "  \n").unwrap();
        let err = load("", dir.to_str().unwrap()).unwrap_err();
        assert!(err.contains("空"));
        assert!(!err.contains("secret"));
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn join_keeps_windows_drive_root() {
        assert_eq!(join_remote("C:", "Tailfile"), "C:/Tailfile");
        assert_eq!(join_remote("/a/b/", "c"), "/a/b/c");
        assert_eq!(base_remote("/a/b/c"), "c");
        assert_eq!(parent_remote("/a/b").as_deref(), Some("/a"));
        assert_eq!(parent_remote("C:/Tailfile").as_deref(), Some("C:/"));
    }

    #[test]
    fn preview_kind_matches_the_agent() {
        assert_eq!(preview_kind("hello.txt"), "text");
        assert_eq!(preview_kind("week.md"), "text");
        assert_eq!(preview_kind("swatch.PNG"), "image");
        assert_eq!(preview_kind("sample.pdf"), "pdf");
        assert_eq!(preview_kind("payload.bin"), "other");
    }

    #[test]
    fn file_uri_decodes() {
        let path = file_uri_to_path("file:///tmp/a%20b.txt");
        assert!(path.ends_with("a b.txt"), "{path}");
    }
}
