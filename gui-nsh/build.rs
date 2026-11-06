//! Embeds `assets/tailfile-app-icon.ico` as Win32 icon resource ID `1`
//! (`RT_GROUP_ICON` = 14, ordinal 1) when targeting Windows.
//!
//! `nsh-platform-windows` registers `NshWindow` with
//! `LoadIconW(Some(hinstance), PCWSTR(1 as _))`, and Windows Explorer / Taskbar
//! also use the first icon group resource (ID 1) as the executable's icon.
//! Generating the `.res` binary directly in Rust avoids requiring `rc.exe` in
//! `PATH` while allowing MSVC `link.exe` / `lld-link` to link the resource
//! natively.

use std::env;
use std::fs;
use std::path::PathBuf;

fn main() {
    println!("cargo:rerun-if-changed=assets/tailfile-app-icon.ico");
    println!("cargo:rerun-if-changed=assets/tailfile-app-icon.svg");

    let target_os = env::var("CARGO_CFG_TARGET_OS").unwrap_or_default();
    if target_os != "windows" {
        return;
    }

    let manifest_dir = PathBuf::from(env::var("CARGO_MANIFEST_DIR").expect("CARGO_MANIFEST_DIR"));
    let ico_path = manifest_dir.join("assets").join("tailfile-app-icon.ico");
    let ico = fs::read(&ico_path).expect("failed to read assets/tailfile-app-icon.ico");
    let res = ico_to_win32_res(&ico);

    let out_dir = PathBuf::from(env::var("OUT_DIR").expect("OUT_DIR"));
    let res_path = out_dir.join("tailfile-app-icon.res");
    fs::write(&res_path, res).expect("failed to write tailfile-app-icon.res");

    let target_env = env::var("CARGO_CFG_TARGET_ENV").unwrap_or_default();
    if target_env == "gnu" {
        // GNU ld accepts `.res` files directly as input objects.
        println!("cargo:rustc-link-arg-bins={}", res_path.display());
    } else {
        // MSVC link.exe / lld-link accepts `.res` files directly and converts
        // them via cvtres.
        println!("cargo:rustc-link-arg-bins={}", res_path.display());
    }
}

fn read_u16_le(buf: &[u8], offset: usize) -> u16 {
    u16::from_le_bytes([buf[offset], buf[offset + 1]])
}

fn read_u32_le(buf: &[u8], offset: usize) -> u32 {
    u32::from_le_bytes([
        buf[offset],
        buf[offset + 1],
        buf[offset + 2],
        buf[offset + 3],
    ])
}

fn push_u16_le(out: &mut Vec<u8>, v: u16) {
    out.extend_from_slice(&v.to_le_bytes());
}

fn push_u32_le(out: &mut Vec<u8>, v: u32) {
    out.extend_from_slice(&v.to_le_bytes());
}

fn pad_dword(out: &mut Vec<u8>) {
    while !out.len().is_multiple_of(4) {
        out.push(0);
    }
}

fn write_res_header(
    out: &mut Vec<u8>,
    data_size: u32,
    type_id: u16,
    name_id: u16,
    memory_flags: u16,
    language_id: u16,
) {
    push_u32_le(out, data_size);
    push_u32_le(out, 32); // HeaderSize = 32 bytes
    push_u16_le(out, 0xFFFF);
    push_u16_le(out, type_id);
    push_u16_le(out, 0xFFFF);
    push_u16_le(out, name_id);
    push_u32_le(out, 0); // DataVersion
    push_u16_le(out, memory_flags);
    push_u16_le(out, language_id);
    push_u32_le(out, 0); // Version
    push_u32_le(out, 0); // Characteristics
}

fn ico_to_win32_res(ico: &[u8]) -> Vec<u8> {
    assert!(ico.len() >= 6, "invalid ICO header");
    let reserved = read_u16_le(ico, 0);
    let ico_type = read_u16_le(ico, 2);
    let count = read_u16_le(ico, 4);
    assert!(reserved == 0 && ico_type == 1 && count > 0, "invalid ICO file");

    let mut out = Vec::with_capacity(ico.len() + 512);
    // 32-byte null resource entry marking a 32-bit `.res` stream.
    write_res_header(&mut out, 0, 0, 0, 0, 0);

    // GRPICONDIR header: idReserved=0, idType=1, idCount=count.
    let mut grp = Vec::with_capacity(6 + (count as usize) * 14);
    push_u16_le(&mut grp, 0);
    push_u16_le(&mut grp, 1);
    push_u16_le(&mut grp, count);

    for i in 0..(count as usize) {
        let entry_off = 6 + i * 16;
        let entry = &ico[entry_off..entry_off + 16];
        let size = read_u32_le(entry, 8) as usize;
        let offset = read_u32_le(entry, 12) as usize;
        let icon_id = (i + 1) as u16;

        // RT_ICON = 3, MemoryFlags = 0x1010 (MOVEABLE | PURE), LanguageId = 0x0409.
        write_res_header(&mut out, size as u32, 3, icon_id, 0x1010, 0x0409);
        out.extend_from_slice(&ico[offset..offset + size]);
        pad_dword(&mut out);

        // GRPICONDIRENTRY: first 12 bytes of ICONDIRENTRY + u16 nID.
        grp.extend_from_slice(&entry[..12]);
        push_u16_le(&mut grp, icon_id);
    }

    // RT_GROUP_ICON = 14, ordinal ID = 1, MemoryFlags = 0x1030, LanguageId = 0x0409.
    write_res_header(&mut out, grp.len() as u32, 14, 1, 0x1030, 0x0409);
    out.extend_from_slice(&grp);
    pad_dword(&mut out);

    out
}
