//! Tailfile's Native Shell window. The agent stays a separate process.
//! This crate is an HTTP client of `http://127.0.0.1:8645`.

pub mod api;
mod launch;
mod model;
pub mod token;
pub mod ui;

pub use launch::{Launch, set_launch};

use nsh_core::prelude::*;

/// What a TSX entry could import. The window itself is the Rust root.
#[native_module("tailfile-nsh/native")]
fn native(registry: &mut Registry) {
    registry.register::<ui::App>();
}
