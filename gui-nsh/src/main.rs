//! Desktop entry. `cargo run` and the built binary parse the same flags as
//! the other Tailfile shells. `nsh run` mounts `ui::App` without this file;
//! that path uses the default agent address and the default state directory
//! (or `TAILFILE_AGENT` / `TAILFILE_STATE` / `TAILFILE_TOKEN_FILE`).

#[cfg(target_os = "linux")]
use nsh_platform_linux as platform;
#[cfg(target_os = "macos")]
use nsh_platform_macos as platform;
#[cfg(target_os = "windows")]
use nsh_platform_windows as platform;

#[cfg(any(target_os = "linux", target_os = "macos", target_os = "windows"))]
fn main() {
    env_logger::Builder::from_env(env_logger::Env::default().default_filter_or("info")).init();
    match tailfile_nsh::Launch::parse(std::env::args().skip(1)) {
        Ok(launch) => tailfile_nsh::set_launch(launch),
        Err(message) => {
            eprintln!("{message}");
            std::process::exit(2);
        }
    }
    let _ = platform::run_app(
        platform::AppSource::Rust(
            platform::Root::of::<tailfile_nsh::ui::App>().tokens(tailfile_nsh::ui::theme_tokens()),
        ),
        platform::AppOptions {
            title: "Tailfile".to_owned(),
            #[cfg(any(target_os = "linux", target_os = "windows"))]
            app_id: "dev.iori.tailfile.nsh".to_owned(),
            size: (1440, 900),
            ..Default::default()
        },
    );
    tailfile_nsh::force_exit(0);
}

#[cfg(not(any(target_os = "linux", target_os = "macos", target_os = "windows")))]
fn main() {
    compile_error!("tailfile-nsh runs on Linux, Windows, and macOS");
}
