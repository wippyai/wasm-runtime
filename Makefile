.PHONY: build-testbed check-testbed build-core-host

build-core-host: ## Rebuild the core-module host-import fixture (needs wasi-sdk)
	$(WASI_SDK_PATH)/bin/clang -mexec-model=reactor -O2 -nostartfiles \
		-Wl,--no-entry -Wl,--export=run \
		-o testbed/core-host.wasm testbed/fixtures/core-host.c

check-testbed: ## Verify Rust toolchain for test components
	rustup target list --installed | grep -q wasm32-wasip2 || rustup target add wasm32-wasip2

build-testbed: check-testbed ## Build all test WASM components from Rust source
	cd testbed/crates && cargo build --release --target wasm32-wasip2
	cp testbed/crates/target/wasm32-wasip2/release/hello_http.wasm testbed/hello_http.wasm
