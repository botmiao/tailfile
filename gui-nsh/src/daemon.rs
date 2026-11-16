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
        summary: "本客户端不会自动登录或修改远程机器。请用下面的命令在目标主机上安装并启动 agent，然后点刷新。".into(),
        linux: "# 在 tailfile 仓库目录中运行，经 Tailscale SSH 安装到目标主机（需要 Go）：\nscripts/install-agent.sh --ssh <user>@<host>\n# 或在目标主机本机运行：\nscripts/install-agent.sh".into(),
        windows: "下载或构建 tailfile-agent.exe，然后在 PowerShell（无需管理员）中：\n.\\tailfile-agent.exe service install --net system --read \"$HOME\\Tailfile\" --write \"$HOME\\Tailfile\"\n# install 会注册用户级登录任务并立即启动".into(),
        macos: "# 在 tailfile 仓库目录中运行，经 Tailscale SSH 安装到目标 Mac（需要 Go）：\nscripts/install-agent.sh --ssh <user>@<host>\n# 或在目标 Mac 本机运行：\nscripts/install-agent.sh".into(),
        note: "Tailscale SSH 一键安装由仓库里的 scripts/install-agent.sh 完成（目标需开启 Tailscale SSH）；已安装时会只替换二进制并重启。确保目标主机已加入同一 tailnet，默认读写根目录为 ~/Tailfile。".into(),
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

    #[test]
    fn guide_uses_the_shipped_install_script() {
        let g = for_unreachable("vps");
        let script = concat!(env!("CARGO_MANIFEST_DIR"), "/../scripts/install-agent.sh");
        assert!(std::path::Path::new(script).is_file(), "missing {script}");
        for os in ["linux", "macos"] {
            let cmd = commands_clipboard(&g, os);
            assert!(cmd.contains("scripts/install-agent.sh --ssh"), "{cmd}");
            assert!(!cmd.contains("curl"), "{cmd}");
        }
        assert!(commands_clipboard(&g, "windows").contains("service install"));
    }
}
