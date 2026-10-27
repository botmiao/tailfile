# Manual test checklist

Automated coverage is `go test` in `agent` and `gui-mygo`. This list is the pass a person does on a machine. Do it as a normal user. Nothing here should ask for sudo, an administrator password, or a UAC prompt. Do not paste an auth key into a shell that records history if you can put it in Settings instead. Logs must name `gui-token` and `ts-authkey` paths and must not print either value.

`<hostname>` below is `tailfile-` plus the machine hostname, unless you passed `-hostname`.

## State files

| OS | State directory |
| --- | --- |
| Linux | `~/.config/tailfile/<hostname>/` |
| macOS | `~/Library/Application Support/tailfile/<hostname>/` |
| Windows | `%AppData%\tailfile\<hostname>\` |

- [ ] After a start with no `-state`, `gui-token` is in that directory, mode `0600` (Windows: the file's DACL grants the current user and not other users).
- [ ] The agent log prints the `gui-token` path and not the token.
- [ ] Restarting the agent changes the token. The GUI that was already open fails until it is restarted and reads the new file.
- [ ] `ts-authkey` is absent until you save one. After a save it is mode `0600` (Windows: current-user DACL) and still absent from the log and from `GET /v1/local/settings`.

## Default share

- [ ] Start with no `-read` and no `-write`. The agent creates `~/Tailfile` mode `0700` and shares only that directory.
- [ ] A paste into that folder works. A paste into a path outside it is refused.
- [ ] `-demo` does not create `~/Tailfile`. Demo shares its temporary fixture directories.

## System tailscale mode

Requires a tailscaled that is already logged in as you. No auth key.

```sh
./tailfile-agent -net system -read "$HOME/Tailfile" -write "$HOME/Tailfile"
```

- [ ] The log says system mode, not tsnet. `GET /v1/local/settings` (bearer from `gui-token`) shows `"net": "system"`.
- [ ] The listener is on the node's Tailscale addresses, port 8644, and not on `0.0.0.0`.
- [ ] A second machine on the same tailnet, same tailnet user, lists this node and can read `~/Tailfile`.
- [ ] A different tailnet user is refused.
- [ ] On Linux, if the log says the LocalAPI socket is not readable, `sudo tailscale set --operator=$USER` and start the agent again as that user. The agent itself still must not be run with sudo.
- [ ] `-net auto` selects system when tailscaled is Running and has a Tailscale IP, and tsnet otherwise. The log says which and why.
- [ ] Quit tailscaled, or take away its IP, and confirm the agent rebinds or exits rather than listening on a non-Tailscale address.

## Per-user service

Use the same flags you want the background process to keep. Example:

```sh
./tailfile-agent service install -net system -read "$HOME/Tailfile" -write "$HOME/Tailfile"
./tailfile-agent service start
./tailfile-agent service status
```

- [ ] Install prints a per-user success line and does not prompt for a password.
- [ ] `service.json` in the state directory has `"scope": "user"`, the executable, and the flags. It does not contain an auth key.
- [ ] The unit or task also contains the resolved `-hostname`, `-state`, and `-net` when you omitted them, and `-read` pointing at `~/Tailfile` when you omitted `-read`.
- [ ] A second `service install` fails and tells you to uninstall first.
- [ ] `./tailfile-agent service install --system` fails and does not write a system unit, a LaunchDaemon, or a Windows service.
- [ ] `service stop` then `service status` reports stopped. `service uninstall` removes the definition. `service.json` is removed. `ts-authkey` is not removed by uninstall.

### Linux

- [ ] The unit is `~/.config/systemd/user/tailfile-agent.service`, wanted by `default.target`. There is no `/etc/systemd/system/tailfile-agent.service`.
- [ ] Log out and back in. The agent is running without a manual start.
- [ ] `loginctl enable-linger "$USER"`, then reboot and do not log in. The user manager still runs the agent. Disable linger afterwards if you do not want that.

### macOS

- [ ] The plist is `~/Library/LaunchAgents/tailfile-agent.plist`. There is no plist under `/Library/LaunchDaemons`.
- [ ] Install alone does not start the process. `service start` loads it (`launchctl list` shows `tailfile-agent`).
- [ ] `service stop` unloads it. It does not come back at the next login until `service start`.

### Windows

- [ ] Task Scheduler has a task named `TailfileAgent` for the current user. It is not listed in `services.msc`.
- [ ] The task's run level is Least Privilege. Creating it did not show UAC.
- [ ] Log off and on. The agent starts as the current user.
- [ ] `service start`, `service stop`, and `service status` match `schtasks /Run`, `/End`, and `/Query`.

## Conflict prompt

On two nodes (or one node and a local import), copy a file onto a path that already has a finished file with the same name and different contents.

- [ ] The transfer stays at `conflict`. The old destination bytes are unchanged. Wails and MyGo both show 目标已有同名文件, the path, 覆盖, 跳过, and 对后续冲突都这样处理.
- [ ] 跳过 leaves the destination unchanged and the job completes.
- [ ] Repeat, choose 覆盖. The destination becomes the source bytes. While the copy is in progress the old file is still the old bytes (the new bytes are in `*.tailfile-partial` until the hash check).
- [ ] Copy a directory of two conflicting files. Check 对后续冲突都这样处理 and choose 跳过 on the first. The second is skipped with no second dialog.
- [ ] Stop the agent while a dialog is open. Start it again. The job is still `conflict` (not `interrupted`), and the dialog comes back. Answer it. The copy finishes.
- [ ] Interrupt a copy that is mid-file, leaving a partial whose meta matches the source size and mtime, and leave a finished file at the destination. Resume. There is no dialog, and the finished file is replaced only after the hash matches.
- [ ] Cancel from the dialog (the transfer Cancel control, not 覆盖 or 跳过). The destination is unchanged.

## Auth key setting

Use a disposable tsnet auth key. Do not use a key you cannot revoke.

- [ ] Settings in both GUIs shows the current net mode (`system` or `tsnet`, and the requested `-net` when it differs).
- [ ] Before a save, the screen says the key is not configured. `GET /v1/local/settings` has `"authKeyConfigured": false` and no key field.
- [ ] Paste a key and save. The GUI says to restart. The response has `configured: true` and `restartRequired: true` and does not contain the key. `ts-authkey` on disk matches the key you pasted. The agent log does not.
- [ ] Restart with `-net tsnet` and without `TS_AUTHKEY`. The log says the key came from the file and does not print it. The node joins.
- [ ] Export a different `TS_AUTHKEY` and restart. The log says the source is `TS_AUTHKEY`. The file is unchanged. The node uses the environment value.
- [ ] Clear in Settings, restart with no `TS_AUTHKEY` and no `-authkey`. The file is gone. tsnet prints a login URL instead of using the old key.
- [ ] `GET` the settings URL on the tailnet port (8644). It is not the settings document.
