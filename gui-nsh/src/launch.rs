//! Command-line and environment configuration (`-agent`, `-token-file`, and `-state`).

use std::sync::OnceLock;

static LAUNCH: OnceLock<Launch> = OnceLock::new();

/// How this process finds the local agent.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Launch {
    /// Local agent base URL, without a trailing slash.
    pub agent: String,
    /// Explicit `gui-token` path. Empty means derive it from `state`.
    pub token_file: String,
    /// Agent state directory. Empty means the per-OS default.
    pub state: String,
}

impl Launch {
    /// Environment defaults, then flags. Flags win.
    ///
    /// `TAILFILE_AGENT`, `TAILFILE_TOKEN_FILE`, and `TAILFILE_STATE` exist so
    /// `nsh run` (which does not execute `main`) can still point at an agent.
    pub fn parse(args: impl IntoIterator<Item = String>) -> Result<Self, String> {
        let mut launch = Self::from_env();
        let args: Vec<String> = args.into_iter().collect();
        let mut i = 0;
        while i < args.len() {
            let arg = &args[i];
            if arg == "--" {
                break;
            }
            let (name, inline) = split_flag(arg)?;
            let value = if let Some(inline) = inline {
                inline
            } else {
                i += 1;
                args.get(i).cloned().ok_or_else(|| {
                    format!("缺少 {name} 的参数\n\n{}", usage())
                })?
            };
            match name.as_str() {
                "-agent" | "--agent" => launch.agent = value.trim_end_matches('/').to_owned(),
                "-token-file" | "--token-file" => launch.token_file = value,
                "-state" | "--state" => launch.state = value,
                other => {
                    return Err(format!("未知参数 {other}\n\n{}", usage()));
                }
            }
            i += 1;
        }
        if launch.agent.is_empty() {
            launch.agent = "http://127.0.0.1:8645".to_owned();
        }
        Ok(launch)
    }

    fn from_env() -> Self {
        Self {
            agent: std::env::var("TAILFILE_AGENT")
                .ok()
                .map(|s| s.trim_end_matches('/').to_owned())
                .filter(|s| !s.is_empty())
                .unwrap_or_else(|| "http://127.0.0.1:8645".to_owned()),
            token_file: std::env::var("TAILFILE_TOKEN_FILE").unwrap_or_default(),
            state: std::env::var("TAILFILE_STATE").unwrap_or_default(),
        }
    }
}

fn split_flag(arg: &str) -> Result<(String, Option<String>), String> {
    if arg == "-h" || arg == "--help" || arg == "-help" {
        return Err(usage());
    }
    let body = arg
        .strip_prefix("--")
        .map(|rest| format!("--{rest}"))
        .or_else(|| arg.strip_prefix('-').map(|rest| format!("-{rest}")))
        .ok_or_else(|| format!("多余的参数 {arg}\n\n{}", usage()))?;
    if let Some((name, value)) = body.split_once('=') {
        if name == "-" || name == "--" || value.is_empty() {
            return Err(format!("无法解析 {arg}\n\n{}", usage()));
        }
        return Ok((name.to_owned(), Some(value.to_owned())));
    }
    Ok((body, None))
}

pub fn usage() -> String {
    "\
用法: tailfile-nsh [-agent URL] [-token-file PATH] [-state DIR]

  -agent       本机 agent 地址（默认 http://127.0.0.1:8645）
  -token-file  gui-token 文件（默认 <state>/gui-token）
  -state       agent 状态目录（默认与 agent 相同的用户配置目录）

也可以用环境变量 TAILFILE_AGENT、TAILFILE_TOKEN_FILE、TAILFILE_STATE。
token 只从文件读取，不会打印。"
        .to_owned()
}

/// Called from `main` before the window starts.
pub fn set_launch(launch: Launch) {
    let _ = LAUNCH.set(launch);
}

/// The launch `main` stored, or the environment defaults when `nsh run`
/// mounted the root without executing `main`.
pub fn current() -> Launch {
    LAUNCH.get().cloned().unwrap_or_else(Launch::from_env)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn flags_override_defaults() {
        let launch = Launch::parse([
            "-agent".into(),
            "http://127.0.0.1:9999/".into(),
            "--state".into(),
            "/tmp/tf".into(),
            "-token-file=gui-token".into(),
        ])
        .unwrap();
        assert_eq!(launch.agent, "http://127.0.0.1:9999");
        assert_eq!(launch.state, "/tmp/tf");
        assert_eq!(launch.token_file, "gui-token");
    }

    #[test]
    fn help_is_an_error_message_without_a_token() {
        let err = Launch::parse(["--help".into()]).unwrap_err();
        assert!(err.contains("-agent"));
        assert!(!err.contains("tskey"));
    }
}
