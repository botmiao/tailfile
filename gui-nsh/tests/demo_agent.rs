//! Drives the HTTP client against `tailfile-agent -demo`.
//! Skips when Go is not on PATH. The window is not opened.

use std::io::{Read, Write};
use std::net::TcpListener;
use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::time::{Duration, Instant};

use std::sync::Mutex;
use tailfile_nsh::api::Client;
use tailfile_nsh::token;

static DEMO_LOCK: Mutex<()> = Mutex::new(());

struct Demo {
    child: Child,
    base: String,
    state: PathBuf,
}

impl Demo {
    fn start() -> Option<Self> {
        if Command::new("go").arg("version").output().is_err() {
            return None;
        }
        let repo = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("..");
        let agent_dir = repo.join("agent");
        let bin = std::env::temp_dir().join(format!("tailfile-agent-nsh-test-{}", std::process::id()));
        let build = Command::new("go")
            .args(["build", "-o"])
            .arg(&bin)
            .arg("./cmd/tailfile-agent")
            .current_dir(&agent_dir)
            .env("GOWORK", "off")
            .output()
            .ok()?;
        if !build.status.success() {
            eprintln!("{}", String::from_utf8_lossy(&build.stderr));
            return None;
        }
        let listener = TcpListener::bind("127.0.0.1:0").ok()?;
        let port = listener.local_addr().ok()?.port();
        drop(listener);
        let state = std::env::temp_dir().join(format!("tailfile-nsh-demo-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&state);
        std::fs::create_dir_all(&state).ok()?;
        let log = std::fs::File::create(state.join("agent.log")).ok()?;
        let mut child = Command::new(&bin)
            .args([
                "-demo",
                "-state",
                state.to_str()?,
                "-listen",
                &format!("127.0.0.1:{port}"),
            ])
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(log)
            .spawn()
            .ok()?;
        let base = format!("http://127.0.0.1:{port}");
        let deadline = Instant::now() + Duration::from_secs(15);
        loop {
            if token::load("", state.to_str().unwrap_or("")).is_ok() {
                break;
            }
            if Instant::now() > deadline {
                let _ = child.kill();
                let _ = child.wait();
                return None;
            }
            if let Ok(Some(_)) = child.try_wait() {
                return None;
            }
            std::thread::sleep(Duration::from_millis(50));
        }
        Some(Self { child, base, state })
    }

    fn client(&self) -> Client {
        let token = token::load("", self.state.to_str().unwrap()).expect("token");
        assert!(!token.is_empty());
        Client::new(&self.base, token)
    }
}

impl Drop for Demo {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
        let _ = std::fs::remove_dir_all(&self.state);
    }
}

#[test]
fn demo_lists_previews_copies_and_hides_the_auth_key() {
    let _guard = DEMO_LOCK.lock().unwrap_or_else(|e| e.into_inner());
    let Some(demo) = Demo::start() else {
        eprintln!("skip: go or tailfile-agent is unavailable");
        return;
    };
    let client = demo.client();
    client.health().expect("health");
    let nodes = client.nodes().expect("nodes");
    let names: Vec<_> = nodes.iter().map(|node| node.name.as_str()).collect();
    assert!(names.contains(&"alpha"), "{names:?}");
    assert!(names.contains(&"beta"), "{names:?}");
    assert!(names.contains(&"gamma"), "{names:?}");
    let alpha = nodes.iter().find(|node| node.name == "alpha").unwrap();
    let beta = nodes.iter().find(|node| node.name == "beta").unwrap();
    let gamma = nodes.iter().find(|node| node.name == "gamma").unwrap();
    assert!(!alpha.read_only);
    assert!(gamma.read_only);

    let root = alpha.read_roots[0].clone();
    let entries = client.list(&alpha.id, &root).unwrap();
    assert!(entries.iter().any(|entry| entry.name == "hello.txt"));
    let hello = entries.iter().find(|entry| entry.name == "hello.txt").unwrap();
    let preview = client.preview(&alpha.id, &hello.path).unwrap();
    assert_eq!(preview.kind, "text");
    assert!(preview.text.contains("Hello from alpha"));

    let notes = entries.iter().find(|entry| entry.name == "notes").unwrap();
    let note_entries = client.list(&alpha.id, &notes.path).unwrap();
    let pdf = note_entries.iter().find(|entry| entry.name == "sample.pdf").unwrap();
    let pdf_preview = client.preview(&alpha.id, &pdf.path).unwrap();
    assert_eq!(pdf_preview.kind, "pdf");
    assert!(pdf_preview.text.contains("Tailfile PDF preview"), "{}", pdf_preview.text);

    let photos = entries.iter().find(|entry| entry.name == "photos").unwrap();
    let photo_entries = client.list(&alpha.id, &photos.path).unwrap();
    let png = photo_entries.iter().find(|entry| entry.name == "swatch.png").unwrap();
    let bytes = client.read(&alpha.id, &png.path).unwrap();
    assert!(bytes.starts_with(b"\x89PNG"));

    let beta_root = beta.read_roots[0].clone();
    let transfer = client
        .start_transfer("push", &alpha.id, &hello.path, &beta.id, &token::join_remote(&beta_root, "hello.txt"))
        .unwrap();
    wait_status(&client, &transfer.id, "completed");
    let again = client
        .start_transfer("push", &alpha.id, &hello.path, &beta.id, &token::join_remote(&beta_root, "hello.txt"))
        .unwrap();
    wait_status(&client, &again.id, "conflict");
    client.answer_transfer_conflict(&again.id, "skip", true).unwrap();
    wait_status(&client, &again.id, "completed");

    let denied = client
        .start_transfer(
            "push",
            &alpha.id,
            &hello.path,
            &gamma.id,
            &token::join_remote(&gamma.read_roots[0], "hello.txt"),
        )
        .unwrap();
    let final_status = wait_not_running(&client, &denied.id);
    assert!(final_status == "error" || final_status == "canceled", "{final_status}");

    let settings = client.settings().unwrap();
    assert_eq!(settings.net, "demo");
    assert!(!settings.auth_key_configured);
    let secret = "tskey-auth-nsh-probe-do-not-log";
    let saved = client.set_auth_key(secret).unwrap();
    assert!(saved.configured && saved.restart_required);
    let raw = std::fs::read_to_string(demo.state.join("ts-authkey")).unwrap();
    assert_eq!(raw.trim(), secret);
    let body = probe_settings(&demo);
    assert!(!body.contains(secret));
    assert!(body.contains("\"authKeyConfigured\":true") || body.contains("\"authKeyConfigured\": true"));
    let cleared = client.clear_auth_key().unwrap();
    assert!(!cleared.configured && cleared.restart_required);
    assert!(!demo.state.join("ts-authkey").exists());
}

fn wait_status(client: &Client, id: &str, want: &str) {
    let got = wait_not_running(client, id);
    assert_eq!(got, want, "transfer {id}");
}

fn wait_not_running(client: &Client, id: &str) -> String {
    let deadline = Instant::now() + Duration::from_secs(20);
    let mut last = String::new();
    while Instant::now() < deadline {
        let transfers = client.transfers().unwrap_or_default();
        if let Some(transfer) = transfers.iter().find(|item| item.id == id) {
            last = transfer.job.status.clone();
            if last != "queued" && last != "running" {
                return last;
            }
        }
        std::thread::sleep(Duration::from_millis(50));
    }
    panic!("transfer {id} stayed {last}");
}

fn probe_settings(demo: &Demo) -> String {
    let token = token::load("", demo.state.to_str().unwrap()).unwrap();
    let mut stream = std::net::TcpStream::connect(demo.base.trim_start_matches("http://")).unwrap();
    write!(
        stream,
        "GET /v1/local/settings HTTP/1.1\r\nHost: 127.0.0.1\r\nAuthorization: Bearer {token}\r\nConnection: close\r\n\r\n"
    )
    .unwrap();
    let mut body = String::new();
    stream.read_to_string(&mut body).unwrap();
    body
}

#[test]
fn demo_mkdir_rename_delete() {
    let _guard = DEMO_LOCK.lock().unwrap_or_else(|e| e.into_inner());
    let Some(demo) = Demo::start() else {
        eprintln!("skip: go or tailfile-agent is unavailable");
        return;
    };
    let client = demo.client();
    let nodes = client.nodes().expect("nodes");
    let alpha = nodes
        .iter()
        .find(|n| n.name.contains("alpha") || n.id.contains("alpha"))
        .or_else(|| nodes.iter().find(|n| !n.read_only))
        .expect("writable node");
    let root = alpha.read_roots.first().cloned().expect("root");
    let folder = format!("{}/nsh-ui-test-dir", root.trim_end_matches('/'));
    client.mkdir(&alpha.id, &folder).expect("mkdir");
    let renamed = format!("{}/nsh-ui-test-dir-renamed", root.trim_end_matches('/'));
    client.rename(&alpha.id, &folder, &renamed).expect("rename");
    client.delete(&alpha.id, &renamed).expect("delete");
    let listing = client.list(&alpha.id, &root).expect("list");
    assert!(
        listing.iter().all(|e| e.path != renamed && e.path != folder),
        "deleted folder should be gone"
    );
}
