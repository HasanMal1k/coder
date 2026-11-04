// Generates the protocol's Rust code from the same file the Go side is
// generated from, so the two cannot drift apart. Needs `protoc` on the path.
fn main() -> Result<(), Box<dyn std::error::Error>> {
    let proto = "../internal/linkapi/link.proto";
    println!("cargo:rerun-if-changed={proto}");

    tonic_prost_build::configure().compile_protos(&[proto], &["../internal/linkapi"])?;

    Ok(())
}
