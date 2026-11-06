//! Remote daemon install guidance shown in the client.
//! Commands are for the user to copy; the app never SSHes or mutates remotes.

#[derive(Clone, PartialEq, Eq)]
pub struct InstallGuide {
    pub title: String,
    pub summary: String,
    pub linux: String,
    pub windows: String,
    pub macos: String,
    pub note: String,
}

pub fn for_unreachable(node_label: &str) -> InstallGuide {
    InstallGuide {
        title: format!("无法连接 {node_label} 上的 Tailfile daemon"),
        summary: "本客户端不会自动登录或修改远程机器。请在目标主机上自行安装并运行 agent，然后点刷新。".into(),
        linux: "curl -fsSL https://raw.githubusercontent.com/botmiao/tailfile/main/scripts/install-agent.sh | sh\n# 或：从发布页下载 binary 后\n./tailfile-agent -hostname <name> -state ~/.config/tailfile/<name>".into(),
        windows: "在 PowerShell（管理员可选）下载发布包中的 tailfile-agent.exe，然后：\n.\\tailfile-agent.exe -hostname <name> -state $env:APPDATA\\tailfile\\<name>\n# 可选：注册用户级计划任务见文档 service 章节".into(),
        macos: "从发布页下载 darwin binary：\nchmod +x ./tailfile-agent\n./tailfile-agent -hostname <name> -state ~/Library/Application\\ Support/tailfile/<name>".into(),
        note: "未来可能提供 Tailscale SSH 一键安装；当前 MVP 仅生成可复制命令。确保目标主机已加入同一 tailnet，并配置读写白名单根目录。".into(),
    }
}

pub fn commands_clipboard(guide: &InstallGuide, os: &str) -> String {
    match os {
        "windows" => guide.windows.clone(),
        "macos" => guide.macos.clone(),
        _ => guide.linux.clone(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn guide_mentions_no_ssh() {
        let g = for_unreachable("vps");
        assert!(g.summary.contains("不会自动"));
        assert!(g.note.contains("Tailscale SSH"));
        assert!(!commands_clipboard(&g, "linux").is_empty());
    }
}
