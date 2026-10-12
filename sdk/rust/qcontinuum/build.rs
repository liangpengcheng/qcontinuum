use std::path::PathBuf;

fn main() {
    // protoc: prefer the repo-bundled Linux toolchain, fall back to PATH.
    let manifest_dir = PathBuf::from(std::env::var("CARGO_MANIFEST_DIR").unwrap());
    let bundled = manifest_dir.join("../../../tools/protobuf/linux/protoc");
    if bundled.exists() {
        std::env::set_var("PROTOC", &bundled);
    }

    let proto_dir = manifest_dir.join("../../../proto");
    let proto_file = proto_dir.join("test.proto");

    prost_build::Config::new()
        .compile_protos(&[proto_file], &[proto_dir])
        .expect("prost codegen failed");

    println!("cargo:rerun-if-changed=../../../proto/test.proto");
}
