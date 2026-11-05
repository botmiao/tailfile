//! Folder/file name validation shared by new-folder and rename.

pub fn validate_name(name: &str, existing: &[String], windows_rules: bool) -> Result<(), String> {
    let name = name.trim();
    if name.is_empty() {
        return Err("名称不能为空".into());
    }
    if name == "." || name == ".." {
        return Err("不能使用 . 或 ..".into());
    }
    if name.as_bytes().len() > 255 {
        return Err("名称超过 255 字节".into());
    }
    if name.contains('/') || name.contains('\\') || name.contains('\0') {
        return Err("名称包含非法字符".into());
    }
    if windows_rules {
        if name.chars().any(|c| "<>:\"|?*".contains(c)) {
            return Err("名称包含 Windows 非法字符".into());
        }
        if name.ends_with(' ') || name.ends_with('.') {
            return Err("名称不能以空格或点结尾".into());
        }
        let stem = name.split('.').next().unwrap_or(name);
        const RESERVED: &[&str] = &[
            "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7",
            "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9",
        ];
        if RESERVED.iter().any(|r| stem.eq_ignore_ascii_case(r)) {
            return Err("名称是 Windows 保留名".into());
        }
    }
    let conflict = if windows_rules {
        existing.iter().any(|e| e.eq_ignore_ascii_case(name))
    } else {
        existing.iter().any(|e| e == name)
    };
    if conflict {
        return Err("已存在同名项".into());
    }
    Ok(())
}

pub fn unique_folder_name(existing: &[String]) -> String {
    let base = "新建文件夹";
    if !existing.iter().any(|e| e == base) {
        return base.into();
    }
    for n in 2..10_000 {
        let candidate = format!("{base} {n}");
        if !existing.iter().any(|e| e == &candidate) {
            return candidate;
        }
    }
    format!("{base}-{}", existing.len() + 1)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn rejects_empty_and_dup() {
        assert!(validate_name("", &[], false).is_err());
        assert!(validate_name("a", &["a".into()], false).is_err());
        assert!(validate_name("ok", &["a".into()], false).is_ok());
        assert!(validate_name("A", &["a".into()], true).is_err());
    }

    #[test]
    fn unique_increments() {
        assert_eq!(unique_folder_name(&[]), "新建文件夹");
        assert_eq!(
            unique_folder_name(&["新建文件夹".into()]),
            "新建文件夹 2"
        );
    }
}
