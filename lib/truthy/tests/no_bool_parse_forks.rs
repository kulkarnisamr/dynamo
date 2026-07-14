// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

//! Grep-guard: no hand-rolled truthy/bool string parsing outside this crate.
//!
//! Dynamo once had 8+ divergent copies of `matches!(v, "1" | "true" | ...)`
//! with different accepted sets, so `SOMEFLAG=on` worked for some flags and
//! silently not others. All bool parsing of user-supplied strings
//! must go through `dynamo-truthy` (re-exported as
//! `dynamo_runtime::config::{is_truthy, is_falsey, parse_bool, env_is_truthy,
//! env_is_falsey}`). If this test flags your new code, call those helpers
//! instead; if the match is a false positive (e.g. a string that genuinely is
//! not a boolean flag), add it to `ALLOWED` with a justification.

use std::path::{Path, PathBuf};

/// Known non-fork matches, as (path suffix, line substring) pairs.
const ALLOWED: &[(&str, &str)] = &[];

/// Substring patterns that indicate a hand-rolled bool parser.
const FORK_PATTERNS: &[&str] = &[
    r#"eq_ignore_ascii_case("true")"#,
    r#"eq_ignore_ascii_case("false")"#,
    r#"== "true""#,
    r#"== "false""#,
    r#"!= "true""#,
    r#"!= "false""#,
    r#""true" |"#,
    r#"| "true""#,
    r#""false" |"#,
    r#"| "false""#,
];

fn scan_dir(dir: &Path, offenders: &mut Vec<String>) {
    for entry in std::fs::read_dir(dir).unwrap_or_else(|e| panic!("read_dir {dir:?}: {e}")) {
        let path = entry.expect("dir entry").path();
        let name = path.file_name().and_then(|n| n.to_str()).unwrap_or("");
        if path.is_dir() {
            // Skip build output and the canonical implementation itself.
            if name == "target" || name == "node_modules" || path.ends_with("lib/truthy") {
                continue;
            }
            scan_dir(&path, offenders);
        } else if name.ends_with(".rs") {
            scan_file(&path, offenders);
        }
    }
}

fn scan_file(path: &Path, offenders: &mut Vec<String>) {
    let Ok(content) = std::fs::read_to_string(path) else {
        return;
    };
    for (idx, line) in content.lines().enumerate() {
        let is_fork = FORK_PATTERNS.iter().any(|p| line.contains(p))
            || (line.contains("matches!") && line.contains(r#""true""#));
        if !is_fork {
            continue;
        }
        let allowed = ALLOWED.iter().any(|(suffix, substr)| {
            path.to_string_lossy().ends_with(suffix) && line.contains(substr)
        });
        if !allowed {
            offenders.push(format!("{}:{}: {}", path.display(), idx + 1, line.trim()));
        }
    }
}

#[test]
fn no_bool_parse_forks_outside_canonical_crate() {
    // lib/truthy/ -> repo root
    let repo_root = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .parent()
        .and_then(Path::parent)
        .expect("repo root")
        .to_path_buf();
    // Only meaningful inside the repo checkout (not a published crate).
    if !repo_root.join("Cargo.toml").is_file() || !repo_root.join("lib").is_dir() {
        eprintln!("skipping: not running inside the dynamo repo");
        return;
    }

    let mut offenders = Vec::new();
    for dir in ["lib", "deploy/inference-gateway"] {
        let dir = repo_root.join(dir);
        if dir.is_dir() {
            scan_dir(&dir, &mut offenders);
        }
    }

    assert!(
        offenders.is_empty(),
        "Hand-rolled truthy/bool parsing found; use dynamo_runtime::config::{{is_truthy, \
         parse_bool, env_is_truthy}} (or dynamo-truthy directly if the crate cannot depend on \
         dynamo-runtime):\n{}",
        offenders.join("\n")
    );
}
