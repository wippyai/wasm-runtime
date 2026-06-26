.PHONY: build-testbed check-testbed

check-testbed: ## Verify Rust toolchain for test components
	rustup target list --installed | grep -q wasm32-wasip2 || rustup target add wasm32-wasip2

build-testbed: check-testbed ## Build all test WASM components from Rust source
	cd testbed/crates && cargo build --release --target wasm32-wasip2
	cp testbed/crates/target/wasm32-wasip2/release/hello_http.wasm testbed/hello_http.wasm
