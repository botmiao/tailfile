//! Local UI persistence: tabs + theme. Mode 0600 / dirs 0700.

use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};

use crate::token;

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, Default)]
#[serde(rename_all = "camelCase")]
pub enum ThemePref {
    #[default]
    System,
    Light,
    Dark,
}

impl ThemePref {
    pub fn as_str(&self) -> &'static str {
        match self {
            ThemePref::System => "system",
            ThemePref::Light => "light",
            ThemePref::Dark => "dark",
        }
    }

    pub fn parse(raw: &str) -> Self {
        match raw.trim().to_ascii_lowercase().as_str() {
            "light" => ThemePref::Light,
            "dark" => ThemePref::Dark,
            _ => ThemePref::System,
        }
    }

    pub fn label(&self) -> &'static str {
        match self {
            ThemePref::System => "跟随系统",
            ThemePref::Light => "浅色",
            ThemePref::Dark => "深色",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SavedTab {
    pub id: String,
    pub node: String,
    pub path: String,
    #[serde(default)]
    pub filter: String,
    #[serde(default = "default_sort")]
    pub sort: String,
    #[serde(default = "default_view")]
    pub view: String,
}

fn default_sort() -> String {
    "name".into()
}
fn default_view() -> String {
    "list".into()
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize, Default)]
#[serde(rename_all = "camelCase")]
pub struct UiPersist {
    pub theme: ThemePref,
    #[serde(default = "default_true")]
    pub restore_tabs: bool,
    #[serde(default)]
    pub tabs: Vec<SavedTab>,
    #[serde(default)]
    pub active_tab: usize,
    #[serde(default = "default_true")]
    pub preview_on: bool,
}

fn default_true() -> bool {
    true
}

pub fn path_for(state_dir: &str) -> PathBuf {
    let dir = if state_dir.is_empty() {
        token::default_state_dir("")
    } else {
        PathBuf::from(state_dir)
    };
    dir.join("nsh-ui.json")
}

pub fn load(state_dir: &str) -> UiPersist {
    let path = path_for(state_dir);
    match std::fs::read_to_string(&path) {
        Ok(raw) => serde_json::from_str(&raw).unwrap_or_default(),
        Err(_) => UiPersist {
            preview_on: token::load_preview_on(),
            ..UiPersist::default()
        },
    }
}

pub fn save(state_dir: &str, data: &UiPersist) {
    let path = path_for(state_dir);
    if let Some(dir) = path.parent() {
        let _ = ensure_dir(dir);
    }
    if let Ok(raw) = serde_json::to_string_pretty(data) {
        let tmp = path.with_extension("json.tmp");
        if std::fs::write(&tmp, raw).is_ok() {
            let _ = std::fs::rename(&tmp, &path);
            #[cfg(unix)]
            {
                use std::os::unix::fs::PermissionsExt;
                let _ = std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o600));
            }
        }
    }
}

fn ensure_dir(dir: &Path) -> std::io::Result<()> {
    std::fs::create_dir_all(dir)?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let _ = std::fs::set_permissions(dir, std::fs::Permissions::from_mode(0o700));
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn round_trip_theme_and_tabs() {
        let dir = std::env::temp_dir().join(format!("tailfile-nsh-persist-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        let data = UiPersist {
            theme: ThemePref::Dark,
            restore_tabs: true,
            tabs: vec![SavedTab {
                id: "t1".into(),
                node: "alpha".into(),
                path: "/tmp/a".into(),
                filter: "png".into(),
                sort: "size".into(),
                view: "icons".into(),
            }],
            active_tab: 0,
            preview_on: false,
        };
        save(dir.to_str().unwrap(), &data);
        let loaded = load(dir.to_str().unwrap());
        assert_eq!(loaded.theme, ThemePref::Dark);
        assert_eq!(loaded.tabs.len(), 1);
        assert_eq!(loaded.tabs[0].filter, "png");
        assert!(!loaded.preview_on);
        let _ = std::fs::remove_dir_all(&dir);
    }
}
