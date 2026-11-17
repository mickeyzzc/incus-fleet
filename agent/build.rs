fn main() -> Result<(), Box<dyn std::error::Error>> {
    println!("cargo:rerun-if-changed=../api/agent/v1/agent.proto");
    prost_build::Config::new().compile_protos(&["../api/agent/v1/agent.proto"], &["../api"])?;
    Ok(())
}
