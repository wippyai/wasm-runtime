package http

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

// OutgoingHandlerNamespace is the WASI HTTP outgoing handler namespace.
const OutgoingHandlerNamespace = "wasi:http/outgoing-handler@0.2.8"

// Outgoing handler resource type IDs (110-113 range)
const (
	resourceTypeOutgoingRequest        = preview2.ResourceType(110)
	resourceTypeRequestBody            = preview2.ResourceType(111)
	resourceTypeFutureIncomingResponse = preview2.ResourceType(112)
	resourceTypeIncomingResponse       = preview2.ResourceType(113)
)

// OutgoingHandlerHost implements wasi:http/outgoing-handler@0.2.8
type OutgoingHandlerHost struct {
	resources *preview2.ResourceTable
	client    *http.Client
}

// NewOutgoingHandlerHost creates a new outgoing handler host.
func NewOutgoingHandlerHost(res *preview2.ResourceTable) *OutgoingHandlerHost {
	return &OutgoingHandlerHost{
		resources: res,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Namespace returns the WASI namespace.
func (h *OutgoingHandlerHost) Namespace() string {
	return OutgoingHandlerNamespace
}

// OutgoingRequest resource
type outgoingRequestResource struct {
	url     *url.URL
	headers map[string][]string
	body    *bytes.Buffer
	method  string
}

func (r *outgoingRequestResource) Type() preview2.ResourceType { return resourceTypeOutgoingRequest }
func (r *outgoingRequestResource) Drop()                       {}

// OutgoingRequest constructors and methods

// ConstructorOutgoingRequest creates a new outgoing request.
// [constructor]outgoing-request(headers: headers) -> outgoing-request
func (h *OutgoingHandlerHost) ConstructorOutgoingRequest(_ context.Context, headersHandle uint32) uint32 {
	headers := make(map[string][]string)
	if r, ok := h.resources.Get(headersHandle); ok {
		if fields, ok := r.(*fieldsResource); ok {
			for k, v := range fields.Values() {
				headers[k] = append([]string{}, v...)
			}
		}
	}

	req := &outgoingRequestResource{
		method:  "GET",
		url:     &url.URL{Scheme: "http"},
		headers: headers,
		body:    &bytes.Buffer{},
	}
	return h.resources.Add(req)
}

// MethodOutgoingRequestSetMethod sets the method.
// [method]outgoing-request.set-method(method: method) -> result
func (h *OutgoingHandlerHost) MethodOutgoingRequestSetMethod(_ context.Context, self uint32, method string) uint32 {
	r, ok := h.resources.Get(self)
	if !ok {
		return 1
	}
	req, ok := r.(*outgoingRequestResource)
	if !ok {
		return 1
	}
	req.method = method
	return 0
}

// MethodOutgoingRequestSetPathWithQuery sets path and query.
// [method]outgoing-request.set-path-with-query(path-with-query: option<string>) -> result
func (h *OutgoingHandlerHost) MethodOutgoingRequestSetPathWithQuery(_ context.Context, self uint32, hasPath bool, path string) uint32 {
	r, ok := h.resources.Get(self)
	if !ok {
		return 1
	}
	req, ok := r.(*outgoingRequestResource)
	if !ok {
		return 1
	}
	if hasPath {
		req.url.Path = path
	}
	return 0
}

// MethodOutgoingRequestSetScheme sets the scheme.
// [method]outgoing-request.set-scheme(scheme: option<scheme>) -> result
func (h *OutgoingHandlerHost) MethodOutgoingRequestSetScheme(_ context.Context, self uint32, hasScheme bool, scheme uint8) uint32 {
	r, ok := h.resources.Get(self)
	if !ok {
		return 1
	}
	req, ok := r.(*outgoingRequestResource)
	if !ok {
		return 1
	}
	if hasScheme {
		if scheme == 1 {
			req.url.Scheme = "https"
		} else {
			req.url.Scheme = "http"
		}
	}
	return 0
}

// MethodOutgoingRequestSetAuthority sets the authority (host).
// [method]outgoing-request.set-authority(authority: option<string>) -> result
func (h *OutgoingHandlerHost) MethodOutgoingRequestSetAuthority(_ context.Context, self uint32, hasAuth bool, authority string) uint32 {
	r, ok := h.resources.Get(self)
	if !ok {
		return 1
	}
	req, ok := r.(*outgoingRequestResource)
	if !ok {
		return 1
	}
	if hasAuth {
		req.url.Host = authority
	}
	return 0
}

// MethodOutgoingRequestHeaders returns the request headers.
// [method]outgoing-request.headers() -> headers
func (h *OutgoingHandlerHost) MethodOutgoingRequestHeaders(_ context.Context, self uint32) uint32 {
	r, ok := h.resources.Get(self)
	if !ok {
		return h.resources.Add(preview2.NewFieldsResource())
	}
	req, ok := r.(*outgoingRequestResource)
	if !ok {
		return h.resources.Add(preview2.NewFieldsResource())
	}
	fields := preview2.NewFieldsResource()
	for k, vs := range req.headers {
		for _, v := range vs {
			fields.Append(k, v)
		}
	}
	return h.resources.Add(fields)
}

// MethodOutgoingRequestBody gets the request body.
// [method]outgoing-request.body() -> result<outgoing-body>
func (h *OutgoingHandlerHost) MethodOutgoingRequestBody(_ context.Context, self uint32) (uint32, uint32) {
	r, ok := h.resources.Get(self)
	if !ok {
		return 0, 1
	}
	req, ok := r.(*outgoingRequestResource)
	if !ok {
		return 0, 1
	}

	body := &requestBodyResource{buffer: req.body}
	handle := h.resources.Add(body)
	return handle, 0
}

// ResourceDropOutgoingRequest drops an outgoing request resource.
func (h *OutgoingHandlerHost) ResourceDropOutgoingRequest(_ context.Context, self uint32) {
	h.resources.Remove(self)
}

// Request body resource
type requestBodyResource struct {
	buffer *bytes.Buffer
}

func (b *requestBodyResource) Type() preview2.ResourceType { return resourceTypeRequestBody }
func (b *requestBodyResource) Drop()                       {}

// MethodRequestBodyWrite gets a stream for writing body data.
func (h *OutgoingHandlerHost) MethodRequestBodyWrite(_ context.Context, self uint32) (uint32, uint32) {
	r, ok := h.resources.Get(self)
	if !ok {
		return 0, 1
	}
	body, ok := r.(*requestBodyResource)
	if !ok {
		return 0, 1
	}

	stream := preview2.NewOutputStreamResource(body.buffer)
	handle := h.resources.Add(stream)
	return handle, 0
}

// FutureIncomingResponse resource
type futureIncomingResponseResource struct {
	err      error
	response *http.Response
	body     []byte
	mu       sync.Mutex
	ready    bool
}

func (f *futureIncomingResponseResource) Type() preview2.ResourceType {
	return resourceTypeFutureIncomingResponse
}
func (f *futureIncomingResponseResource) Drop() {
	if f.response != nil && f.response.Body != nil {
		f.response.Body.Close()
	}
}

// Handle sends an HTTP request.
// handle(request: outgoing-request, options: option<request-options>) -> result<future-incoming-response>
func (h *OutgoingHandlerHost) Handle(ctx context.Context, requestHandle uint32, _ bool, _ uint32) (uint32, uint32) {
	r, ok := h.resources.Get(requestHandle)
	if !ok {
		return 0, 1
	}
	req, ok := r.(*outgoingRequestResource)
	if !ok {
		return 0, 1
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.method, req.url.String(), bytes.NewReader(req.body.Bytes()))
	if err != nil {
		future := &futureIncomingResponseResource{err: err, ready: true}
		return h.resources.Add(future), 0
	}

	for k, v := range req.headers {
		for _, val := range v {
			httpReq.Header.Add(k, val)
		}
	}

	future := &futureIncomingResponseResource{}
	futureHandle := h.resources.Add(future)

	go func() {
		resp, err := h.client.Do(httpReq)
		if err != nil {
			future.mu.Lock()
			future.err = err
			future.ready = true
			future.mu.Unlock()
			return
		}

		body, readErr := io.ReadAll(resp.Body)
		// Close error is intentionally ignored after full body read
		_ = resp.Body.Close()
		future.mu.Lock()
		if readErr != nil {
			future.err = readErr
			future.ready = true
			future.mu.Unlock()
			return
		}
		future.response = resp
		future.body = body
		future.ready = true
		future.mu.Unlock()
	}()

	return futureHandle, 0
}

// MethodFutureIncomingResponseSubscribe subscribes to the future.
func (h *OutgoingHandlerHost) MethodFutureIncomingResponseSubscribe(_ context.Context, self uint32) uint32 {
	pollable := &preview2.PollableResource{}
	r, ok := h.resources.Get(self)
	if ok {
		if future, ok := r.(*futureIncomingResponseResource); ok {
			future.mu.Lock()
			pollable.SetReady(future.ready)
			future.mu.Unlock()
		}
	}
	return h.resources.Add(pollable)
}

// MethodFutureIncomingResponseGet gets the response.
func (h *OutgoingHandlerHost) MethodFutureIncomingResponseGet(_ context.Context, self uint32) (uint32, bool, uint32) {
	r, ok := h.resources.Get(self)
	if !ok {
		return 0, false, 0
	}
	future, ok := r.(*futureIncomingResponseResource)
	if !ok {
		return 0, false, 0
	}
	future.mu.Lock()
	defer future.mu.Unlock()
	if !future.ready {
		return 0, false, 0
	}
	if future.err != nil {
		return 0, true, 1 // ready, error
	}

	resp := &incomingResponseResource{
		statusCode: uint16(future.response.StatusCode),
		headers:    future.response.Header,
		body:       future.body,
	}
	handle := h.resources.Add(resp)
	return handle, true, 0 // handle, ready, ok
}

// ResourceDropFutureIncomingResponse drops a future incoming response.
func (h *OutgoingHandlerHost) ResourceDropFutureIncomingResponse(_ context.Context, self uint32) {
	r, ok := h.resources.Get(self)
	if ok {
		if future, ok := r.(*futureIncomingResponseResource); ok {
			future.Drop()
		}
	}
	h.resources.Remove(self)
}

// IncomingResponse resource
type incomingResponseResource struct {
	headers    map[string][]string
	body       []byte
	statusCode uint16
}

func (r *incomingResponseResource) Type() preview2.ResourceType { return resourceTypeIncomingResponse }
func (r *incomingResponseResource) Drop()                       {}

// MethodIncomingResponseStatus gets the status code.
func (h *OutgoingHandlerHost) MethodIncomingResponseStatus(_ context.Context, self uint32) uint16 {
	r, ok := h.resources.Get(self)
	if !ok {
		return 0
	}
	resp, ok := r.(*incomingResponseResource)
	if !ok {
		return 0
	}
	return resp.statusCode
}

// MethodIncomingResponseHeaders gets the response headers.
func (h *OutgoingHandlerHost) MethodIncomingResponseHeaders(_ context.Context, self uint32) uint32 {
	r, ok := h.resources.Get(self)
	if !ok {
		return h.resources.Add(preview2.NewFieldsResource())
	}
	resp, ok := r.(*incomingResponseResource)
	if !ok {
		return h.resources.Add(preview2.NewFieldsResource())
	}
	fields := preview2.NewFieldsResource()
	for k, vs := range resp.headers {
		for _, v := range vs {
			fields.Append(k, v)
		}
	}
	return h.resources.Add(fields)
}

// MethodIncomingResponseConsume consumes the response body.
func (h *OutgoingHandlerHost) MethodIncomingResponseConsume(_ context.Context, self uint32) (uint32, uint32) {
	r, ok := h.resources.Get(self)
	if !ok {
		return 0, 1
	}
	resp, ok := r.(*incomingResponseResource)
	if !ok {
		return 0, 1
	}
	body := &incomingBodyResource{data: resp.body}
	return h.resources.Add(body), 0
}

// ResourceDropIncomingResponse drops an incoming response.
func (h *OutgoingHandlerHost) ResourceDropIncomingResponse(_ context.Context, self uint32) {
	h.resources.Remove(self)
}

// Register implements ExplicitRegistrar
func (h *OutgoingHandlerHost) Register() map[string]any {
	return map[string]any{
		"handle": h.Handle,
		// Outgoing request methods
		"[constructor]outgoing-request":                h.ConstructorOutgoingRequest,
		"[method]outgoing-request.set-method":          h.MethodOutgoingRequestSetMethod,
		"[method]outgoing-request.set-path-with-query": h.MethodOutgoingRequestSetPathWithQuery,
		"[method]outgoing-request.set-scheme":          h.MethodOutgoingRequestSetScheme,
		"[method]outgoing-request.set-authority":       h.MethodOutgoingRequestSetAuthority,
		"[method]outgoing-request.headers":             h.MethodOutgoingRequestHeaders,
		"[method]outgoing-request.body":                h.MethodOutgoingRequestBody,
		"[resource-drop]outgoing-request":              h.ResourceDropOutgoingRequest,
		// Future incoming response methods
		"[method]future-incoming-response.subscribe": h.MethodFutureIncomingResponseSubscribe,
		"[method]future-incoming-response.get":       h.MethodFutureIncomingResponseGet,
		"[resource-drop]future-incoming-response":    h.ResourceDropFutureIncomingResponse,
		// Incoming response methods
		"[method]incoming-response.status":  h.MethodIncomingResponseStatus,
		"[method]incoming-response.headers": h.MethodIncomingResponseHeaders,
		"[method]incoming-response.consume": h.MethodIncomingResponseConsume,
		"[resource-drop]incoming-response":  h.ResourceDropIncomingResponse,
	}
}
