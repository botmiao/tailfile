//! Logo mark (dotted F) helpers and third-party icon license pointer.
//! Lucide SVG icons are not vendored wholesale in this MVP; toolbar uses
//! short text labels. Notices are retained for when Lucide assets land.

pub const LOGO_LABEL: &str = "F";
pub const LOGO_TITLE: &str = "Tailfile";

/// Orange used for the dotted-F mark (light / dark accents).
pub fn logo_hex(dark: bool) -> &'static str {
    if dark {
        "#FB923C"
    } else {
        "#EA580C"
    }
}

pub const THIRD_PARTY_NOTICES: &str = r#"Lucide Icons — ISC License
Copyright (c) 2026 Lucide Icons and Contributors

Feather-derived Lucide icons — MIT License
Copyright (c) 2013-present Cole Bemis

Full texts: see docs/NSH_UI.md and the approved UX prototype README.
"#;

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn notices_mention_licenses() {
        assert!(THIRD_PARTY_NOTICES.contains("ISC"));
        assert!(THIRD_PARTY_NOTICES.contains("MIT"));
    }
}
