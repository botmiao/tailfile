//! Localhost client for the Tailfile agent. The bearer comes from disk.
//! This client does not set `Origin`: an empty Origin plus the bearer is
//! what the agent allows for a native shell that is not the Wails page.
//! The auth key is sent once on PUT and is never written to a log.

use std::time::Duration;

use serde::Deserialize;
use serde::de::DeserializeOwned;

use crate::token::{base_remote, format_size};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct NodeInfo {
    pub id: String,
    pub name: String,
    /// Host OS as the agent reports it (`macos`, `windows`, `linux`, `android`, …).
    pub os: String,
    pub online: bool,
    pub agent_ok: bool,
    pub self_node: bool,
    pub read_only: bool,
    pub read_roots: Vec<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Entry {
    pub name: String,
    pub path: String,
    pub is_dir: bool,
    pub size: i64,
    pub mod_time: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Preview {
    pub kind: String,
    pub name: String,
    pub text: String,
    pub truncated: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Job {
    pub id: String,
    pub mode: String,
    pub status: String,
    pub bytes_total: i64,
    pub bytes_done: i64,
    pub current: String,
    pub dest_path: String,
    pub error: String,
    pub conflict_path: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Transfer {
    pub id: String,
    pub mode: String,
    pub from_node: String,
    pub from_path: String,
    pub to_node: String,
    pub to_path: String,
    pub job: Job,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Settings {
    pub net: String,
    pub net_requested: String,
    pub auth_key_configured: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AuthKeyStatus {
    pub configured: bool,
    pub restart_required: bool,
}

#[derive(Debug, Clone)]
pub struct Client {
    pub base: String,
    pub token: String,
}

impl Client {
    pub fn new(base: impl Into<String>, token: impl Into<String>) -> Self {
        Self {
            base: base.into().trim_end_matches('/').to_owned(),
            token: token.into(),
        }
    }

    pub fn health(&self) -> Result<(), String> {
        let _ = self.get("/v1/health")?;
        Ok(())
    }

    pub fn nodes(&self) -> Result<Vec<NodeInfo>, String> {
        let body = self.get("/v1/nodes")?;
        let parsed: NodesBody = parse_json(&body)?;
        Ok(parsed.nodes.into_iter().map(NodeInfo::from).collect())
    }

    pub fn list(&self, node: &str, path: &str) -> Result<Vec<Entry>, String> {
        let url = query("/v1/fs/list", &[("node", node), ("path", path)]);
        let body = self.get(&url)?;
        let parsed: ListBody = parse_json(&body)?;
        Ok(parsed.entries.into_iter().map(Entry::from).collect())
    }

    pub fn preview(&self, node: &str, path: &str) -> Result<Preview, String> {
        let url = query("/v1/fs/preview", &[("node", node), ("path", path)]);
        Ok(parse_json::<PreviewBody>(&self.get(&url)?)?.into())
    }

    pub fn read(&self, node: &str, path: &str) -> Result<Vec<u8>, String> {
        let url = query("/v1/fs/read", &[("node", node), ("path", path)]);
        self.get(&url)
    }

    pub fn mkdir(&self, node: &str, path: &str) -> Result<(), String> {
        self.send(
            "POST",
            "/v1/fs/mkdir",
            Some(&serde_json::json!({ "node": node, "path": path })),
        )?;
        Ok(())
    }

    pub fn rename(&self, node: &str, from: &str, to: &str) -> Result<(), String> {
        self.send(
            "POST",
            "/v1/fs/rename",
            Some(&serde_json::json!({ "node": node, "from": from, "to": to })),
        )?;
        Ok(())
    }

    pub fn delete(&self, node: &str, path: &str) -> Result<(), String> {
        self.send(
            "POST",
            "/v1/fs/delete",
            Some(&serde_json::json!({ "node": node, "path": path })),
        )?;
        Ok(())
    }

    pub fn start_transfer(
        &self,
        mode: &str,
        from_node: &str,
        from_path: &str,
        to_node: &str,
        to_path: &str,
    ) -> Result<Transfer, String> {
        let body = serde_json::json!({
            "mode": mode,
            "fromNode": from_node,
            "fromPath": from_path,
            "toNode": to_node,
            "toPath": to_path,
        });
        Ok(parse_json::<TransferBody>(&self.send("POST", "/v1/transfers", Some(&body))?)?.into())
    }

    pub fn transfers(&self) -> Result<Vec<Transfer>, String> {
        let parsed: TransfersBody = parse_json(&self.get("/v1/transfers")?)?;
        Ok(parsed.transfers.into_iter().map(Transfer::from).collect())
    }

    pub fn jobs(&self) -> Result<Vec<Job>, String> {
        let parsed: JobsBody = parse_json(&self.get("/v1/jobs")?)?;
        Ok(parsed.jobs.into_iter().map(Job::from).collect())
    }

    pub fn cancel_transfer(&self, id: &str) -> Result<(), String> {
        self.send(
            "POST",
            &format!("/v1/transfers/{}/cancel", encode(id)),
            None,
        )?;
        Ok(())
    }

    pub fn resume_transfer(&self, id: &str) -> Result<(), String> {
        self.send(
            "POST",
            &format!("/v1/transfers/{}/resume", encode(id)),
            None,
        )?;
        Ok(())
    }

    pub fn answer_transfer_conflict(
        &self,
        id: &str,
        action: &str,
        apply_to_rest: bool,
    ) -> Result<(), String> {
        self.answer(
            &format!("/v1/transfers/{}/conflict", encode(id)),
            action,
            apply_to_rest,
        )
    }

    pub fn answer_job_conflict(
        &self,
        id: &str,
        action: &str,
        apply_to_rest: bool,
    ) -> Result<(), String> {
        self.answer(
            &format!("/v1/jobs/{}/conflict", encode(id)),
            action,
            apply_to_rest,
        )
    }

    fn answer(&self, path: &str, action: &str, apply_to_rest: bool) -> Result<(), String> {
        let body = serde_json::json!({ "action": action, "applyToRest": apply_to_rest });
        self.send("POST", path, Some(&body))?;
        Ok(())
    }

    pub fn set_drop_target(&self, node: &str, dest_dir: &str) -> Result<(), String> {
        self.send(
            "POST",
            "/v1/local/drop-target",
            Some(&serde_json::json!({ "node": node, "destDir": dest_dir })),
        )?;
        Ok(())
    }

    pub fn import(&self, node: &str, dest_dir: &str, paths: &[String]) -> Result<Job, String> {
        let body = serde_json::json!({ "node": node, "destDir": dest_dir, "paths": paths });
        Ok(parse_json::<JobBody>(&self.send("POST", "/v1/local/import", Some(&body))?)?.into())
    }

    pub fn cache(&self, node: &str, path: &str) -> Result<String, String> {
        let body = serde_json::json!({ "node": node, "path": path });
        let parsed: CacheBody = parse_json(&self.send("POST", "/v1/local/cache", Some(&body))?)?;
        Ok(parsed.local_path)
    }

    pub fn reveal(&self, path: &str) -> Result<(), String> {
        self.send(
            "POST",
            "/v1/local/reveal",
            Some(&serde_json::json!({ "path": path })),
        )?;
        Ok(())
    }

    pub fn settings(&self) -> Result<Settings, String> {
        Ok(parse_json::<SettingsBody>(&self.get("/v1/local/settings")?)?.into())
    }

    /// Writes the key. The returned status does not contain it, and this
    /// function's error text is the agent's `error` field.
    pub fn set_auth_key(&self, auth_key: &str) -> Result<AuthKeyStatus, String> {
        let body = serde_json::json!({ "authKey": auth_key });
        Ok(
            parse_json::<AuthBody>(&self.send(
                "PUT",
                "/v1/local/settings/authkey",
                Some(&body),
            )?)?
            .into(),
        )
    }

    pub fn clear_auth_key(&self) -> Result<AuthKeyStatus, String> {
        Ok(
            parse_json::<AuthBody>(&self.send("DELETE", "/v1/local/settings/authkey", None)?)?
                .into(),
        )
    }

    fn get(&self, path: &str) -> Result<Vec<u8>, String> {
        self.send("GET", path, None)
    }

    fn send(
        &self,
        method: &str,
        path: &str,
        body: Option<&serde_json::Value>,
    ) -> Result<Vec<u8>, String> {
        let url = format!("{}{path}", self.base);
        let auth = format!("Bearer {}", self.token);
        let agent = http_agent();
        let result = if let Some(body) = body {
            let payload = body.to_string();
            match method {
                "POST" => agent
                    .post(&url)
                    .header("Authorization", &auth)
                    .header("Content-Type", "application/json")
                    .send(payload),
                "PUT" => agent
                    .put(&url)
                    .header("Authorization", &auth)
                    .header("Content-Type", "application/json")
                    .send(payload),
                other => return Err(format!("不支持的请求 {other}")),
            }
        } else {
            match method {
                "GET" => agent.get(&url).header("Authorization", &auth).call(),
                "DELETE" => agent.delete(&url).header("Authorization", &auth).call(),
                "POST" => agent
                    .post(&url)
                    .header("Authorization", &auth)
                    .header("Content-Type", "application/json")
                    .send("{}"),
                other => return Err(format!("不支持的请求 {other}")),
            }
        };
        let mut response = result.map_err(|err| err.to_string())?;
        let status = response.status().as_u16();
        let bytes = response
            .body_mut()
            .read_to_vec()
            .map_err(|err| err.to_string())?;
        if !(200..300).contains(&status) {
            return Err(error_message(status, &bytes));
        }
        Ok(bytes)
    }
}

/// A fresh agent per call. Sharing one `ureq` agent across the engine's
/// blocking threads stalled later requests (preview never returned) while
/// the poll loop held the pool.
fn http_agent() -> ureq::Agent {
    ureq::Agent::config_builder()
        .http_status_as_error(false)
        .timeout_global(Some(Duration::from_secs(60)))
        .build()
        .new_agent()
}

fn error_message(status: u16, body: &[u8]) -> String {
    if let Ok(value) = serde_json::from_slice::<serde_json::Value>(body) {
        if let Some(message) = value.get("error").and_then(|item| item.as_str()) {
            if !message.is_empty() {
                return message.to_owned();
            }
        }
    }
    let text = String::from_utf8_lossy(body);
    let text = text.trim();
    if text.is_empty() {
        format!("HTTP {status}")
    } else {
        format!("HTTP {status}: {text}")
    }
}

fn parse_json<T: DeserializeOwned>(body: &[u8]) -> Result<T, String> {
    serde_json::from_slice(body).map_err(|err| format!("无法解析 agent 响应：{err}"))
}

fn query(path: &str, pairs: &[(&str, &str)]) -> String {
    let mut url = String::from(path);
    if !pairs.is_empty() {
        url.push('?');
        for (index, (key, value)) in pairs.iter().enumerate() {
            if index > 0 {
                url.push('&');
            }
            url.push_str(&encode(key));
            url.push('=');
            url.push_str(&encode(value));
        }
    }
    url
}

fn encode(value: &str) -> String {
    let mut out = String::new();
    for byte in value.bytes() {
        match byte {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                out.push(byte as char);
            }
            _ => out.push_str(&format!("%{byte:02X}")),
        }
    }
    out
}

pub fn job_line(job: &Job) -> String {
    let pct = if job.bytes_total > 0 {
        format!(" {}%", job.bytes_done.saturating_mul(100) / job.bytes_total)
    } else {
        String::new()
    };
    let mut name = base_remote(&job.current);
    if name.is_empty() {
        name = base_remote(&job.dest_path);
    }
    let mut message = format!("{}{pct}  {name}", status_label(&job.status));
    if !job.error.is_empty() && job.status != "completed" {
        message.push_str("  ");
        message.push_str(&job.error);
    }
    message.trim().to_owned()
}

pub fn status_label(status: &str) -> String {
    match status {
        "queued" => "排队".to_owned(),
        "running" => "进行中".to_owned(),
        "completed" => "完成".to_owned(),
        "canceled" => "已取消".to_owned(),
        "error" => "错误".to_owned(),
        "interrupted" => "已中断".to_owned(),
        "conflict" => "冲突".to_owned(),
        other => other.to_owned(),
    }
}

pub fn mode_label(mode: &str) -> &'static str {
    match mode {
        "push" => "推送",
        "pull" => "拉取",
        "import" => "导入",
        _ => "传输",
    }
}

pub fn size_label(bytes: i64, dir: bool) -> String {
    format_size(bytes, dir)
}

fn vec_or_empty<'de, D, T>(deserializer: D) -> Result<Vec<T>, D::Error>
where
    D: serde::Deserializer<'de>,
    T: Deserialize<'de>,
{
    Ok(Option::<Vec<T>>::deserialize(deserializer)?.unwrap_or_default())
}

#[derive(Deserialize)]
struct NodesBody {
    #[serde(default, deserialize_with = "vec_or_empty")]
    nodes: Vec<NodeBody>,
}

#[derive(Deserialize)]
struct NodeBody {
    #[serde(default)]
    id: String,
    #[serde(default)]
    name: String,
    #[serde(default)]
    os: String,
    #[serde(default)]
    online: bool,
    #[serde(default, rename = "agentOK")]
    agent_ok: bool,
    #[serde(default, rename = "self")]
    self_node: bool,
    #[serde(default, rename = "readOnly")]
    read_only: bool,
    #[serde(default, rename = "readRoots", deserialize_with = "vec_or_empty")]
    read_roots: Vec<String>,
}

impl From<NodeBody> for NodeInfo {
    fn from(body: NodeBody) -> Self {
        Self {
            id: body.id,
            name: body.name,
            os: body.os,
            online: body.online,
            agent_ok: body.agent_ok,
            self_node: body.self_node,
            read_only: body.read_only,
            read_roots: body.read_roots,
        }
    }
}

#[derive(Deserialize)]
struct ListBody {
    #[serde(default, deserialize_with = "vec_or_empty")]
    entries: Vec<EntryBody>,
}

#[derive(Deserialize)]
struct EntryBody {
    #[serde(default)]
    name: String,
    #[serde(default)]
    path: String,
    #[serde(default, rename = "isDir")]
    is_dir: bool,
    #[serde(default)]
    size: i64,
    #[serde(default, rename = "modTime")]
    mod_time: String,
}

impl From<EntryBody> for Entry {
    fn from(body: EntryBody) -> Self {
        Self {
            name: body.name,
            path: body.path,
            is_dir: body.is_dir,
            size: body.size,
            mod_time: body.mod_time,
        }
    }
}

#[derive(Deserialize)]
struct PreviewBody {
    #[serde(default)]
    kind: String,
    #[serde(default)]
    name: String,
    #[serde(default)]
    text: String,
    #[serde(default)]
    truncated: bool,
}

impl From<PreviewBody> for Preview {
    fn from(body: PreviewBody) -> Self {
        Self {
            kind: body.kind,
            name: body.name,
            text: body.text,
            truncated: body.truncated,
        }
    }
}

#[derive(Deserialize)]
struct TransfersBody {
    #[serde(default, deserialize_with = "vec_or_empty")]
    transfers: Vec<TransferBody>,
}

#[derive(Deserialize)]
struct TransferBody {
    #[serde(default)]
    id: String,
    #[serde(default)]
    mode: String,
    #[serde(default, rename = "fromNode")]
    from_node: String,
    #[serde(default, rename = "fromPath")]
    from_path: String,
    #[serde(default, rename = "toNode")]
    to_node: String,
    #[serde(default, rename = "toPath")]
    to_path: String,
    #[serde(default)]
    job: JobBody,
}

impl From<TransferBody> for Transfer {
    fn from(body: TransferBody) -> Self {
        Self {
            id: body.id,
            mode: body.mode,
            from_node: body.from_node,
            from_path: body.from_path,
            to_node: body.to_node,
            to_path: body.to_path,
            job: body.job.into(),
        }
    }
}

#[derive(Deserialize)]
struct JobsBody {
    #[serde(default, deserialize_with = "vec_or_empty")]
    jobs: Vec<JobBody>,
}

#[derive(Deserialize, Default)]
struct JobBody {
    #[serde(default)]
    id: String,
    #[serde(default)]
    mode: String,
    #[serde(default)]
    status: String,
    #[serde(default, rename = "bytesTotal")]
    bytes_total: i64,
    #[serde(default, rename = "bytesDone")]
    bytes_done: i64,
    #[serde(default)]
    current: String,
    #[serde(default, rename = "destPath")]
    dest_path: String,
    #[serde(default)]
    error: String,
    #[serde(default)]
    conflict: Option<ConflictBody>,
}

#[derive(Deserialize, Default)]
struct ConflictBody {
    #[serde(default)]
    path: String,
}

impl From<JobBody> for Job {
    fn from(body: JobBody) -> Self {
        Self {
            id: body.id,
            mode: body.mode,
            status: body.status,
            bytes_total: body.bytes_total,
            bytes_done: body.bytes_done,
            current: body.current,
            dest_path: body.dest_path,
            error: body.error,
            conflict_path: body.conflict.map(|item| item.path).unwrap_or_default(),
        }
    }
}

#[derive(Deserialize)]
struct SettingsBody {
    #[serde(default)]
    net: String,
    #[serde(default, rename = "netRequested")]
    net_requested: String,
    #[serde(default, rename = "authKeyConfigured")]
    auth_key_configured: bool,
}

impl From<SettingsBody> for Settings {
    fn from(body: SettingsBody) -> Self {
        Self {
            net: body.net,
            net_requested: body.net_requested,
            auth_key_configured: body.auth_key_configured,
        }
    }
}

#[derive(Deserialize)]
struct AuthBody {
    #[serde(default)]
    configured: bool,
    #[serde(default, rename = "restartRequired")]
    restart_required: bool,
}

impl From<AuthBody> for AuthKeyStatus {
    fn from(body: AuthBody) -> Self {
        Self {
            configured: body.configured,
            restart_required: body.restart_required,
        }
    }
}

#[derive(Deserialize)]
struct CacheBody {
    #[serde(default, rename = "localPath")]
    local_path: String,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn null_lists_decode_as_empty() {
        let parsed: NodesBody = serde_json::from_str(r#"{"nodes":null}"#).unwrap();
        assert!(parsed.nodes.is_empty());
    }

    #[test]
    fn error_body_is_the_message() {
        let message = error_message(403, br#"{"error":"origin is not a tailfile gui"}"#);
        assert_eq!(message, "origin is not a tailfile gui");
    }

    #[test]
    fn auth_status_has_no_key_field() {
        let status: AuthBody =
            serde_json::from_str(r#"{"configured":true,"restartRequired":true}"#).unwrap();
        let rendered = serde_json::to_string(&serde_json::json!({
            "configured": status.configured,
            "restartRequired": status.restart_required,
        }))
        .unwrap();
        assert!(!rendered.contains("tskey"));
        assert!(!rendered.contains("authKey"));
    }
}
