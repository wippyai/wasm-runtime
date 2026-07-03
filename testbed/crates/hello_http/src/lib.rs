wit_bindgen::generate!({
    path: "wit",
    world: "hello-http",
    with: {
        "wasi:clocks/monotonic-clock@0.2.8": generate,
        "wasi:io/error@0.2.8": generate,
        "wasi:io/poll@0.2.8": generate,
        "wasi:io/streams@0.2.8": generate,
        "wasi:http/types@0.2.8": generate,
    },
});

struct Component;

impl Guest for Component {
    fn run() -> String {
        "Hello from HTTP WASM component".to_string()
    }
}

impl exports::wasi::http::incoming_handler::Guest for Component {
    fn handle(
        _request: wasi::http::types::IncomingRequest,
        _response_out: wasi::http::types::ResponseOutparam,
    ) {
        let fields = wasi::http::types::Fields::new();
        let response = wasi::http::types::OutgoingResponse::new(fields);
        let _ = response.set_status_code(200);
    }
}

export!(Component);
