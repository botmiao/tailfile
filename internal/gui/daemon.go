package gui

import (
	"fmt"
	"runtime"
	"strings"
)

// InstallGuide explains how to start or install the background tailfile agent
// when the GUI cannot reach an explicitly configured or external agent.
type InstallGuide struct {
	Headline string
	Summary  string
	Commands []string
	Footnote string
}

// ForUnreachable builds a platform-specific help guide for daemon startup.
func ForUnreachable(localURL, reason string) InstallGuide {
	var cmds []string
	var footnote string
	switch runtime.GOOS {
	case "windows":
		cmds = []string{
			"tailfile",
			"tailfile agent --local 127.0.0.1:8645",
			"tailfile service status",
		}
		footnote = "直接运行 tailfile 不带参数即可使用内置模式，无需开启 8645 本地端口；仅调试外部后台时通过 --local 指定地址。"
	case "darwin":
		cmds = []string{
			"tailfile",
			"tailfile agent --local 127.0.0.1:8645",
			"tailfile service status",
		}
		footnote = "直接运行 tailfile 不带参数即可使用内置模式并驻留托盘；仅调试外部后台时通过 --local 指定地址。"
	default:
		cmds = []string{
			"tailfile",
			"tailfile agent --local 127.0.0.1:8645",
			"tailfile service status",
		}
		footnote = "直接运行 tailfile 不带参数即可使用内置模式；仅调试外部后台时通过 --local 指定地址。"
	}
	summary := fmt.Sprintf("未能连接到本地或指定的 agent（%s）。", localURL)
	if strings.TrimSpace(reason) != "" {
		summary = fmt.Sprintf("未能连接到 %s：%s", localURL, reason)
	}
	return InstallGuide{
		Headline: "后台 Agent 未运行或无法连接",
		Summary:  summary,
		Commands: cmds,
		Footnote: footnote,
	}
}

// CommandsClipboard returns newline-separated shell commands for copying.
func CommandsClipboard(g InstallGuide) string {
	return strings.Join(g.Commands, "\n")
}
