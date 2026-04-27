// build.rs — generates Rust types from proto/milliways.json via typify.
//
// Output: $OUT_DIR/generated.rs, included by src/rpc/generated.rs.
//
// If typify fails (network issue, schema feature unsupported), we fall
// back to writing an empty module — the hand-mirrored types in
// src/rpc/types.rs still compile, and the build doesn't break.
// A diagnostic line is printed via cargo:warning= so the issue is visible.

use std::env;
use std::fs;
use std::path::PathBuf;

fn main() {
    let manifest = PathBuf::from(env::var("CARGO_MANIFEST_DIR").unwrap());
    // crates/milliways-term/milliways/Cargo.toml -> repo root is up 4 levels
    let schema = manifest
        .join("..")
        .join("..")
        .join("..")
        .join("proto")
        .join("milliways.json");

    println!("cargo:rerun-if-changed={}", schema.display());

    let out_dir = PathBuf::from(env::var("OUT_DIR").unwrap());
    let out_path = out_dir.join("generated.rs");

    let result = run_typify(&schema);
    match result {
        Ok(code) => {
            fs::write(&out_path, code).expect("write generated.rs");
        }
        Err(e) => {
            // Fall back to an empty module so the rest of the crate compiles.
            println!("cargo:warning=typify codegen failed: {}", e);
            println!("cargo:warning=falling back to hand-mirrored types in src/rpc/types.rs");
            fs::write(
                &out_path,
                "// typify codegen failed at build time; types come from src/rpc/types.rs\n",
            )
            .expect("write fallback generated.rs");
        }
    }
}

fn run_typify(schema_path: &PathBuf) -> anyhow::Result<String> {
    use anyhow::Context;
    use typify::{TypeSpace, TypeSpaceSettings};

    let schema_text =
        fs::read_to_string(schema_path).with_context(|| format!("read {}", schema_path.display()))?;
    let schema: schemars::schema::RootSchema =
        serde_json::from_str(&schema_text).context("parse schema")?;

    let mut settings = TypeSpaceSettings::default();
    settings.with_struct_builder(false);
    let mut ts = TypeSpace::new(&settings);
    ts.add_root_schema(schema).context("add_root_schema")?;

    let tokens = ts.to_stream();
    let formatted = match syn::parse2::<syn::File>(tokens.clone()) {
        Ok(f) => prettyplease::unparse(&f),
        Err(_) => tokens.to_string(),
    };
    Ok(formatted)
}
