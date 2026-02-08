package http

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func TestTypesHost_Namespace(t *testing.T) {
	h := NewTypesHost(preview2.NewResourceTable())
	if ns := h.Namespace(); ns != "wasi:http/types@0.2.8" {
		t.Errorf("expected namespace wasi:http/types@0.2.8, got %s", ns)
	}
}

func TestTypesHost_Fields(t *testing.T) {
	ctx := context.Background()
	resources := preview2.NewResourceTable()
	h := NewTypesHost(resources)

	// Create fields
	handle := h.ConstructorFields(ctx)
	if handle == 0 {
		t.Fatal("expected non-zero handle")
	}

	// Append values
	if err := h.MethodFieldsAppend(ctx, handle, "Content-Type", []byte("application/json")); err != 0 {
		t.Errorf("append failed: %d", err)
	}
	if err := h.MethodFieldsAppend(ctx, handle, "X-Custom", []byte("value1")); err != 0 {
		t.Errorf("append failed: %d", err)
	}
	if err := h.MethodFieldsAppend(ctx, handle, "X-Custom", []byte("value2")); err != 0 {
		t.Errorf("append failed: %d", err)
	}

	// Get values
	vals := h.MethodFieldsGet(ctx, handle, "Content-Type")
	if len(vals) != 1 || string(vals[0]) != "application/json" {
		t.Errorf("expected [application/json], got %v", vals)
	}

	vals = h.MethodFieldsGet(ctx, handle, "X-Custom")
	if len(vals) != 2 {
		t.Errorf("expected 2 values, got %d", len(vals))
	}

	// Has check
	if !h.MethodFieldsHas(ctx, handle, "Content-Type") {
		t.Error("expected has to return true for Content-Type")
	}
	if h.MethodFieldsHas(ctx, handle, "NonExistent") {
		t.Error("expected has to return false for NonExistent")
	}

	// Set values
	if err := h.MethodFieldsSet(ctx, handle, "X-Custom", [][]byte{[]byte("newvalue")}); err != 0 {
		t.Errorf("set failed: %d", err)
	}
	vals = h.MethodFieldsGet(ctx, handle, "X-Custom")
	if len(vals) != 1 || string(vals[0]) != "newvalue" {
		t.Errorf("expected [newvalue], got %v", vals)
	}

	// Delete
	if err := h.MethodFieldsDelete(ctx, handle, "X-Custom"); err != 0 {
		t.Errorf("delete failed: %d", err)
	}
	if h.MethodFieldsHas(ctx, handle, "X-Custom") {
		t.Error("expected X-Custom to be deleted")
	}

	// Entries
	entries := h.MethodFieldsEntries(ctx, handle)
	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}

	// Clone
	cloneHandle := h.MethodFieldsClone(ctx, handle)
	if cloneHandle == handle {
		t.Error("clone should return different handle")
	}
	cloneVals := h.MethodFieldsGet(ctx, cloneHandle, "Content-Type")
	if len(cloneVals) != 1 {
		t.Error("clone should have same values")
	}

	// Drop
	h.ResourceDropFields(ctx, handle)
	h.ResourceDropFields(ctx, cloneHandle)
}

func TestTypesHost_IncomingRequest(t *testing.T) {
	ctx := context.Background()
	resources := preview2.NewResourceTable()
	h := NewTypesHost(resources)

	// Create a mock request
	reqURL, _ := url.Parse("https://example.com/api/test?foo=bar")
	req := &http.Request{
		Method: "POST",
		URL:    reqURL,
		Host:   "example.com",
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"X-Request-Id": []string{"12345"},
		},
	}
	h.SetRequest(&Request{Request: req, Body: []byte(`{"key":"value"}`)})

	// Test method
	method := h.MethodIncomingRequestMethod(ctx, 1)
	if method != "POST" {
		t.Errorf("expected POST, got %s", method)
	}

	// Test path-with-query
	path, ok := h.MethodIncomingRequestPathWithQuery(ctx, 1)
	if !ok || path != "/api/test?foo=bar" {
		t.Errorf("expected /api/test?foo=bar, got %s (ok=%v)", path, ok)
	}

	// Test scheme
	scheme, ok := h.MethodIncomingRequestScheme(ctx, 1)
	if !ok || scheme != 1 { // 1 = HTTPS
		t.Errorf("expected scheme 1 (HTTPS), got %d (ok=%v)", scheme, ok)
	}

	// Test authority
	auth, ok := h.MethodIncomingRequestAuthority(ctx, 1)
	if !ok || auth != "example.com" {
		t.Errorf("expected example.com, got %s (ok=%v)", auth, ok)
	}

	// Test headers
	headersHandle := h.MethodIncomingRequestHeaders(ctx, 1)
	if headersHandle == 0 {
		t.Fatal("expected non-zero headers handle")
	}
	ctVals := h.MethodFieldsGet(ctx, headersHandle, "Content-Type")
	if len(ctVals) != 1 || string(ctVals[0]) != "application/json" {
		t.Errorf("expected [application/json], got %v", ctVals)
	}

	// Test consume body
	bodyHandle, err := h.MethodIncomingRequestConsume(ctx, 1)
	if err != 0 {
		t.Fatalf("consume failed: %d", err)
	}

	// Get body stream
	streamHandle, err := h.MethodIncomingBodyStream(ctx, bodyHandle)
	if err != 0 {
		t.Fatalf("stream failed: %d", err)
	}
	if streamHandle == 0 {
		t.Fatal("expected non-zero stream handle")
	}

	// Finish body
	trailersHandle := h.StaticIncomingBodyFinish(ctx, bodyHandle)
	if trailersHandle == 0 {
		t.Fatal("expected non-zero trailers handle")
	}

	// Get trailers
	_, ready, errCode := h.MethodFutureTrailersGet(ctx, trailersHandle)
	if !ready || errCode != 0 {
		t.Errorf("expected ready=true, errCode=0, got ready=%v, errCode=%d", ready, errCode)
	}

	// Subscribe
	pollHandle := h.MethodFutureTrailersSubscribe(ctx, trailersHandle)
	if pollHandle == 0 {
		t.Fatal("expected non-zero pollable handle")
	}

	// Cleanup
	h.ResourceDropIncomingRequest(ctx, 1)
	h.ResourceDropIncomingBody(ctx, bodyHandle)
	h.ResourceDropFutureTrailers(ctx, trailersHandle)
}

func TestTypesHost_OutgoingResponse(t *testing.T) {
	ctx := context.Background()
	resources := preview2.NewResourceTable()
	h := NewTypesHost(resources)

	// Create headers
	headersHandle := h.ConstructorFields(ctx)
	h.MethodFieldsAppend(ctx, headersHandle, "Content-Type", []byte("text/html"))

	// Create outgoing response
	respHandle := h.ConstructorOutgoingResponse(ctx, headersHandle)
	if respHandle == 0 {
		t.Fatal("expected non-zero response handle")
	}

	// Set status code
	if err := h.MethodOutgoingResponseSetStatusCode(ctx, respHandle, 201); err != 0 {
		t.Errorf("set status failed: %d", err)
	}

	// Get body
	bodyHandle, err := h.MethodOutgoingResponseBody(ctx, respHandle)
	if err != 0 {
		t.Fatalf("body failed: %d", err)
	}

	// Write to body
	streamHandle, err := h.MethodOutgoingBodyWrite(ctx, bodyHandle)
	if err != 0 {
		t.Fatalf("write failed: %d", err)
	}

	// Get the stream resource and write
	streamRes, ok := resources.Get(streamHandle)
	if !ok {
		t.Fatal("stream resource not found")
	}
	if writer, ok := streamRes.(interface{ Write([]byte) error }); ok {
		writer.Write([]byte("<html>Hello</html>"))
	}

	// Finish body
	if err := h.StaticOutgoingBodyFinish(ctx, bodyHandle, false, 0); err != 0 {
		t.Errorf("finish failed: %d", err)
	}

	// Check response
	resp := h.GetResponse()
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.StatusCode != 201 {
		t.Errorf("expected status 201, got %d", resp.StatusCode)
	}
	if string(resp.Body) != "<html>Hello</html>" {
		t.Errorf("expected body <html>Hello</html>, got %s", string(resp.Body))
	}

	// Response outparam
	h.SetResponseOutparamHandle(999)
	if h.GetResponseOutparamHandle() != 999 {
		t.Error("response outparam handle mismatch")
	}
	h.StaticResponseOutparamSet(ctx, 999, true, respHandle)
	h.ResourceDropResponseOutparam(ctx, 999)

	// Reset
	h.Reset()
	if h.GetResponse() != nil {
		t.Error("expected nil response after reset")
	}

	// Cleanup
	h.ResourceDropOutgoingResponse(ctx, respHandle)
	h.ResourceDropOutgoingBody(ctx, bodyHandle)
}

func TestTypesHost_Register(t *testing.T) {
	resources := preview2.NewResourceTable()
	h := NewTypesHost(resources)

	reg := h.Register()
	expectedMethods := []string{
		"[constructor]fields",
		"[method]fields.append",
		"[method]fields.get",
		"[method]incoming-request.method",
		"[method]incoming-request.headers",
		"[constructor]outgoing-response",
		"[method]outgoing-response.body",
		"[resource-drop]fields",
	}

	for _, method := range expectedMethods {
		if _, ok := reg[method]; !ok {
			t.Errorf("expected method %s in register", method)
		}
	}
}

func TestTypesHost_InvalidHandles(t *testing.T) {
	ctx := context.Background()
	resources := preview2.NewResourceTable()
	h := NewTypesHost(resources)

	// Invalid handle for fields operations
	if err := h.MethodFieldsAppend(ctx, 999, "key", []byte("val")); err != 1 {
		t.Error("expected error for invalid handle")
	}
	if vals := h.MethodFieldsGet(ctx, 999, "key"); vals != nil {
		t.Error("expected nil for invalid handle")
	}
	if h.MethodFieldsHas(ctx, 999, "key") {
		t.Error("expected false for invalid handle")
	}

	// Invalid handle for response operations
	if err := h.MethodOutgoingResponseSetStatusCode(ctx, 999, 200); err != 1 {
		t.Error("expected error for invalid handle")
	}
	if _, err := h.MethodOutgoingResponseBody(ctx, 999); err != 1 {
		t.Error("expected error for invalid handle")
	}
	if _, err := h.MethodOutgoingBodyWrite(ctx, 999); err != 1 {
		t.Error("expected error for invalid handle")
	}
	if _, err := h.MethodIncomingBodyStream(ctx, 999); err != 1 {
		t.Error("expected error for invalid handle")
	}
}

func TestTypesHost_NoRequest(t *testing.T) {
	ctx := context.Background()
	resources := preview2.NewResourceTable()
	h := NewTypesHost(resources)

	// No request set - should return defaults
	if method := h.MethodIncomingRequestMethod(ctx, 1); method != "GET" {
		t.Errorf("expected GET for no request, got %s", method)
	}
	if _, ok := h.MethodIncomingRequestPathWithQuery(ctx, 1); ok {
		t.Error("expected ok=false for no request")
	}
	if _, ok := h.MethodIncomingRequestScheme(ctx, 1); ok {
		t.Error("expected ok=false for no request")
	}
	if _, ok := h.MethodIncomingRequestAuthority(ctx, 1); ok {
		t.Error("expected ok=false for no request")
	}
}

func TestOutgoingHandlerHost_Namespace(t *testing.T) {
	h := NewOutgoingHandlerHost(preview2.NewResourceTable())
	if ns := h.Namespace(); ns != "wasi:http/outgoing-handler@0.2.8" {
		t.Errorf("expected namespace wasi:http/outgoing-handler@0.2.8, got %s", ns)
	}
}

func TestOutgoingHandlerHost_OutgoingRequest(t *testing.T) {
	ctx := context.Background()
	resources := preview2.NewResourceTable()
	h := NewOutgoingHandlerHost(resources)

	// Create headers
	fields := preview2.NewFieldsResource()
	fields.Append("User-Agent", "test-agent")
	headersHandle := resources.Add(fields)

	// Create request
	reqHandle := h.ConstructorOutgoingRequest(ctx, headersHandle)
	if reqHandle == 0 {
		t.Fatal("expected non-zero request handle")
	}

	// Set method
	if err := h.MethodOutgoingRequestSetMethod(ctx, reqHandle, "POST"); err != 0 {
		t.Errorf("set method failed: %d", err)
	}

	// Set path
	if err := h.MethodOutgoingRequestSetPathWithQuery(ctx, reqHandle, true, "/api/test"); err != 0 {
		t.Errorf("set path failed: %d", err)
	}

	// Set scheme
	if err := h.MethodOutgoingRequestSetScheme(ctx, reqHandle, true, 1); err != 0 { // HTTPS
		t.Errorf("set scheme failed: %d", err)
	}

	// Set authority
	if err := h.MethodOutgoingRequestSetAuthority(ctx, reqHandle, true, "api.example.com"); err != 0 {
		t.Errorf("set authority failed: %d", err)
	}

	// Get headers
	newHeadersHandle := h.MethodOutgoingRequestHeaders(ctx, reqHandle)
	if newHeadersHandle == 0 {
		t.Fatal("expected non-zero headers handle")
	}

	// Get body
	bodyHandle, err := h.MethodOutgoingRequestBody(ctx, reqHandle)
	if err != 0 {
		t.Fatalf("body failed: %d", err)
	}
	if bodyHandle == 0 {
		t.Fatal("expected non-zero body handle")
	}

	// Cleanup
	h.ResourceDropOutgoingRequest(ctx, reqHandle)
}

func TestOutgoingHandlerHost_InvalidHandles(t *testing.T) {
	ctx := context.Background()
	resources := preview2.NewResourceTable()
	h := NewOutgoingHandlerHost(resources)

	// Invalid request handle
	if err := h.MethodOutgoingRequestSetMethod(ctx, 999, "GET"); err != 1 {
		t.Error("expected error for invalid handle")
	}
	if err := h.MethodOutgoingRequestSetPathWithQuery(ctx, 999, true, "/"); err != 1 {
		t.Error("expected error for invalid handle")
	}
	if err := h.MethodOutgoingRequestSetScheme(ctx, 999, true, 0); err != 1 {
		t.Error("expected error for invalid handle")
	}
	if err := h.MethodOutgoingRequestSetAuthority(ctx, 999, true, "x"); err != 1 {
		t.Error("expected error for invalid handle")
	}
	if _, err := h.MethodOutgoingRequestBody(ctx, 999); err != 1 {
		t.Error("expected error for invalid handle")
	}

	// Invalid future handle
	if _, ready, _ := h.MethodFutureIncomingResponseGet(ctx, 999); ready {
		t.Error("expected not ready for invalid handle")
	}
}

func TestOutgoingHandlerHost_Register(t *testing.T) {
	resources := preview2.NewResourceTable()
	h := NewOutgoingHandlerHost(resources)

	reg := h.Register()
	expectedMethods := []string{
		"handle",
		"[constructor]outgoing-request",
		"[method]outgoing-request.set-method",
		"[method]future-incoming-response.subscribe",
		"[method]incoming-response.status",
	}

	for _, method := range expectedMethods {
		if _, ok := reg[method]; !ok {
			t.Errorf("expected method %s in register", method)
		}
	}
}

func TestIncomingResponseMethods(t *testing.T) {
	ctx := context.Background()
	resources := preview2.NewResourceTable()
	h := NewOutgoingHandlerHost(resources)

	// Create incoming response resource directly
	resp := &incomingResponseResource{
		statusCode: 200,
		headers:    map[string][]string{"X-Test": {"value"}},
		body:       []byte("response body"),
	}
	respHandle := resources.Add(resp)

	// Test status
	status := h.MethodIncomingResponseStatus(ctx, respHandle)
	if status != 200 {
		t.Errorf("expected status 200, got %d", status)
	}

	// Test headers
	headersHandle := h.MethodIncomingResponseHeaders(ctx, respHandle)
	if headersHandle == 0 {
		t.Fatal("expected non-zero headers handle")
	}

	// Test consume
	bodyHandle, err := h.MethodIncomingResponseConsume(ctx, respHandle)
	if err != 0 {
		t.Fatalf("consume failed: %d", err)
	}
	if bodyHandle == 0 {
		t.Fatal("expected non-zero body handle")
	}

	// Invalid status
	if status := h.MethodIncomingResponseStatus(ctx, 999); status != 0 {
		t.Error("expected 0 for invalid handle")
	}
	if _, err := h.MethodIncomingResponseConsume(ctx, 999); err != 1 {
		t.Error("expected error for invalid handle")
	}

	// Cleanup
	h.ResourceDropIncomingResponse(ctx, respHandle)
}

func TestFutureIncomingResponse(t *testing.T) {
	ctx := context.Background()
	resources := preview2.NewResourceTable()
	h := NewOutgoingHandlerHost(resources)

	// Create future with response
	future := &futureIncomingResponseResource{
		response: &http.Response{StatusCode: 200},
		body:     []byte("body"),
		ready:    true,
	}
	futureHandle := resources.Add(future)

	// Subscribe
	pollHandle := h.MethodFutureIncomingResponseSubscribe(ctx, futureHandle)
	if pollHandle == 0 {
		t.Fatal("expected non-zero pollable handle")
	}

	// Get response
	respHandle, ready, errCode := h.MethodFutureIncomingResponseGet(ctx, futureHandle)
	if !ready || errCode != 0 {
		t.Errorf("expected ready=true, errCode=0, got ready=%v, errCode=%d", ready, errCode)
	}
	if respHandle == 0 {
		t.Fatal("expected non-zero response handle")
	}

	// Cleanup
	h.ResourceDropFutureIncomingResponse(ctx, futureHandle)

	// Test not ready future
	notReadyFuture := &futureIncomingResponseResource{ready: false}
	notReadyHandle := resources.Add(notReadyFuture)
	if _, ready, _ := h.MethodFutureIncomingResponseGet(ctx, notReadyHandle); ready {
		t.Error("expected not ready")
	}

	// Test error future
	errFuture := &futureIncomingResponseResource{ready: true, err: bytes.ErrTooLarge}
	errHandle := resources.Add(errFuture)
	if _, _, errCode := h.MethodFutureIncomingResponseGet(ctx, errHandle); errCode != 1 {
		t.Error("expected error code 1")
	}
}
